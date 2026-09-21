// Package reconciler converges persisted composition intent with provider state.
//
//nolint:wsl_v5 // Reconciliation preserves each durable lifecycle checkpoint as a separate branch.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/verification"
)

type Store interface {
	Active(context.Context) ([]domain.Composition, error)
	SaveObservation(context.Context, domain.Composition) error
	Expire(context.Context, time.Time) error
}

type Runtime interface {
	domain.RuntimeProvider
	Absent(context.Context, domain.WorkloadRef) (bool, error)
}

type Verifier interface {
	Verify(context.Context, string, string, map[string]string, domain.ResolvedPlan) (verification.Result, error)
	Absent(context.Context, string) error
}

type Config struct {
	NewObservationSession func(Runtime) (func(context.Context, func(string)) error, error)
	StartWatch            func(context.Context, func(string)) error
	PolicyGuard           func(context.Context) error
	Messaging             domain.MessagingProvider
	Interval              time.Duration
	ProvisionTimeout      time.Duration
	DrainTimeout          time.Duration
}

type Reconciler struct {
	recoveryInterval time.Duration
	routeMu          *sync.Mutex
	store            Store
	runtime          Runtime
	routes           domain.RoutingProvider
	verifier         Verifier
	guard            func(context.Context) error
	log              *slog.Logger
	cfg              Config
	now              func() time.Time
	routeCycle       *routeCycle
}

// Only successful, identical route snapshots may share a provider observation
// within one scan. The next scan always checks external routing drift again.
type routeCycle struct {
	applied *domain.RouteSnapshot
}

func New(store Store, runtime Runtime, routes domain.RoutingProvider, verifier Verifier, guard func(context.Context) error, logger *slog.Logger, cfg Config) *Reconciler {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}

	if cfg.ProvisionTimeout <= 0 {
		cfg.ProvisionTimeout = 60 * time.Second
	}

	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 10 * time.Second
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Reconciler{recoveryInterval: 30 * time.Second, routeMu: &sync.Mutex{}, store: store, runtime: runtime, routes: routes, verifier: verifier, guard: guard, log: logger, cfg: cfg, now: time.Now}
}

func (r *Reconciler) Run(ctx context.Context) error {
	if store, ok := r.store.(eventStore); ok {
		return r.runQueue(ctx, store)
	}

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}

		if err := r.Tick(ctx); err != nil {
			return err
		}

		timer.Reset(r.cfg.Interval)
	}
}

// Tick scans the database every time; no in-memory notification is required for
// discovery or recovery. Losing leadership aborts the entire cycle.
func (r *Reconciler) Tick(ctx context.Context) error {
	r.routeCycle = &routeCycle{}
	defer func() { r.routeCycle = nil }()
	if r.guard == nil {
		return fmt.Errorf("reconciler requires a leadership guard")
	}

	if err := r.guard(ctx); err != nil {
		return fmt.Errorf("check leadership: %w", err)
	}

	if err := r.store.Expire(ctx, r.now()); err != nil {
		return fmt.Errorf("persist expirations: %w", err)
	}

	compositions, err := r.store.Active(ctx)
	if err != nil {
		return fmt.Errorf("scan desired compositions: %w", err)
	}

	for _, c := range compositions {
		if err := r.reconcile(ctx, &c, false); err != nil {
			return err
		}
	}

	return nil
}

func (r *Reconciler) reconcile(ctx context.Context, c *domain.Composition, event bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if c.Runtime.NextAttemptAt.After(r.now()) && (!event || c.Runtime.Attempts > 0) {
		return nil
	}

	if err := r.guard(ctx); err != nil {
		return fmt.Errorf("check leadership: %w", err)
	}

	before := c.Phase
	stepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err := r.step(stepCtx, c)
	cancel()
	if errors.Is(err, domain.ErrStaleObservation) {
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Do not publish an observation from a worker that lost its session lock.
	if guardErr := r.guard(ctx); guardErr != nil {
		return fmt.Errorf("leadership lost: %w", guardErr)
	}

	err = r.pendingRoutes(c, err)
	switch {
	case err != nil:
		r.failure(c, err)
		r.log.Warn("composition reconcile failed", "composition", c.ID, "phase", c.Phase, "error", err)
	case event && c.Phase == domain.PhaseReady:
		c.Runtime.NextAttemptAt = r.now().Add(30 * time.Second)
	case !c.Runtime.NextAttemptAt.After(r.now()):
		c.Runtime.NextAttemptAt = r.now().Add(r.cfg.Interval)
	}

	c.ObservedGeneration = c.Generation
	return r.publishObservation(ctx, c, before)
}

func (r *Reconciler) publishObservation(ctx context.Context, c *domain.Composition, before domain.Phase) error {
	if err := r.store.SaveObservation(ctx, *c); err != nil {
		if errors.Is(err, domain.ErrStaleObservation) {
			return nil
		}

		return fmt.Errorf("save composition observation: %w", err)
	}

	if before != c.Phase {
		r.log.Info("composition phase changed", "composition", c.ID, "from", before, "to", c.Phase)
	}

	return nil
}

func (r *Reconciler) pendingRoutes(c *domain.Composition, err error) error {
	if !errors.Is(err, errRoutesPending) {
		return err
	}

	started := c.Runtime.ProvisionStartedAt
	if started.IsZero() {
		started = c.CreatedAt
	}

	if !c.DeletionRequested && r.now().Sub(started) >= r.cfg.ProvisionTimeout {
		return err
	}

	if len(c.Conditions) > 1 {
		c.Conditions[1].Status = false
		c.Conditions[1].Message = err.Error()
	}

	c.LastError = nil
	return nil
}

func (r *Reconciler) step(ctx context.Context, c *domain.Composition) error {
	if c.DeletionRequested {
		return r.destroy(ctx, c)
	}

	if c.Endpoints == nil {
		c.Endpoints = map[string]domain.Endpoint{}
	}

	if c.Components == nil {
		c.Components = map[string]domain.ComponentObservation{}
	}

	c.Phase = domain.PhaseProvisioning
	if c.LatestOperation.Kind == "update" {
		c.Phase = domain.PhaseUpdating
	}

	startedAt := c.Runtime.ProvisionStartedAt
	if startedAt.IsZero() {
		startedAt = c.CreatedAt
	}

	setReady(c, false)
	if c.LatestOperation.Status != "succeeded" {
		c.LatestOperation.Status = "running"
		c.LatestOperation.Error = nil
	}

	if c.Runtime.Plan == nil {
		return fmt.Errorf("persisted composition has no resolved catalog plan")
	}

	names := domain.OverrideNames(c.Overrides)
	profiles := c.Runtime.Plan.Profiles()
	if len(names) > domain.MaxOverrides {
		return fmt.Errorf("persisted overrides and profiles do not match")
	}

	if c.Runtime.Workloads == nil {
		c.Runtime.Workloads = map[string]domain.WorkloadRef{}
	}
	if c.Runtime.Executions == nil {
		c.Runtime.Executions = map[string]domain.ExecutionRef{}
	}

	allReady := true
	allCompleted := true
	anySuspended := false
	waitingForOther := false
	hasEndpoint := c.Runtime.Plan.Baseline.Verification.Kind != "none"
	failed := false
	messages := []string{}
	var failures []error
	pods := map[string]string{}
	c.Conditions = []domain.Condition{{Type: "WorkloadsReady"}, {Type: "RoutesConfigured"}, {Type: "RouteVerified"}}
	if c.MessageIsolation {
		c.Conditions = append(c.Conditions, domain.Condition{Type: "MessagingReady"})
		if r.cfg.Messaging == nil {
			return fmt.Errorf("Pub/Sub provider unavailable")
		}

		if err := r.cfg.Messaging.Validate(ctx, c.Runtime.Plan.Baseline); err != nil {
			for i := range c.MessageSubscriptions {
				c.MessageSubscriptions[i].Ready = false
			}

			return err
		}

		desired := append([]domain.MessageSubscription(nil), c.Runtime.Plan.MessageSubscriptions...)
		for i := range desired {
			desired[i].Ready = c.Runtime.MessagingObserved[desired[i].Name]
			for _, old := range c.MessageSubscriptions {
				if old.Name == desired[i].Name {
					desired[i].Ready = old.Ready || c.Runtime.MessagingObserved[old.Name]
					desired[i].BacklogMayBeLost = old.BacklogMayBeLost
					desired[i].Instance = old.Instance
				}
			}
		}

		var err error
		c.MessageSubscriptions, err = r.cfg.Messaging.Ensure(ctx, domain.MessagingSpec{CompositionID: c.ID, OwnershipToken: c.Runtime.OwnershipToken, Subscriptions: desired})
		if c.Runtime.MessagingObserved == nil {
			c.Runtime.MessagingObserved = map[string]bool{}
		}

		for _, sub := range c.MessageSubscriptions {
			if sub.Ready {
				c.Runtime.MessagingObserved[sub.Name] = true
			}
		}

		if err != nil {
			return err
		}

		for _, sub := range c.MessageSubscriptions {
			if !sub.Ready {
				return fmt.Errorf("Pub/Sub subscription is not ready: %s", sub.Name)
			}
		}

		c.Conditions[3] = domain.Condition{Type: "MessagingReady", Status: true, Message: "filtered subscriptions ready; application propagation requires separate verification"}
		for _, sub := range c.MessageSubscriptions {
			if sub.BacklogMayBeLost {
				c.Conditions = append(c.Conditions, domain.Condition{Type: "MessagingBacklogLost", Status: true, Message: "a subscription was recreated; previously queued messages may be lost"})
				break
			}
		}

		// Persist observed subscriptions before any publisher can start.
		if err := r.store.SaveObservation(ctx, *c); err != nil {
			return err
		}
	}

	for _, component := range names {
		profile, ok := profiles[component]
		if !ok || profile.ID != component {
			return fmt.Errorf("missing resolved profile for %s", component)
		}
		hasEndpoint = hasEndpoint || profile.HasEndpoint()

		override := c.Overrides[component]
		count := max(len(names), len(c.Runtime.PublishedOverrides))
		var preview *domain.PreviewSnapshot
		if snapshot, ok := c.Runtime.Plan.Previews[component]; ok {
			preview = &snapshot
		}

		var execution *domain.ExecutionRef
		if profile.WorkloadKind() == domain.WorkloadJob || profile.WorkloadKind() == domain.WorkloadScheduledJob {
			id, hash := domain.ExecutionID(profile, override.Image)
			persisted, exists := c.Runtime.Executions[component]
			switch {
			case !exists:
				persisted = domain.ExecutionRef{ID: id, SpecHash: hash, Generation: c.Generation, State: domain.ExecutionPending}
				c.Runtime.Executions[component] = persisted
				// The execution identity is durable before the provider can create a Job.
				if err := r.store.SaveObservation(ctx, *c); err != nil {
					return err
				}
			case persisted.SpecHash != hash && (persisted.State == domain.ExecutionPending || persisted.State == domain.ExecutionRunning || (profile.WorkloadKind() == domain.WorkloadScheduledJob && persisted.State == domain.ExecutionReady)):
				return fmt.Errorf("cannot replace active Job %s; wait for it to finish or delete the composition", component)
			case persisted.SpecHash != hash:
				persisted = domain.ExecutionRef{ID: id, SpecHash: hash, Generation: c.Generation, State: domain.ExecutionPending}
				c.Runtime.Executions[component] = persisted
				if err := r.store.SaveObservation(ctx, *c); err != nil {
					return err
				}
			}
			execution = &persisted
		}

		// Kubernetes CronJobs cannot durably cap executions themselves. Leave
		// schedules suspended until an Envy-owned execution gate is available.
		scheduleActive := false
		ref, err := r.runtime.Ensure(ctx, domain.WorkloadSpec{Execution: execution, ScheduleActive: scheduleActive, DesiredComponents: names, BaselineNamespace: c.Runtime.Plan.Baseline.Routing.Namespace, MessagingEnv: domain.MessagingEnvironment(c.Runtime.Plan.Baseline, component, c.MessageIsolation, c.MessageSubscriptions), Preview: preview, Previews: c.Runtime.Plan.Previews, CompositionID: c.ID, ProjectID: c.Project, ComponentID: component, Image: override.Image, OwnershipToken: c.Runtime.OwnershipToken, Profile: profile, WorkloadCount: max(1, count)})
		if ref.Namespace != "" {
			c.Runtime.Workloads[component] = ref
		}

		observation := domain.WorkloadObservation{}
		if err == nil {
			observation, err = r.runtime.Observe(ctx, ref)
		}

		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", component, err))
			observation.Message = err.Error()
		}

		state := domain.ComponentObservation{Source: "override", Status: "provisioning", Image: override.Image, WorkloadID: observation.WorkloadID}
		if execution != nil {
			state.ExecutionID = execution.ID
			state.ExecutionState = observation.State
			if state.ExecutionState == "" {
				state.ExecutionState = domain.ExecutionPending
			}
			persisted := c.Runtime.Executions[component]
			persisted.State = state.ExecutionState
			persisted.Runs = observation.Runs
			persisted.ProviderID = ref.JobUID
			if ref.CronJobUID != "" {
				persisted.ProviderID = ref.CronJobUID
			}
			c.Runtime.Executions[component] = persisted
			state.Status = string(state.ExecutionState)
		}
		if observation.Image != "" {
			state.Image = observation.Image
		}

		if observation.Ready {
			state.Status = "ready"
			if execution != nil && observation.State == domain.ExecutionSucceeded {
				state.Status = string(domain.ExecutionSucceeded)
			}
			pods[component] = observation.WorkloadID
		} else {
			allReady = false
			if observation.State == domain.ExecutionSuspended {
				anySuspended = true
			} else {
				waitingForOther = true
			}
			messages = append(messages, component+": "+observation.Message)
		}

		failed = failed || observation.Failed
		if execution != nil && observation.State != domain.ExecutionSucceeded {
			allCompleted = false
		}
		c.Components[component] = state
		c.Conditions = append(c.Conditions, domain.Condition{Type: "WorkloadReady/" + component, Status: observation.Ready, Message: observation.Message})
	}

	c.Conditions[0] = domain.Condition{Type: "WorkloadsReady", Status: allReady, Message: strings.Join(messages, "; ")}
	if !allReady {
		if anySuspended && !waitingForOther && !failed && len(failures) == 0 {
			c.Phase = domain.PhaseSuspended
			c.VerificationLevel = "none"
			c.Conditions[1] = domain.Condition{Type: "RoutesConfigured", Status: true, Message: "no endpoint routes required"}
			c.Conditions[2] = domain.Condition{Type: "RouteVerified", Status: true, Message: "schedule remains suspended pending an execution gate"}
			c.LastError = nil
			c.LatestOperation.Status = "succeeded"
			c.LatestOperation.Error = nil
			c.Runtime.NextAttemptAt = r.now().Add(r.cfg.Interval)
			return nil
		}
		// Retain every published override route even when only one workload fails.
		if c.Runtime.RoutingActive {
			if err := r.syncRoutes(ctx); err != nil {
				return err
			}

			c.Conditions[1].Status = true
		}

		if len(failures) > 0 {
			return errors.Join(failures...)
		}

		if failed || r.now().Sub(startedAt) >= r.cfg.ProvisionTimeout {
			return fmt.Errorf("components are not ready: %s", strings.Join(messages, "; "))
		}

		c.LastError = nil
		c.Runtime.Attempts = 0
		return nil
	}
	if !hasEndpoint {
		retiring, err := r.retireEndpointFree(ctx, c)
		if err != nil {
			return err
		}
		if retiring {
			return nil
		}
		c.Phase = domain.PhaseReady
		if allCompleted {
			c.Phase = domain.PhaseCompleted
		}
		c.VerificationLevel = "none"
		c.Conditions[1] = domain.Condition{Type: "RoutesConfigured", Status: true, Message: "no endpoint routes required"}
		c.Conditions[2] = domain.Condition{Type: "RouteVerified", Status: true, Message: "no endpoint routes required"}
		c.LastError = nil
		c.LatestOperation.Status = "succeeded"
		c.LatestOperation.Error = nil
		c.Runtime.Attempts = 0
		c.Runtime.NextAttemptAt = r.now().Add(r.cfg.Interval)
		return nil
	}

	newRoutingIntent := !c.Runtime.RoutingActive
	c.Runtime.RoutingActive = true
	// Persist the intention before writing shared routing configuration. A crash
	// after a Kubernetes write can therefore be recovered from durable state.
	if newRoutingIntent {
		if err := r.store.SaveObservation(ctx, *c); err != nil {
			return err
		}
	}

	if err := r.syncRoutes(ctx); err != nil {
		return err
	}

	if !overridesEqual(c.Runtime.PublishedOverrides, c.Overrides) {
		if c.Runtime.RetiringWorkloads == nil {
			c.Runtime.RetiringWorkloads = map[string]domain.WorkloadRef{}
		}

		for component := range c.Runtime.PublishedOverrides {
			if _, stillDesired := c.Overrides[component]; stillDesired {
				continue
			}

			if ref := c.Runtime.WorkloadFor(component); ref.Namespace != "" {
				c.Runtime.RetiringWorkloads[component] = ref
				delete(c.Runtime.Workloads, component)
			}
		}

		c.Runtime.PublishedOverrides = cloneOverrides(c.Overrides)
		if err := r.store.SaveObservation(ctx, *c); err != nil {
			return err
		}

		if err := r.syncRoutes(ctx); err != nil {
			return err
		}
	}

	c.Conditions[1].Status = true
	host, err := endpointHost(*c)
	if err != nil {
		return err
	}

	verified, err := r.verifier.Verify(ctx, c.ID, host, pods, planForOverrides(*c.Runtime.Plan, c.Overrides))
	if err != nil {
		c.Conditions[2].Message = err.Error()
		if c.LatestOperation.Status != "succeeded" && r.now().Sub(startedAt) < r.cfg.ProvisionTimeout {
			// Proxy convergence is expected during initial provisioning. A bounded
			// wait must not return a spurious terminal failure during this window.
			c.LastError = nil
			return nil
		}

		return fmt.Errorf("verify request routing: %w", err)
	}

	for _, hop := range verified.Composition {
		old := c.Components[hop.Service]
		old.Status = "ready"
		old.WorkloadID = hop.WorkloadID
		if _, overridden := c.Overrides[hop.Service]; !overridden {
			old.Source = "baseline"
		}

		c.Components[hop.Service] = old
	}

	if len(c.Runtime.RetiringWorkloads) > 0 {
		if c.Runtime.RetirementDrainUntil == nil {
			until := r.now().Add(r.cfg.DrainTimeout)
			c.Runtime.RetirementDrainUntil = &until
			c.Conditions = append(c.Conditions, domain.Condition{Type: "RetiringWorkloads", Message: "waiting for retired override drain"})
			return nil
		}

		if r.now().Before(*c.Runtime.RetirementDrainUntil) {
			c.Conditions = append(c.Conditions, domain.Condition{Type: "RetiringWorkloads", Message: "draining retired override requests"})
			return nil
		}

		for component, ref := range c.Runtime.RetiringWorkloads {
			if err := r.runtime.DeleteWorkload(ctx, ref); err != nil {
				return fmt.Errorf("retire %s: %w", component, err)
			}

			absent, err := r.runtime.WorkloadAbsent(ctx, ref)
			if err != nil {
				return fmt.Errorf("confirm retirement for %s: %w", component, err)
			}

			if !absent {
				c.Conditions = append(c.Conditions, domain.Condition{Type: "RetiringWorkloads", Message: "waiting for retired workload deletion"})
				return nil
			}

			delete(c.Runtime.RetiringWorkloads, component)
		}

		c.Runtime.RetirementDrainUntil = nil
	}

	c.Phase = domain.PhaseReady
	c.Conditions[2].Status = true
	c.Conditions[2].Message = "override pod and shared baseline hops observed through ingress"
	c.VerificationLevel = "routing"
	if c.Runtime.Plan.Baseline.Verification.Kind == "http" {
		c.VerificationLevel = "reachability"
		c.Conditions[2] = domain.Condition{Type: "RouteVerified", Status: false, Message: "HTTP reachability does not prove context propagation or override selection"}
	}

	c.Conditions = append(c.Conditions, domain.Condition{Type: "IngressReachable", Status: true, Message: "baseline and composition ingress probes passed"})
	c.LastError = nil
	c.LatestOperation.Status = "succeeded"
	c.LatestOperation.Error = nil
	// The current selection is the published routing selection after its complete
	// aggregate snapshot has been written. Readiness remains false until the
	// configured verifier observes it through ingress.
	c.Runtime.PublishedOverrides = cloneOverrides(c.Overrides)
	c.Runtime.Attempts = 0
	c.Runtime.NextAttemptAt = r.now().Add(r.cfg.Interval)
	setReady(c, true)
	return nil
}

func overridesEqual(a, b map[string]domain.ComponentOverride) bool { return reflect.DeepEqual(a, b) }

func (r *Reconciler) retireEndpointFree(ctx context.Context, c *domain.Composition) (bool, error) {
	if !overridesEqual(c.Runtime.PublishedOverrides, c.Overrides) {
		if c.Runtime.RetiringWorkloads == nil {
			c.Runtime.RetiringWorkloads = map[string]domain.WorkloadRef{}
		}
		for component := range c.Runtime.PublishedOverrides {
			if _, desired := c.Overrides[component]; desired {
				continue
			}
			if ref := c.Runtime.WorkloadFor(component); ref.Namespace != "" {
				c.Runtime.RetiringWorkloads[component] = ref
				delete(c.Runtime.Workloads, component)
			}
		}
		c.Runtime.PublishedOverrides = cloneOverrides(c.Overrides)
		if err := r.store.SaveObservation(ctx, *c); err != nil {
			return false, err
		}
	}
	for component, ref := range c.Runtime.RetiringWorkloads {
		if err := r.runtime.DeleteWorkload(ctx, ref); err != nil {
			return false, fmt.Errorf("retire %s: %w", component, err)
		}
		absent, err := r.runtime.WorkloadAbsent(ctx, ref)
		if err != nil {
			return false, fmt.Errorf("confirm retirement for %s: %w", component, err)
		}
		if !absent {
			c.Conditions = append(c.Conditions, domain.Condition{Type: "RetiringWorkloads", Message: "waiting for retired workload deletion"})
			return true, nil
		}
		delete(c.Runtime.RetiringWorkloads, component)
	}
	return false, nil
}

func planForOverrides(plan domain.ResolvedPlan, overrides map[string]domain.ComponentOverride) domain.ResolvedPlan {
	profiles := plan.Profiles()
	plan.Components = map[string]domain.Component{}
	for component := range overrides {
		if profile, ok := profiles[component]; ok {
			plan.Components[component] = profile
		}
	}

	plan.Component = domain.Component{}
	return plan
}

func cloneOverrides(in map[string]domain.ComponentOverride) map[string]domain.ComponentOverride {
	out := make(map[string]domain.ComponentOverride, len(in))
	maps.Copy(out, in)
	return out
}

func endpointHost(c domain.Composition) (string, error) {
	u, err := url.Parse(c.Endpoints["public"].URL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("composition has no valid allocated endpoint")
	}

	return u.Hostname(), nil
}

// Snapshot makes ingress publication and mesh route retention separate decisions
// so deletion can close new requests before draining and removing the mesh entry.
func Snapshot(compositions []domain.Composition) (domain.RouteSnapshot, error) {
	compositions = slices.Clone(compositions)
	slices.SortFunc(compositions, func(a, b domain.Composition) int { return strings.Compare(a.ID, b.ID) })
	snapshot := domain.RouteSnapshot{OwnedCompositions: map[string]string{}}
	for _, c := range compositions {
		snapshot.OwnedCompositions[c.ID] = c.Runtime.OwnershipToken
		if c.Runtime.Plan == nil {
			return snapshot, fmt.Errorf("composition has no resolved catalog plan")
		}

		plan := c.Runtime.Plan
		profiles := plan.Profiles()
		selection := c.Runtime.PublishedOverrides
		if selection == nil {
			selection = c.Overrides
		}

		names := domain.OverrideNames(selection)
		for _, component := range names {
			profile, ok := profiles[component]
			if !ok || profile.ID != component {
				return snapshot, fmt.Errorf("missing profile for %s", component)
			}
			if !profile.HasEndpoint() {
				continue
			}

			d := plan.Baseline.RouteDomain(component)
			snapshot.Domains = append(snapshot.Domains, d)
			if !c.Runtime.RoutingActive || c.Runtime.RoutesRemoved || c.Phase == domain.PhaseDestroyed {
				continue
			}

			ref := c.Runtime.WorkloadFor(component)
			if ref.Namespace != domain.NamespaceForID(c.ID) || ref.Service != component {
				return snapshot, fmt.Errorf("active routing intent has no valid workload reference for %s", component)
			}

			snapshot.MeshEntries = append(snapshot.MeshEntries, domain.RouteEntry{Domain: d, CompositionID: c.ID, DestinationHost: ref.Service + "." + ref.Namespace + ".svc.cluster.local", Port: profile.Port, OwnershipToken: c.Runtime.OwnershipToken})
		}

		if !c.Runtime.RoutingActive || c.Runtime.RoutesRemoved || c.Phase == domain.PhaseDestroyed || c.DeletionRequested {
			continue
		}

		host, err := endpointHost(c)
		if err != nil {
			return snapshot, err
		}

		entryComponent := plan.Baseline.Routing.EntryComponent
		binding := plan.Baseline.Components[entryComponent]
		entry := domain.RouteEntry{MessageIsolation: c.MessageIsolation, Domain: plan.Baseline.RouteDomain(entryComponent), CompositionID: c.ID, Host: host, DestinationHost: binding.ServiceHost, Port: binding.Port, OwnershipToken: c.Runtime.OwnershipToken}
		if profile, ok := profiles[entryComponent]; ok {
			ref := c.Runtime.WorkloadFor(entryComponent)
			entry.DestinationHost = ref.Service + "." + ref.Namespace + ".svc.cluster.local"
			entry.Port = profile.Port
		}

		snapshot.IngressEntries = append(snapshot.IngressEntries, entry)
	}

	return snapshot, nil
}

var errRoutesPending = errors.New("waiting for route controller acceptance")

func (r *Reconciler) syncRoutes(ctx context.Context) error {
	r.routeMu.Lock()
	defer r.routeMu.Unlock()
	if err := r.guard(ctx); err != nil {
		return err
	}

	all, err := r.store.Active(ctx)
	if err != nil {
		return fmt.Errorf("load aggregate routes: %w", err)
	}

	snapshot, err := Snapshot(all)
	if err != nil {
		return err
	}

	if r.routeCycle != nil && r.routeCycle.applied != nil && reflect.DeepEqual(*r.routeCycle.applied, snapshot) {
		return nil
	}

	if r.routeCycle != nil {
		// A failed reconciliation may have partially changed routing; no earlier
		// snapshot remains safe to reuse after attempting a different one.
		r.routeCycle.applied = nil
	}

	observed, err := r.routes.Reconcile(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("configure routes: %w", err)
	}

	if !observed.Ready {
		return fmt.Errorf("%w: %s", errRoutesPending, observed.Message)
	}

	if r.routeCycle != nil {
		r.routeCycle.applied = &snapshot
	}

	return nil
}

func (r *Reconciler) destroy(ctx context.Context, c *domain.Composition) error {
	c.Phase = domain.PhaseDestroying
	setReady(c, false)
	c.LatestOperation.Status = "running"
	if err := r.syncRoutes(ctx); err != nil {
		return err
	}

	if c.Runtime.RoutingActive && !c.Runtime.RoutesRemoved {
		host, err := endpointHost(*c)
		if err != nil {
			return err
		}

		if err := r.verifier.Absent(ctx, host); err != nil {
			return err
		}

		if c.Runtime.DrainUntil == nil {
			until := r.now().Add(r.cfg.DrainTimeout)
			c.Runtime.DrainUntil = &until
			c.LastError = nil
			return nil
		}

		if r.now().Before(*c.Runtime.DrainUntil) {
			return nil
		}

		c.Runtime.RoutesRemoved = true
		if err := r.store.SaveObservation(ctx, *c); err != nil {
			return err
		}

		if err := r.syncRoutes(ctx); err != nil {
			return err
		}
	}

	// The namespace is the cleanup unit for all overrides. Require recorded
	// namespace identities to agree before deleting it once.
	ref := domain.WorkloadRef{Namespace: domain.NamespaceForID(c.ID), OwnershipToken: c.Runtime.OwnershipToken}
	owned := map[string]domain.WorkloadRef{}
	maps.Copy(owned, c.Runtime.Workloads)
	maps.Copy(owned, c.Runtime.RetiringWorkloads)
	for _, observed := range owned {
		if observed.Namespace == "" {
			continue
		}

		if observed.Namespace != ref.Namespace || observed.OwnershipToken != ref.OwnershipToken || (ref.NamespaceUID != "" && observed.NamespaceUID != "" && ref.NamespaceUID != observed.NamespaceUID) {
			return fmt.Errorf("workload namespace ownership identities disagree")
		}

		if observed.NamespaceUID != "" {
			ref.NamespaceUID = observed.NamespaceUID
		}
	}

	if err := r.runtime.Delete(ctx, ref); err != nil {
		return fmt.Errorf("delete owned workload: %w", err)
	}

	absent, err := r.runtime.Absent(ctx, ref)
	if err != nil {
		return fmt.Errorf("confirm workload cleanup: %w", err)
	}

	if !absent {
		c.Runtime.NextAttemptAt = r.now().Add(r.cfg.Interval)
		return nil
	}

	if c.MessageIsolation {
		if r.cfg.Messaging == nil || c.Runtime.Plan == nil {
			return fmt.Errorf("Pub/Sub cleanup requires provider and resolved plan")
		}

		absent, err := r.cfg.Messaging.Delete(ctx, domain.MessagingSpec{CompositionID: c.ID, OwnershipToken: c.Runtime.OwnershipToken, Subscriptions: c.Runtime.Plan.MessageSubscriptions})
		if err != nil {
			return err
		}

		if !absent {
			return nil
		}

		for i := range c.MessageSubscriptions {
			c.MessageSubscriptions[i].Ready = false
		}
	}

	c.Phase = domain.PhaseDestroyed
	c.LastError = nil
	c.Runtime.Attempts = 0
	c.LatestOperation.Status = "succeeded"
	c.LatestOperation.Error = nil
	c.Conditions = []domain.Condition{{Type: "WorkloadsReady", Status: false, Message: "owned workloads removed"}, {Type: "RoutesConfigured", Status: false, Message: "composition routes removed"}, {Type: "RouteVerified", Status: false, Message: "composition endpoint withdrawn"}}
	return nil
}

func setReady(c *domain.Composition, ready bool) {
	if !ready {
		c.VerificationLevel = "none"
	}

	for key, endpoint := range c.Endpoints {
		endpoint.Ready = ready
		c.Endpoints[key] = endpoint
	}
}

func (r *Reconciler) failure(c *domain.Composition, err error) {
	if !c.DeletionRequested {
		c.Phase = domain.PhaseFailed
	}

	setReady(c, false)
	message := err.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}

	code := "reconciliation_failed"
	if classified, ok := errors.AsType[interface {
		error
		ReconciliationCode() string
	}](err); ok {
		code = classified.ReconciliationCode()
	}

	c.LastError = &domain.Error{Code: code, Message: message, Retryable: true, Composition: c.ID}
	if c.LatestOperation.Status != "succeeded" {
		c.LatestOperation.Status = "failed"
		c.LatestOperation.Error = c.LastError
	}

	c.Runtime.Attempts++
	shift := min(c.Runtime.Attempts-1, 5)
	delay := time.Second * time.Duration(1<<shift)
	// Capped backoff with jitter remains persisted across process restarts.
	delay += time.Duration(rand.Int64N(int64(delay/4) + 1))
	c.Runtime.NextAttemptAt = r.now().Add(min(delay, 30*time.Second))
}

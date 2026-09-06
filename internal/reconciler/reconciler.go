// Package reconciler converges persisted composition intent with provider state.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
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
	Verify(context.Context, string, string, string, domain.ResolvedPlan) (verification.Result, error)
	Absent(context.Context, string) error
}

type Config struct {
	Interval         time.Duration
	ProvisionTimeout time.Duration
	DrainTimeout     time.Duration
}

type Reconciler struct {
	store    Store
	runtime  Runtime
	routes   domain.RoutingProvider
	verifier Verifier
	guard    func(context.Context) error
	log      *slog.Logger
	cfg      Config
	now      func() time.Time
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
	return &Reconciler{store: store, runtime: runtime, routes: routes, verifier: verifier, guard: guard, log: logger, cfg: cfg, now: time.Now}
}

func (r *Reconciler) Run(ctx context.Context) error {
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
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.Runtime.NextAttemptAt.After(r.now()) {
			continue
		}
		if err := r.guard(ctx); err != nil {
			return fmt.Errorf("check leadership: %w", err)
		}
		before := c.Phase
		stepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := r.step(stepCtx, &c)
		cancel()
		if errors.Is(err, domain.ErrStaleObservation) {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Do not publish an observation from a worker that lost its session lock.
		if guardErr := r.guard(ctx); guardErr != nil {
			return fmt.Errorf("leadership lost: %w", guardErr)
		}
		if err != nil {
			r.failure(&c, err)
			r.log.Warn("composition reconcile failed", "composition", c.ID, "phase", c.Phase, "error", err)
		} else if !c.Runtime.NextAttemptAt.After(r.now()) {
			c.Runtime.NextAttemptAt = r.now().Add(r.cfg.Interval)
		}
		c.ObservedGeneration = c.Generation
		if err := r.store.SaveObservation(ctx, c); err != nil {
			if errors.Is(err, domain.ErrStaleObservation) {
				continue
			}
			return fmt.Errorf("save composition observation: %w", err)
		}
		if before != c.Phase {
			r.log.Info("composition phase changed", "composition", c.ID, "from", before, "to", c.Phase)
		}
	}
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
	if len(c.Overrides) == 0 {
		return fmt.Errorf("persisted composition has no overrides")
	}
	if c.Runtime.Plan == nil {
		return fmt.Errorf("persisted composition has no resolved catalog plan")
	}
	if c.Runtime.Workloads == nil {
		c.Runtime.Workloads = make(map[string]domain.WorkloadRef)
	}

	allReady := true
	var anyObservation domain.WorkloadObservation
	var firstWorkloadID string

	for compID, override := range c.Overrides {
		profile, ok := c.Runtime.Plan.Components[compID]
		if !ok {
			profile = c.Runtime.Plan.Component
		}
		ref, err := r.runtime.Ensure(ctx, domain.WorkloadSpec{
			CompositionID:  c.ID,
			ProjectID:      c.Project,
			ComponentID:    compID,
			Image:          override.Image,
			OwnershipToken: c.Runtime.OwnershipToken,
			Profile:        profile,
		})
		if ref.Namespace != "" {
			c.Runtime.Workload = ref
			c.Runtime.Workloads[compID] = ref
		}
		if err != nil {
			return fmt.Errorf("ensure component %s: %w", compID, err)
		}
		obs, err := r.runtime.Observe(ctx, ref)
		if err != nil {
			return fmt.Errorf("observe component %s: %w", compID, err)
		}
		anyObservation = obs
		if firstWorkloadID == "" {
			firstWorkloadID = obs.WorkloadID
		}

		state := domain.ComponentObservation{
			Source:     "override",
			Status:     "provisioning",
			Image:      override.Image,
			WorkloadID: obs.WorkloadID,
		}
		if obs.Image != "" {
			state.Image = obs.Image
		}
		c.Components[compID] = state

		if !obs.Ready {
			allReady = false
			if obs.Failed || r.now().Sub(startedAt) >= r.cfg.ProvisionTimeout {
				return fmt.Errorf("component %s is not ready: %s", compID, obs.Message)
			}
		}
	}

	c.Conditions = []domain.Condition{
		{Type: "WorkloadsReady", Status: allReady, Message: "workloads ready"},
		{Type: "RoutesConfigured", Status: false},
		{Type: "RouteVerified", Status: false},
	}
	if !allReady {
		c.Conditions[0].Message = anyObservation.Message
		if c.Runtime.RoutingActive {
			if err := r.syncRoutes(ctx); err != nil {
				return err
			}
			c.Conditions[1].Status = true
		}
		c.LastError = nil
		c.Runtime.Attempts = 0
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
	c.Conditions[1].Status = true
	host, err := endpointHost(*c)
	if err != nil {
		return err
	}
	verified, err := r.verifier.Verify(ctx, c.ID, host, firstWorkloadID, *c.Runtime.Plan)
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
		if _, isOverride := c.Overrides[hop.Service]; !isOverride {
			old.Source = "baseline"
		}
		c.Components[hop.Service] = old
	}
	c.Phase = domain.PhaseReady
	c.Conditions[2].Status = true
	c.Conditions[2].Message = "override pod and shared baseline hops observed through ingress"
	c.LastError = nil
	c.LatestOperation.Status = "succeeded"
	c.LatestOperation.Error = nil
	c.Runtime.Attempts = 0
	c.Runtime.NextAttemptAt = r.now().Add(r.cfg.Interval)
	setReady(c, true)
	return nil
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
	snapshot := domain.RouteSnapshot{OwnedCompositions: map[string]string{}}
	for _, c := range compositions {
		snapshot.OwnedCompositions[c.ID] = c.Runtime.OwnershipToken
		if c.Runtime.Plan == nil {
			return snapshot, fmt.Errorf("composition has no resolved catalog plan")
		}
		plan := c.Runtime.Plan
		if len(c.Overrides) == 0 && plan.Component.ID != "" {
			d := plan.Baseline.RouteDomain(plan.Component.ID)
			snapshot.Domains = append(snapshot.Domains, d)
		}
		for compID := range c.Overrides {
			d := plan.Baseline.RouteDomain(compID)
			snapshot.Domains = append(snapshot.Domains, d)
		}
		if !c.Runtime.RoutingActive || c.Runtime.RoutesRemoved || c.Phase == domain.PhaseDestroyed {
			continue
		}
		host, err := endpointHost(c)
		if err != nil {
			return snapshot, err
		}

		for compID := range c.Overrides {
			d := plan.Baseline.RouteDomain(compID)
			ref, ok := c.Runtime.Workloads[compID]
			if !ok || ref.Namespace == "" || ref.Service == "" {
				ref = c.Runtime.Workload
			}
			if ref.Namespace == "" || ref.Service == "" {
				return snapshot, fmt.Errorf("active routing intent has no workload reference for %s", compID)
			}
			profile, ok := plan.Components[compID]
			if !ok {
				profile = plan.Component
			}
			entry := domain.RouteEntry{
				Domain:          d,
				CompositionID:   c.ID,
				Host:            host,
				DestinationHost: ref.Service + "." + ref.Namespace + ".svc.cluster.local",
				Port:            profile.Port,
				OwnershipToken:  c.Runtime.OwnershipToken,
			}
			snapshot.MeshEntries = append(snapshot.MeshEntries, entry)
		}

		if !c.DeletionRequested {
			entryComp := plan.Baseline.Routing.EntryComponent
			var ingressEntry domain.RouteEntry
			if _, isOverridden := c.Overrides[entryComp]; isOverridden {
				ref, ok := c.Runtime.Workloads[entryComp]
				if !ok || ref.Namespace == "" || ref.Service == "" {
					ref = c.Runtime.Workload
				}
				profile, ok := plan.Components[entryComp]
				if !ok {
					profile = plan.Component
				}
				ingressEntry = domain.RouteEntry{
					Domain:          plan.Baseline.RouteDomain(entryComp),
					CompositionID:   c.ID,
					Host:            host,
					DestinationHost: ref.Service + "." + ref.Namespace + ".svc.cluster.local",
					Port:            profile.Port,
					OwnershipToken:  c.Runtime.OwnershipToken,
				}
			} else {
				binding := plan.Baseline.Components[entryComp]
				ingressEntry = domain.RouteEntry{
					Domain:          plan.Baseline.RouteDomain(entryComp),
					CompositionID:   c.ID,
					Host:            host,
					DestinationHost: binding.ServiceHost,
					Port:            binding.Port,
					OwnershipToken:  c.Runtime.OwnershipToken,
				}
			}
			snapshot.IngressEntries = append(snapshot.IngressEntries, ingressEntry)
		}
	}
	return snapshot, nil
}

func (r *Reconciler) syncRoutes(ctx context.Context) error {
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
	observed, err := r.routes.Reconcile(ctx, snapshot)
	if err != nil {
		return fmt.Errorf("configure routes: %w", err)
	}
	if !observed.Ready {
		return fmt.Errorf("routes not configured: %s", observed.Message)
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
	ref := c.Runtime.Workload
	if ref.Namespace == "" {
		for _, w := range c.Runtime.Workloads {
			if w.Namespace != "" {
				ref = w
				break
			}
		}
	}
	if ref.Namespace == "" {
		ref = domain.WorkloadRef{Namespace: domain.NamespaceForID(c.ID), OwnershipToken: c.Runtime.OwnershipToken}
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
	c.Phase = domain.PhaseDestroyed
	c.LastError = nil
	c.Runtime.Attempts = 0
	c.LatestOperation.Status = "succeeded"
	c.LatestOperation.Error = nil
	c.Conditions = []domain.Condition{{Type: "WorkloadsReady", Status: false, Message: "owned workloads removed"}, {Type: "RoutesConfigured", Status: false, Message: "composition routes removed"}, {Type: "RouteVerified", Status: false, Message: "composition endpoint withdrawn"}}
	return nil
}

func setReady(c *domain.Composition, ready bool) {
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
	c.LastError = &domain.Error{Code: "reconciliation_failed", Message: message, Retryable: true, Composition: c.ID}
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

package reconciler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/dblooman/envy/demo/protocol"
	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/verification"
)

type memoryStore struct {
	records map[string]domain.Composition
	phases  []domain.Phase
}

func clone(c domain.Composition) domain.Composition {
	b, _ := json.Marshal(c)
	var out domain.Composition
	_ = json.Unmarshal(b, &out)
	runtimeJSON, _ := json.Marshal(c.Runtime)
	_ = json.Unmarshal(runtimeJSON, &out.Runtime)
	out.DeletionRequested = c.DeletionRequested
	return out
}
func (s *memoryStore) Active(context.Context) ([]domain.Composition, error) {
	var out []domain.Composition
	for _, c := range s.records {
		if c.Phase != domain.PhaseDestroyed {
			out = append(out, clone(c))
		}
	}

	return out, nil
}
func (s *memoryStore) SaveObservation(_ context.Context, c domain.Composition) error {
	stored := s.records[c.ID]
	if stored.Generation != c.Generation || stored.DeletionRequested != c.DeletionRequested {
		return domain.ErrStaleObservation
	}

	s.records[c.ID] = clone(c)
	s.phases = append(s.phases, c.Phase)
	return nil
}
func (*memoryStore) Expire(context.Context, time.Time) error { return nil }

type memoryRuntime struct {
	ready, failed, absent bool
	created               map[string]bool
	deletes               int
	onEnsure              func()
}

func (m *memoryRuntime) Ensure(_ context.Context, s domain.WorkloadSpec) (domain.WorkloadRef, error) {
	m.created[s.CompositionID] = true
	if m.onEnsure != nil {
		m.onEnsure()
	}

	return domain.WorkloadRef{Namespace: domain.NamespaceForID(s.CompositionID), NamespaceUID: "namespace-uid", Deployment: s.ComponentID, Service: s.ComponentID, OwnershipToken: s.OwnershipToken}, nil
}
func (m *memoryRuntime) Observe(context.Context, domain.WorkloadRef) (domain.WorkloadObservation, error) {
	return domain.WorkloadObservation{Ready: m.ready, Failed: m.failed, WorkloadID: "override-pod", Message: "observed"}, nil
}
func (m *memoryRuntime) Delete(context.Context, domain.WorkloadRef) error { m.deletes++; return nil }
func (m *memoryRuntime) DeleteWorkload(context.Context, domain.WorkloadRef) error {
	m.deletes++
	return nil
}
func (m *memoryRuntime) WorkloadAbsent(context.Context, domain.WorkloadRef) (bool, error) {
	return m.absent, nil
}
func (m *memoryRuntime) Absent(context.Context, domain.WorkloadRef) (bool, error) {
	return m.absent, nil
}

type memoryRoutes struct {
	pending bool
	last    domain.RouteSnapshot
	calls   int
}

func (m *memoryRoutes) Reconcile(_ context.Context, s domain.RouteSnapshot) (domain.RouteObservation, error) {
	m.last = s
	m.calls++
	return domain.RouteObservation{Ready: !m.pending, Message: "waiting for Accepted"}, nil
}

type memoryVerifier struct {
	err     error
	missing bool
}

func (m *memoryVerifier) Verify(_ context.Context, id, host string, pods map[string]string, plan domain.ResolvedPlan) (verification.Result, error) {
	return verification.Result{Composition: []protocol.Hop{{Service: "gateway", WorkloadID: "gateway-pod"}, {Service: "service-a", WorkloadID: "a-pod"}, {Service: "service-b", WorkloadID: pods["service-b"]}}}, m.err
}
func (m *memoryVerifier) Absent(context.Context, string) error {
	if !m.missing {
		return errors.New("ingress still forwarding")
	}

	return nil
}

func fixture(now time.Time) domain.Composition {
	return domain.Composition{ID: "a", Project: "demo", Baseline: "staging", Generation: 1, Phase: domain.PhaseCreated, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "image:v2"}}, Components: map[string]domain.ComponentObservation{}, Endpoints: map[string]domain.Endpoint{"public": {URL: "http://cmp-a.envy.localhost:8080"}}, LatestOperation: domain.Operation{ID: "op", Kind: "create", Status: "pending"}, Runtime: domain.RuntimeState{OwnershipToken: "owner-a", Plan: testPlan()}}
}

func setup(t *testing.T) (*Reconciler, *memoryStore, *memoryRuntime, *memoryRoutes, *memoryVerifier, *time.Time) {
	t.Helper()
	now := time.Now().UTC()
	store := &memoryStore{records: map[string]domain.Composition{"a": fixture(now)}}
	runtime := &memoryRuntime{ready: true, created: map[string]bool{}}
	routes := &memoryRoutes{}
	verifier := &memoryVerifier{missing: true}
	r := New(store, runtime, routes, verifier, func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{Interval: time.Second, ProvisionTimeout: time.Minute, DrainTimeout: 10 * time.Second})
	r.now = func() time.Time { return now }
	return r, store, runtime, routes, verifier, &now
}

func tick(t *testing.T, r *Reconciler) {
	t.Helper()
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessDoesNotFlapAndRestartReusesWorkloads(t *testing.T) {
	r, store, runtime, _, verifier, now := setup(t)
	verifier.err = errors.New("proxy has not converged")
	tick(t, r)
	if c := store.records["a"]; c.Phase != domain.PhaseProvisioning || c.Endpoints["public"].Ready || !c.Runtime.RoutingActive {
		t.Fatalf("premature status: %+v", c)
	}

	verifier.err = nil
	*now = now.Add(2 * time.Second)
	// New process, same persisted desired/observed state.
	restarted := New(store, runtime, r.routes, verifier, r.guard, r.log, r.cfg)
	restarted.now = r.now
	tick(t, restarted)
	if store.records["a"].Phase != domain.PhaseReady || len(runtime.created) != 1 {
		t.Fatal("restart did not recover the existing workload")
	}

	store.phases = nil
	*now = now.Add(2 * time.Second)
	tick(t, restarted)
	for _, phase := range store.phases {
		if phase != domain.PhaseReady {
			t.Fatalf("unchanged ready composition published %s", phase)
		}
	}
}

func TestDeletionWithdrawsIngressThenDrainsThenRemovesWorkload(t *testing.T) {
	r, store, runtime, routes, verifier, now := setup(t)
	tick(t, r)
	c := store.records["a"]
	c.DeletionRequested = true
	c.Generation++
	c.Phase = domain.PhaseDestroying
	c.LatestOperation = domain.Operation{ID: "delete-op", Kind: "destroy", Status: "pending"}
	c.Runtime.NextAttemptAt = time.Time{}
	store.records["a"] = c
	verifier.missing = false
	tick(t, r)
	if runtime.deletes != 0 || len(routes.last.IngressEntries) != 0 || len(routes.last.MeshEntries) != 1 {
		t.Fatal("removed workload before ingress withdrawal")
	}

	verifier.missing = true
	*now = now.Add(31 * time.Second)
	tick(t, r)
	if runtime.deletes != 0 || store.records["a"].Runtime.DrainUntil == nil {
		t.Fatal("request drain was not persisted")
	}

	*now = now.Add(2 * time.Second)
	tick(t, r)
	if runtime.deletes != 0 {
		t.Fatal("deleted before request drain deadline")
	}

	*now = now.Add(10 * time.Second)
	tick(t, r)
	if runtime.deletes != 1 || len(routes.last.MeshEntries) != 0 || store.records["a"].Phase == domain.PhaseDestroyed {
		t.Fatal("cleanup must await observed namespace absence")
	}

	runtime.absent = true
	*now = now.Add(2 * time.Second)
	tick(t, r)
	if store.records["a"].Phase != domain.PhaseDestroyed {
		t.Fatal("absence did not complete destruction")
	}
}

func TestUnavailableOverrideRetainsSelectedRoute(t *testing.T) {
	r, store, runtime, routes, _, now := setup(t)
	tick(t, r)
	runtime.ready = false
	runtime.failed = true
	*now = now.Add(2 * time.Second)
	tick(t, r)
	c := store.records["a"]
	if c.Phase != domain.PhaseFailed || c.Endpoints["public"].Ready || len(routes.last.MeshEntries) != 1 || len(routes.last.IngressEntries) != 1 {
		t.Fatalf("failed override was not retained: %+v", c)
	}
}

func TestLateObservationCannotUndoDeletion(t *testing.T) {
	r, store, runtime, _, _, _ := setup(t)
	runtime.onEnsure = func() {
		c := store.records["a"]
		c.Generation++
		c.DeletionRequested = true
		c.Phase = domain.PhaseDestroying
		store.records["a"] = c
	}
	tick(t, r)
	if c := store.records["a"]; !c.DeletionRequested || c.Generation != 2 || c.Phase != domain.PhaseDestroying {
		t.Fatal("stale result overwrote deletion")
	}
}

func TestLostLeadershipStopsBeforeAnyProviderCall(t *testing.T) {
	r, _, runtime, routes, _, _ := setup(t)
	r.guard = func(context.Context) error { return domain.ErrNotLeader }
	if err := r.Tick(context.Background()); !errors.Is(err, domain.ErrNotLeader) {
		t.Fatalf("lost lock: %v", err)
	}

	if len(runtime.created) != 0 || routes.calls != 0 {
		t.Fatal("mutated without leadership")
	}
}

func TestSnapshotPreservesDeletionOwnershipAfterRoutesRemoved(t *testing.T) {
	a := fixture(time.Now())
	a.Runtime.RoutingActive = true
	a.Runtime.RoutesRemoved = true
	a.DeletionRequested = true
	b := fixture(time.Now())
	b.ID = "b"
	b.Runtime.OwnershipToken = "owner-b"
	b.Runtime.RoutingActive = true
	b.Runtime.Workload = domain.WorkloadRef{Namespace: "envy-b", Service: "service-b"}
	b.Endpoints["public"] = domain.Endpoint{URL: "http://cmp-b.envy.localhost:8080"}
	s, err := Snapshot([]domain.Composition{a, b})
	if err != nil {
		t.Fatal(err)
	}

	if len(s.MeshEntries) != 1 || s.MeshEntries[0].CompositionID != "b" || len(s.IngressEntries) != 1 || s.OwnedCompositions["a"] != "owner-a" {
		t.Fatalf("bad snapshot: %+v", s)
	}
}

func TestUpdateHasFreshProvisioningWindowAndRetainsRoutes(t *testing.T) {
	r, store, runtime, routes, verifier, now := setup(t)
	tick(t, r)
	c := store.records["a"]
	c.Generation = 2
	c.Phase = domain.PhaseUpdating
	c.CreatedAt = now.Add(-time.Hour)
	c.Runtime.ProvisionStartedAt = *now
	c.Runtime.NextAttemptAt = time.Time{}
	c.Overrides["service-b"] = domain.ComponentOverride{Image: "image:v3"}
	c.LatestOperation = domain.Operation{ID: "update-op", Kind: "update", Status: "pending"}
	store.records["a"] = c
	runtime.ready = false
	tick(t, r)
	if got := store.records["a"]; got.Phase != domain.PhaseUpdating || got.Endpoints["public"].Ready || len(routes.last.MeshEntries) != 1 || len(routes.last.IngressEntries) != 1 {
		t.Fatalf("update lost routes or used create timeout: %+v", got)
	}

	runtime.ready = true
	verifier.err = errors.New("old pod still observed")
	*now = now.Add(2 * time.Second)
	tick(t, r)
	if store.records["a"].Phase != domain.PhaseUpdating {
		t.Fatal("proxy convergence prematurely failed update")
	}

	verifier.err = nil
	*now = now.Add(2 * time.Second)
	restarted := New(store, runtime, routes, verifier, r.guard, r.log, r.cfg)
	restarted.now = r.now
	tick(t, restarted)
	got := store.records["a"]
	if got.Phase != domain.PhaseReady || got.ObservedGeneration != 2 || got.LatestOperation.Status != "succeeded" || got.Endpoints["public"].URL != c.Endpoints["public"].URL {
		t.Fatalf("update failed to recover: %+v", got)
	}
}

func TestRemovingFinalOverridePublishesBaselineThenRetiresWorkload(t *testing.T) {
	r, store, runtime, routes, _, now := setup(t)
	tick(t, r)
	c := store.records["a"]
	c.Generation = 2
	c.Phase = domain.PhaseUpdating
	c.Overrides = map[string]domain.ComponentOverride{}
	c.LatestOperation = domain.Operation{ID: "remove-op", Kind: "update", Status: "pending"}
	c.Runtime.ProvisionStartedAt = *now
	c.Runtime.NextAttemptAt = time.Time{}
	store.records["a"] = c
	tick(t, r)
	got := store.records["a"]
	if got.Phase != domain.PhaseUpdating || len(routes.last.MeshEntries) != 0 || len(routes.last.IngressEntries) != 1 || got.Runtime.RetiringWorkloads["service-b"].Service != "service-b" {
		t.Fatalf("removal did not publish baseline-only route and retain workload: %+v", got)
	}

	*now = now.Add(11 * time.Second)
	tick(t, r)
	if runtime.deletes == 0 || store.records["a"].Phase != domain.PhaseUpdating {
		t.Fatal("retirement did not wait for observed workload absence")
	}

	runtime.absent = true
	*now = now.Add(time.Second)
	tick(t, r)
	got = store.records["a"]
	if got.Phase != domain.PhaseReady || len(got.Runtime.RetiringWorkloads) != 0 || len(got.Runtime.PublishedOverrides) != 0 {
		t.Fatalf("final inheritance did not complete: %+v", got)
	}
}

func TestSnapshotForZeroOverridesKeepsPreviewIngressOnBaseline(t *testing.T) {
	c := fixture(time.Now())
	c.Runtime.RoutingActive = true
	c.Runtime.PublishedOverrides = map[string]domain.ComponentOverride{}
	c.Overrides = map[string]domain.ComponentOverride{}
	snapshot, err := Snapshot([]domain.Composition{c})
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.MeshEntries) != 0 || len(snapshot.IngressEntries) != 1 || snapshot.IngressEntries[0].DestinationHost != "gateway.envy-baseline.svc.cluster.local" {
		t.Fatalf("zero override snapshot did not target baseline entry: %+v", snapshot)
	}
}

func TestLateObservationCannotUndoUpdate(t *testing.T) {
	r, store, runtime, _, _, _ := setup(t)
	runtime.onEnsure = func() {
		c := store.records["a"]
		c.Generation++
		c.Phase = domain.PhaseUpdating
		c.Overrides["service-b"] = domain.ComponentOverride{Image: "image:v3"}
		store.records["a"] = c
	}
	tick(t, r)
	if c := store.records["a"]; c.Generation != 2 || c.Phase != domain.PhaseUpdating || c.Overrides["service-b"].Image != "image:v3" {
		t.Fatal("stale result overwrote update")
	}
}

func testPlan() *domain.ResolvedPlan {
	return &domain.ResolvedPlan{Component: domain.Component{ID: "service-b", Port: 8080}, Baseline: domain.Baseline{Routing: domain.BaselineRouting{Namespace: "envy-baseline", Gateway: "envy-preview", EntryComponent: "gateway"}, Components: map[string]domain.BaselineBinding{"service-b": {ServiceHost: "service-b.envy-baseline.svc.cluster.local", Port: 8080}, "gateway": {ServiceHost: "gateway.envy-baseline.svc.cluster.local", Port: 8080}}}}
}

func TestPendingRoutesAreObservedAgainWithoutFailingOperation(t *testing.T) {
	r, store, runtime, routes, _, now := setup(t)
	routes.pending = true
	tick(t, r)
	c := store.records["a"]
	if c.LastError != nil || c.LatestOperation.Status == "failed" || c.Endpoints["public"].Ready {
		t.Fatalf("pending routes published incorrect state: %+v", c)
	}

	calls := routes.calls
	*now = now.Add(2 * time.Second)
	tick(t, r)
	if routes.calls <= calls {
		t.Fatal("pending routes were cached as complete")
	}

	routes.pending = false
	*now = now.Add(2 * time.Second)
	tick(t, r)
	if store.records["a"].Phase != domain.PhaseReady {
		t.Fatal("accepted routes did not progress through verification")
	}

	c = store.records["a"]
	c.DeletionRequested = true
	c.Generation++
	store.records["a"] = c
	routes.pending = true
	*now = now.Add(2 * time.Second)
	tick(t, r)
	if runtime.deletes != 0 {
		t.Fatal("workload retired before route removal was observed")
	}
}

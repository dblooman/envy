package reconciler

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/verification"
)

type multiRuntime struct {
	*memoryRuntime
	unhealthy string
	ensured   []domain.WorkloadSpec
}

func (m *multiRuntime) Ensure(ctx context.Context, s domain.WorkloadSpec) (domain.WorkloadRef, error) {
	m.ensured = append(m.ensured, s)
	return m.memoryRuntime.Ensure(ctx, s)
}

func (m *multiRuntime) Observe(_ context.Context, ref domain.WorkloadRef) (domain.WorkloadObservation, error) {
	return domain.WorkloadObservation{Ready: ref.Service != m.unhealthy, Failed: ref.Service == m.unhealthy, WorkloadID: ref.Service + "-pod", Message: "observed"}, nil
}

type multiVerifier struct {
	calls int
	pods  map[string]string
}

func (v *multiVerifier) Verify(_ context.Context, _, _ string, pods map[string]string, _ domain.ResolvedPlan) (verification.Result, error) {
	v.calls++
	v.pods = pods
	if len(pods) != 2 {
		return verification.Result{}, errors.New("missing pod evidence")
	}

	return verification.Result{}, nil
}
func (v *multiVerifier) Absent(context.Context, string) error { return nil }
func multiFixture(now time.Time) domain.Composition {
	c := fixture(now)
	c.Overrides["service-a"] = domain.ComponentOverride{Image: "a:v2"}
	c.Runtime.Plan.Components = map[string]domain.Component{"service-b": c.Runtime.Plan.Component, "service-a": {ID: "service-a", Port: 9090}}
	c.Runtime.Plan.Baseline.Components["service-a"] = domain.BaselineBinding{ServiceHost: "service-a.envy-baseline.svc.cluster.local", Port: 8080}
	return c
}

func TestAllWorkloadsMustBeReadyAndAllRoutesSurviveFailure(t *testing.T) {
	r, store, base, routes, _, now := setup(t)
	store.records["a"] = multiFixture(*now)
	runtime := &multiRuntime{memoryRuntime: base, unhealthy: "service-a"}
	verifier := &multiVerifier{}
	r.runtime = runtime
	r.verifier = verifier
	tick(t, r)
	if store.records["a"].Phase == domain.PhaseReady || routes.calls != 0 || verifier.calls != 0 || len(store.records["a"].Runtime.Workloads) != 2 {
		t.Fatal("partial readiness published a preview or lost inventory")
	}

	runtime.unhealthy = ""
	*now = now.Add(31 * time.Second)
	tick(t, r)
	if store.records["a"].Phase != domain.PhaseReady || len(routes.last.MeshEntries) != 2 || len(routes.last.IngressEntries) != 1 || len(verifier.pods) != 2 {
		t.Fatal("did not publish and verify every override")
	}

	if runtime.ensured[0].ComponentID != "service-a" || runtime.ensured[0].WorkloadCount != 2 {
		t.Fatal("workloads are not planned deterministically with shared quota capacity")
	}

	runtime.unhealthy = "service-b"
	*now = now.Add(time.Second)
	tick(t, r)
	if store.records["a"].Phase != domain.PhaseFailed || len(routes.last.MeshEntries) != 2 || len(routes.last.IngressEntries) != 1 {
		t.Fatal("unhealthy workload dropped an explicit override route")
	}
}

func TestWorkloadRuntimeReceivesDesiredComponentsAndRetainedQuotaSnapshots(t *testing.T) {
	r, store, base, _, _, now := setup(t)
	c := fixture(*now)
	c.Runtime.Plan.Previews = map[string]domain.PreviewSnapshot{"service-b": {Revision: 1}, "removed": {Revision: 2}}
	store.records[c.ID] = c
	runtime := &multiRuntime{memoryRuntime: base}
	r.runtime = runtime
	tick(t, r)
	if len(runtime.ensured) != 1 || !slices.Equal(runtime.ensured[0].DesiredComponents, []string{"service-b"}) || len(runtime.ensured[0].Previews) != 2 {
		t.Fatalf("runtime did not distinguish desired policy from retained quota snapshots: %+v", runtime.ensured)
	}

	if len(store.records[c.ID].Runtime.Plan.Previews) != 2 {
		t.Fatal("reconciliation discarded historical preview approval")
	}
}

func TestMultipleCleanupRemovesEveryRouteBeforeNamespace(t *testing.T) {
	r, store, base, routes, _, now := setup(t)
	store.records["a"] = multiFixture(*now)
	r.runtime = &multiRuntime{memoryRuntime: base}
	r.verifier = &multiVerifier{}
	tick(t, r)
	c := store.records["a"]
	c.DeletionRequested = true
	c.Generation++
	c.Runtime.NextAttemptAt = time.Time{}
	store.records["a"] = c
	tick(t, r)
	if len(routes.last.IngressEntries) != 0 || len(routes.last.MeshEntries) != 2 || base.deletes != 0 {
		t.Fatal("cleanup did not retain both routes during drain")
	}

	*now = now.Add(11 * time.Second)
	base.absent = true
	tick(t, r)
	if store.records["a"].Phase != domain.PhaseDestroyed || base.deletes != 1 || len(routes.last.MeshEntries) != 0 || len(routes.last.Domains) != 2 {
		t.Fatal("namespace removed before all domain entries, or deleted more than once")
	}
}

func TestEntryOverrideUsesItsOwnService(t *testing.T) {
	c := multiFixture(time.Now())
	c.Runtime.Plan.Components["gateway"] = domain.Component{ID: "gateway", Port: 7070}
	c.Overrides["gateway"] = domain.ComponentOverride{Image: "g:v2"}
	c.Runtime.RoutingActive = true
	c.Runtime.Workloads = map[string]domain.WorkloadRef{}
	for name := range c.Overrides {
		c.Runtime.Workloads[name] = domain.WorkloadRef{Namespace: "envy-a", Service: name}
	}

	snapshot, err := Snapshot([]domain.Composition{c})
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.MeshEntries) != 3 || len(snapshot.IngressEntries) != 1 || snapshot.IngressEntries[0].DestinationHost != "gateway.envy-a.svc.cluster.local" || snapshot.IngressEntries[0].Port != 7070 {
		t.Fatal("preview did not resolve its overridden entry component")
	}
}

func TestHTTPReadyReportsReachabilityWithoutRoutingProof(t *testing.T) {
	r, store, base, _, _, now := setup(t)
	c := multiFixture(*now)
	c.Runtime.Plan.Baseline.Verification = domain.VerificationContract{Kind: "http", Path: "/products", ExpectedStatus: 200}
	store.records["a"] = c
	r.runtime = &multiRuntime{memoryRuntime: base}
	r.verifier = &multiVerifier{}
	tick(t, r)
	got := store.records["a"]
	if got.Phase != domain.PhaseReady || got.VerificationLevel != "reachability" || !got.Endpoints["public"].Ready {
		t.Fatalf("missing HTTP readiness: %+v", got)
	}

	reachable := false
	for _, condition := range got.Conditions {
		if condition.Type == "RouteVerified" && condition.Status {
			t.Fatal("reachability claimed routing proof")
		}

		if condition.Type == "IngressReachable" {
			reachable = condition.Status
		}
	}

	if !reachable {
		t.Fatal("missing explicit ingress evidence")
	}

	r.runtime = &multiRuntime{memoryRuntime: base, unhealthy: "service-a"}
	*now = now.Add(time.Second)
	tick(t, r)
	if store.records["a"].VerificationLevel != "none" || store.records["a"].Endpoints["public"].Ready {
		t.Fatal("unhealthy workload retained readiness evidence")
	}
}

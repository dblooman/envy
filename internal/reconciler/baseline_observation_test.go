package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type observedBaselineRuntime struct {
	*memoryRuntime
	fingerprint string
	unavailable bool
}

func (m *observedBaselineRuntime) ObserveBaseline(_ context.Context, baseline domain.Baseline, _ map[string]domain.ComponentOverride) (domain.BaselineObservation, error) {
	out := domain.BaselineObservation{Installation: "synthetic", Project: baseline.Project, Baseline: baseline.ID, State: "current", Fingerprint: m.fingerprint, Components: map[string]domain.BaselineComponentObservation{}}
	if m.unavailable {
		return out, errors.New("synthetic observation outage")
	}

	return out, nil
}

func TestBaselineChangeOutageAndLateVerificationStayUnproven(t *testing.T) {
	r, store, runtime, _, verifier, now := setup(t)
	observed := &observedBaselineRuntime{memoryRuntime: runtime, fingerprint: "baseline-one"}
	r.runtime = observed
	tick(t, r)
	first := store.records["a"]
	if first.Phase != domain.PhaseReady || len(store.evidence) != 1 || store.evidence[0].BaselineFingerprint != "baseline-one" || domain.EvidenceFreshness(first, store.evidence[0]) != "current" {
		t.Fatalf("initial verification lacks baseline identity: %+v %+v", first, store.evidence)
	}

	observed.fingerprint = "baseline-two"
	*now = now.Add(2 * time.Second)
	tick(t, r)
	second := store.records["a"]
	if second.Phase != domain.PhaseReady || len(store.evidence) != 2 || domain.EvidenceFreshness(second, store.evidence[0]) != "stale" || domain.EvidenceFreshness(second, store.evidence[1]) != "current" {
		t.Fatalf("baseline drift did not preserve history and refresh proof: %+v %+v", second, store.evidence)
	}

	verifier.onVerify = func() { observed.fingerprint = "baseline-three" }
	*now = now.Add(2 * time.Second)
	tick(t, r)
	late := store.records["a"]
	if late.Phase != domain.PhaseProvisioning || late.Endpoints["public"].Ready || len(store.evidence) != 2 || domain.EvidenceFreshness(late, store.evidence[1]) != "stale" {
		t.Fatalf("late result restored stale proof: %+v %+v", late, store.evidence)
	}

	verifier.onVerify = nil
	observed.unavailable = true
	*now = now.Add(2 * time.Second)
	tick(t, r)
	outage := store.records["a"]
	if outage.BaselineObservation.State != "unavailable" || outage.Endpoints["public"].Ready || domain.EvidenceFreshness(outage, store.evidence[1]) != "unavailable" {
		t.Fatalf("outage was treated as current evidence: %+v", outage)
	}

	observed.unavailable = false
	*now = now.Add(2 * time.Second)
	restarted := New(store, observed, r.routes, verifier, r.guard, r.log, r.cfg)
	restarted.now = r.now
	tick(t, restarted)
	recovered := store.records["a"]
	if recovered.Phase != domain.PhaseReady || len(store.evidence) != 3 || domain.EvidenceFreshness(recovered, store.evidence[2]) != "current" {
		t.Fatalf("restart did not refresh baseline proof: %+v %+v", recovered, store.evidence)
	}
}

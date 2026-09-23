package domain

import (
	"testing"
	"time"
)

func TestDiagnosisRetainsIndependentObservedBlockers(t *testing.T) {
	now := time.Now().UTC()
	c := Composition{ID: "synthetic", Generation: 2, Phase: PhaseFailed, UpdatedAt: now, Components: map[string]ComponentObservation{
		"api":    {Source: "override", Status: "failed", ExecutionState: ExecutionFailed},
		"worker": {Source: "override", Status: "ready"},
	}, Conditions: []Condition{
		{Type: "WorkloadReady/api", Message: "image could not be pulled"},
		{Type: "MessagingReady", Message: "isolated subscription denied"},
		{Type: "RoutesConfigured", Message: "waiting for workload"},
	}}
	d := Diagnose(c, nil)
	if d.State != "blocked" || len(d.Blockers) != 2 || d.Blockers[0].Code != "workload_failed" || d.Blockers[1].Code != "messaging_pending" {
		t.Fatalf("independent observations lost or downstream route misreported: %+v", d)
	}

	if d.Blockers[0].Scope != "component/api" || d.Blockers[0].ObservedAt != now {
		t.Fatalf("finding lacks scope or observation time: %+v", d.Blockers[0])
	}
}

func TestDiagnosisDoesNotTurnReachabilityIntoRoutingProof(t *testing.T) {
	c := Composition{ID: "synthetic", Phase: PhaseReady, VerificationLevel: "reachability", Conditions: []Condition{{Type: "RouteVerified", Message: "HTTP reached ingress"}}}
	d := Diagnose(c, nil)
	if d.State != "healthy" || len(d.Blockers) != 0 || len(d.Notes) != 2 || d.Notes[1].Code != "reachability_only" {
		t.Fatalf("reachability mislabeled as failed routing: %+v", d)
	}
}

func TestDiagnosisSeparatesCleanupAndStaleEvidence(t *testing.T) {
	now := time.Now().UTC()
	old := VerificationEvidence{ID: "1", Generation: 1, Outcome: "passed", BaselineFingerprint: "old", ContractFingerprint: "contract", LastCheckedAt: now}
	c := Composition{ID: "synthetic", Generation: 2, Phase: PhaseProvisioning, UpdatedAt: now, BaselineObservation: &BaselineObservation{State: "unavailable", ObservedAt: now}}
	d := Diagnose(c, &old)
	if d.State != "blocked" || d.Verification != "stale" || len(d.Blockers) != 2 {
		t.Fatalf("unavailable baseline or stale result hidden: %+v", d)
	}

	c.Phase = PhaseDestroying
	c.LastError = &Error{Code: "unavailable", Message: "could not confirm absence"}
	d = Diagnose(c, &old)
	if d.State != "cleanup" || len(d.Blockers) != 1 || d.Blockers[0].Code != "cleanup_failed" {
		t.Fatalf("cleanup confused with serving readiness: %+v", d)
	}
}

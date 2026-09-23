package domain

import (
	"testing"
	"time"
)

func TestEvidenceFreshnessChecksContractAndSelectedWorkloadIdentity(t *testing.T) {
	contract := VerificationContract{Kind: "http", Path: "/health", ExpectedStatus: 200}
	c := Composition{Generation: 2, BaselineObservation: &BaselineObservation{Installation: "synthetic", Project: "demo", Baseline: "staging", State: "current", Fingerprint: "baseline", ObservedAt: time.Now().UTC()}, Runtime: RuntimeState{Plan: &ResolvedPlan{Baseline: Baseline{Verification: contract}}}, Overrides: map[string]ComponentOverride{"api": {Image: "example/api:v2"}}, Components: map[string]ComponentObservation{"api": {Image: "example/api:v2", WorkloadID: "pod-two"}}}
	evidence := VerificationEvidence{Generation: 2, Outcome: "passed", BaselineFingerprint: "baseline", BaselineScope: "synthetic/demo/staging", ContractFingerprint: VerificationContractFingerprint(contract), Workloads: map[string]VerificationWorkload{"api": {Image: "example/api:v2", WorkloadID: "pod-two"}}}
	if got := EvidenceFreshness(c, evidence); got != "current" {
		t.Fatalf("matching evidence not current: %s", got)
	}

	plan := c.Runtime.Plan
	c.Runtime.Plan = nil
	if got := EvidenceFreshness(c, evidence); got != "unknown_coverage" {
		t.Fatalf("missing current contract was accepted: %s", got)
	}

	c.Runtime.Plan = plan

	c.Runtime.Plan.Baseline.Verification.Path = "/other"
	if got := EvidenceFreshness(c, evidence); got != "stale" {
		t.Fatalf("changed contract retained current proof: %s", got)
	}

	c.Runtime.Plan.Baseline.Verification = contract
	c.Components["api"] = ComponentObservation{Image: "example/api:v2", WorkloadID: "pod-three"}
	if got := EvidenceFreshness(c, evidence); got != "stale" {
		t.Fatalf("changed selected pod retained current proof: %s", got)
	}

	c.Components["api"] = ComponentObservation{Image: "example/api:v2", WorkloadID: "pod-two"}
	evidence.Workloads = nil
	if got := EvidenceFreshness(c, evidence); got != "unknown_coverage" {
		t.Fatalf("missing workload proof promoted: %s", got)
	}
}

func TestEvidenceFreshnessDoesNotCertifyUnknownInheritedImage(t *testing.T) {
	c := Composition{Generation: 1, BaselineObservation: &BaselineObservation{Installation: "synthetic", Project: "demo", Baseline: "staging", State: "current", Fingerprint: "baseline", ObservedAt: time.Now().UTC(), Components: map[string]BaselineComponentObservation{"api": {ServiceUID: "service", ExecutionState: "observed", ImageIdentity: "unknown"}}}}
	evidence := VerificationEvidence{Generation: 1, Outcome: "passed", BaselineFingerprint: "baseline", BaselineScope: "synthetic/demo/staging", ContractFingerprint: "contract"}
	if got := EvidenceFreshness(c, evidence); got != "unknown_coverage" {
		t.Fatalf("unknown inherited execution certified: %s", got)
	}

	c.BaselineObservation.Components["api"] = BaselineComponentObservation{ServiceUID: "service", ExecutionState: "unknown", ImageIdentity: "unknown"}
	if got := EvidenceFreshness(c, evidence); got != "unknown_coverage" {
		t.Fatalf("unmatched inherited execution certified: %s", got)
	}
}

func TestEvidenceFreshnessExpiresDisconnectedObservation(t *testing.T) {
	now := time.Date(2026, time.September, 23, 12, 0, 0, 0, time.UTC)
	c := Composition{Generation: 1, BaselineObservation: &BaselineObservation{Installation: "local", Project: "demo", Baseline: "staging", State: "current", Fingerprint: "fingerprint", ObservedAt: now}, Runtime: RuntimeState{Plan: &ResolvedPlan{}}}
	evidence := VerificationEvidence{Generation: 1, Outcome: "passed", BaselineFingerprint: "fingerprint", BaselineScope: "local/demo/staging", ContractFingerprint: VerificationContractFingerprint(VerificationContract{})}
	if got := EvidenceFreshnessAt(c, evidence, now.Add(BaselineObservationMaxAge-time.Second)); got != "current" {
		t.Fatalf("fresh observation expired early: %s", got)
	}

	if got := EvidenceFreshnessAt(c, evidence, now.Add(BaselineObservationMaxAge+time.Second)); got != "unavailable" {
		t.Fatalf("disconnected observation remained current: %s", got)
	}

	c.BaselineObservation.ObservedAt = now.Add(BaselineObservationMaxAge + time.Second)
	if got := EvidenceFreshnessAt(c, evidence, now.Add(BaselineObservationMaxAge+time.Second)); got != "current" {
		t.Fatalf("recovered observation did not restore proof: %s", got)
	}

	evidence.BaselineScope = "other/demo/staging"
	if got := EvidenceFreshnessAt(c, evidence, now.Add(BaselineObservationMaxAge+time.Second)); got != "unknown_coverage" {
		t.Fatalf("wrong installation evidence was accepted: %s", got)
	}
}

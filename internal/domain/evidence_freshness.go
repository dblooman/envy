package domain

import "time"

const BaselineObservationMaxAge = 2 * time.Minute

// EvidenceFreshness is derived from the current composition observation.
// Legacy records without a fingerprint remain inspectable with unknown coverage.
func EvidenceFreshness(composition Composition, evidence VerificationEvidence) string {
	return EvidenceFreshnessAt(composition, evidence, time.Now().UTC())
}

// EvidenceFreshnessAt allows deterministic checks of observation expiry.
func EvidenceFreshnessAt(composition Composition, evidence VerificationEvidence, now time.Time) string {
	if evidence.BaselineFingerprint == "" || evidence.ContractFingerprint == "" {
		return "unknown_coverage"
	}

	if evidence.Generation != composition.Generation {
		return "stale"
	}

	if baseline := baselineEvidenceFreshness(composition, evidence, now); baseline != "" {
		return baseline
	}

	if composition.Runtime.Plan == nil {
		return "unknown_coverage"
	}

	if evidence.ContractFingerprint != VerificationContractFingerprint(composition.Runtime.Plan.Baseline.Verification) {
		return "stale"
	}

	if identity := selectedWorkloadFreshness(composition, evidence); identity != "" {
		return identity
	}

	if evidence.Outcome != "passed" {
		return "failed"
	}

	return "current"
}

func baselineEvidenceFreshness(composition Composition, evidence VerificationEvidence, now time.Time) string {
	observed := composition.BaselineObservation
	if observed == nil || observed.State != "current" || observed.ObservedAt.IsZero() || now.Sub(observed.ObservedAt) > BaselineObservationMaxAge {
		return "unavailable"
	}

	if evidence.BaselineScope == "" || evidence.BaselineScope != observed.Installation+"/"+observed.Project+"/"+observed.Baseline {
		return "unknown_coverage"
	}

	if evidence.BaselineFingerprint != observed.Fingerprint {
		return "stale"
	}

	if !BaselineCoverageKnown(*observed) {
		return "unknown_coverage"
	}

	return ""
}

func selectedWorkloadFreshness(composition Composition, evidence VerificationEvidence) string {
	for name, selected := range composition.Overrides {
		checked, ok := evidence.Workloads[name]
		if !ok || checked.Image == "" || checked.WorkloadID == "" {
			return "unknown_coverage"
		}

		observed, ok := composition.Components[name]
		if !ok || observed.Image != checked.Image || observed.WorkloadID != checked.WorkloadID || selected.Image != checked.Image {
			return "stale"
		}
	}

	return ""
}

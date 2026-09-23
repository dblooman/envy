package domain

// EvidenceFreshness is derived from the current composition observation.
// Legacy records without a fingerprint remain inspectable with unknown coverage.
func EvidenceFreshness(composition Composition, evidence VerificationEvidence) string {
	if evidence.BaselineFingerprint == "" || evidence.ContractFingerprint == "" {
		return "unknown_coverage"
	}

	if evidence.Generation != composition.Generation {
		return "stale"
	}

	if composition.BaselineObservation == nil || composition.BaselineObservation.State != "current" {
		return "unavailable"
	}

	if evidence.BaselineFingerprint != composition.BaselineObservation.Fingerprint {
		return "stale"
	}

	if evidence.Outcome != "passed" {
		return "failed"
	}

	return "current"
}

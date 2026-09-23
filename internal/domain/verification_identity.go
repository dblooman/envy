package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func VerificationContractFingerprint(contract VerificationContract) string {
	body, _ := json.Marshal(contract)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// BaselineCoverageKnown only describes the registered Services and inherited
// workload identities. It does not certify Secrets or external dependencies.
func BaselineCoverageKnown(observation BaselineObservation) bool {
	for _, component := range observation.Components {
		if component.ServiceUID == "" || component.ExecutionState == "unknown" {
			return false
		}

		if component.ExecutionState == "observed" && component.ImageIdentity != "known" {
			return false
		}
	}

	return true
}

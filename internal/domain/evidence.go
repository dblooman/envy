package domain

import "time"

// VerificationEvidence records what a specific check observed, not inferred serving state.
type VerificationEvidence struct {
	BaselineFingerprint string                          `json:"baseline_fingerprint,omitempty"`
	BaselineScope       string                          `json:"baseline_scope,omitempty"`
	ContractFingerprint string                          `json:"contract_fingerprint,omitempty"`
	Workloads           map[string]VerificationWorkload `json:"workloads,omitempty"`
	Freshness           string                          `json:"freshness,omitempty"`
	ID                  string                          `json:"id"`
	Composition         string                          `json:"composition"`
	Generation          int64                           `json:"generation"`
	Kind                string                          `json:"kind"`
	Outcome             string                          `json:"outcome"`
	FirstCheckedAt      time.Time                       `json:"first_checked_at"`
	LastCheckedAt       time.Time                       `json:"last_checked_at"`
	Probes              []VerificationProbe             `json:"probes"`
	Hops                []VerificationHop               `json:"hops"`
	Error               *Error                          `json:"error,omitempty"`
}
type VerificationWorkload struct {
	Image      string `json:"image,omitempty"`
	WorkloadID string `json:"workload_id,omitempty"`
}
type VerificationProbe struct {
	Target         string `json:"target"`
	ExpectedStatus int    `json:"expected_status"`
	ObservedStatus int    `json:"observed_status"`
}
type VerificationHop struct {
	Service               string `json:"service"`
	Version               string `json:"version"`
	Composition           string `json:"composition"`
	WorkloadID            string `json:"workload_id"`
	DeploymentComposition string `json:"deployment_composition"`
}
type VerificationPage struct {
	Items      []VerificationEvidence `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}
type ObservabilityTemplate struct {
	Project string `json:"project"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
}
type ObservabilityLink struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	URL   string `json:"url"`
}
type ObservabilityLinks struct {
	Items []ObservabilityLink `json:"items"`
}

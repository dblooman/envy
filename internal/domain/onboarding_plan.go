package domain

// PlanningBlocker is actionable evidence. Codes are stable even when the
// explanatory message changes.
type PlanningBlocker struct {
	Code       string `json:"code"`
	Component  string `json:"component,omitempty"`
	Source     string `json:"source"`
	Message    string `json:"message"`
	NextAction string `json:"next_action"`
}

type PlannedWorkload struct {
	Component          string       `json:"component"`
	Kind               WorkloadKind `json:"kind"`
	Image              string       `json:"image"`
	Immutable          bool         `json:"immutable"`
	BuildID            string       `json:"build_id,omitempty"`
	ProfileRevision    int64        `json:"profile_revision,omitempty"`
	Dependencies       []string     `json:"dependencies,omitempty"`
	SharedDependencies []string     `json:"shared_dependencies,omitempty"`
	ServiceAccount     string       `json:"service_account,omitempty"`
	PodCPUCeiling      string       `json:"pod_cpu_ceiling,omitempty"`
	PodMemoryCeiling   string       `json:"pod_memory_ceiling,omitempty"`
}

type PreviewPlan struct {
	Ready                    bool                       `json:"ready"`
	Project                  string                     `json:"project"`
	Baseline                 string                     `json:"baseline"`
	BaselineRevision         string                     `json:"baseline_revision,omitempty"`
	Installation             string                     `json:"installation"`
	Namespace                string                     `json:"namespace,omitempty"`
	Gateway                  string                     `json:"gateway,omitempty"`
	Lifetime                 string                     `json:"lifetime,omitempty"`
	VerificationLevel        string                     `json:"verification_level"`
	ResourceDemand           string                     `json:"resource_demand"`
	MessageIsolation         bool                       `json:"message_isolation"`
	Selected                 []PlannedWorkload          `json:"selected"`
	Inherited                map[string]BaselineBinding `json:"inherited"`
	ExpectedPreviewRevisions map[string]int64           `json:"expected_preview_revisions,omitempty"`
	Blockers                 []PlanningBlocker          `json:"blockers"`
	Cautions                 []PlanningBlocker          `json:"cautions"`
}

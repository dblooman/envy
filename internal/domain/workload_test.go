package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

//nolint:wsl_v5 // Table-driven cases are intentionally compact.
func TestWorkloadExecutionValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    Component
		ok   bool
	}{
		{"legacy HTTP", Component{ID: "api"}, true},
		{"worker", Component{ID: "worker", Execution: &WorkloadExecution{Kind: WorkloadWorker}}, true},
		{"bounded job", Component{ID: "job", Execution: &WorkloadExecution{Kind: WorkloadJob, Timeout: "5m", RetryLimit: 2}}, true},
		{"bounded schedule", Component{ID: "nightly", Execution: &WorkloadExecution{Kind: WorkloadScheduledJob, Timeout: "5m", Schedule: "0 1 * * *", MaxRuns: 10, ConcurrencyPolicy: "forbid"}}, true},
		{"job has no timeout", Component{ID: "job", Execution: &WorkloadExecution{Kind: WorkloadJob}}, false},
		{"schedule overlaps", Component{ID: "nightly", Execution: &WorkloadExecution{Kind: WorkloadScheduledJob, Timeout: "5m", Schedule: "0 1 * * *", MaxRuns: 10, ConcurrencyPolicy: "allow"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.ValidateExecution(); (err == nil) != tc.ok {
				t.Fatalf("ValidateExecution() = %v", err)
			}
		})
	}
}

func TestValidateExecutionGraph(t *testing.T) {
	ok := map[string]Component{
		"worker": {ID: "worker", Execution: &WorkloadExecution{Kind: WorkloadWorker, Dependencies: []string{"job"}}},
		"job":    {ID: "job", Execution: &WorkloadExecution{Kind: WorkloadJob, Timeout: "1m"}},
	}
	if err := ValidateExecutionGraph(ok); err != nil {
		t.Fatal(err)
	}

	ok["job"] = Component{ID: "job", Execution: &WorkloadExecution{Kind: WorkloadJob, Timeout: "1m", Dependencies: []string{"worker"}}}
	if err := ValidateExecutionGraph(ok); err == nil {
		t.Fatal("dependency cycle accepted")
	}
}

func TestLegacyComponentOmitsExecution(t *testing.T) {
	data, err := json.Marshal(Component{ID: "api", Profile: "http-small", Protocol: "http", Port: 8080})
	if err != nil || strings.Contains(string(data), "execution") {
		t.Fatalf("legacy component compatibility lost: %s (%v)", data, err)
	}
}

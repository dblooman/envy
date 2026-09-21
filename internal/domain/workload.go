package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExecutionID deterministically names the execution for one desired Job input.
// It deliberately excludes the composition generation: an unrelated component
// update must not replace an active execution.
func ExecutionID(component Component, image string) (string, string) {
	payload, _ := json.Marshal(struct {
		Component Component
		Image     string
	}{component, image})
	sum := sha256.Sum256(payload)
	encoded := hex.EncodeToString(sum[:])
	return encoded[:20], encoded
}

// WorkloadKind describes lifecycle semantics, independently of whether a
// workload has an HTTP endpoint. HTTP is retained as the zero-value behavior
// for existing catalog rows.
type WorkloadKind string

const (
	WorkloadHTTP         WorkloadKind = "http"
	WorkloadWorker       WorkloadKind = "worker"
	WorkloadJob          WorkloadKind = "job"
	WorkloadScheduledJob WorkloadKind = "scheduled-job"
)

// ExecutionState is reported per workload. Composition phase remains the
// aggregate view so existing clients can keep using it.
type ExecutionState string

const (
	ExecutionPending   ExecutionState = "pending"
	ExecutionRunning   ExecutionState = "running"
	ExecutionReady     ExecutionState = "ready"
	ExecutionSucceeded ExecutionState = "succeeded"
	ExecutionFailed    ExecutionState = "failed"
	ExecutionSuspended ExecutionState = "suspended"
	ExecutionCancelled ExecutionState = "cancelled"
)

// WorkloadExecution declares bounded automatic execution. Database migrations,
// grants and cloud resource provisioning are intentionally not represented.
type WorkloadExecution struct {
	Kind              WorkloadKind `json:"kind,omitempty"`
	Dependencies      []string     `json:"dependencies,omitempty"`
	Timeout           string       `json:"timeout,omitempty"`
	RetryLimit        int32        `json:"retry_limit,omitempty"`
	Schedule          string       `json:"schedule,omitempty"`
	MaxRuns           int32        `json:"max_runs,omitempty"`
	ConcurrencyPolicy string       `json:"concurrency_policy,omitempty"`
}

// ExecutionRef identifies one provider execution. Generation and SpecHash make
// replacement explicit: an unchanged Job specification is never rerun merely
// because another component changed.
type ExecutionRef struct {
	ID, ProviderID, SpecHash string
	Generation               int64
	Runs                     int32
	State                    ExecutionState
	StartedAt, FinishedAt    time.Time
}

func (c Component) WorkloadKind() WorkloadKind {
	if c.Execution != nil && c.Execution.Kind != "" {
		return c.Execution.Kind
	}

	return WorkloadHTTP
}

func (c Component) HasEndpoint() bool { return c.WorkloadKind() == WorkloadHTTP }

//nolint:gocyclo,wsl_v5 // Each execution kind has intentionally distinct bounds.
func (c Component) ValidateExecution() error {
	if c.Execution == nil {
		return nil
	}
	e := *c.Execution
	kind := c.WorkloadKind()
	switch kind {
	case WorkloadHTTP:
		if e.Timeout != "" || e.RetryLimit != 0 || e.Schedule != "" || e.MaxRuns != 0 || e.ConcurrencyPolicy != "" {
			return Validation("HTTP workloads cannot declare execution scheduling settings")
		}
	case WorkloadWorker:
		if e.Timeout != "" || e.RetryLimit != 0 || e.Schedule != "" || e.MaxRuns != 0 || e.ConcurrencyPolicy != "" {
			return Validation("workers cannot declare finite-job or schedule settings")
		}
	case WorkloadJob:
		if e.Schedule != "" || e.MaxRuns != 0 || e.ConcurrencyPolicy != "" {
			return Validation("jobs cannot declare a schedule")
		}
		if err := validateFiniteExecution(e); err != nil {
			return err
		}
	case WorkloadScheduledJob:
		if err := validateFiniteExecution(e); err != nil {
			return err
		}
		if len(strings.Fields(e.Schedule)) != 5 || len(e.Schedule) > 128 {
			return Validation("scheduled jobs require a bounded five-field cron schedule")
		}
		if e.MaxRuns < 1 || e.MaxRuns > 1000 || e.ConcurrencyPolicy != "forbid" {
			return Validation("scheduled jobs require max_runs of 1–1000 and forbid concurrency")
		}
	default:
		return Validation("execution kind must be http, worker, job, or scheduled-job")
	}

	seen := map[string]bool{}
	for _, dependency := range e.Dependencies {
		if !ValidCatalogID(dependency) || dependency == c.ID || seen[dependency] {
			return Validation("execution dependencies must be distinct component IDs other than self")
		}
		seen[dependency] = true
	}
	return nil
}

func validateFiniteExecution(e WorkloadExecution) error {
	d, err := time.ParseDuration(e.Timeout)
	if err != nil || d < time.Second || d > 24*time.Hour {
		return Validation("finite workloads require a timeout from 1s through 24h")
	}

	if e.RetryLimit < 0 || e.RetryLimit > 10 {
		return Validation("finite workloads require retry_limit from 0 through 10")
	}

	return nil
}

// ValidateExecutionGraph rejects unknown dependency references and cycles before
// execution reaches a provider. All referenced components are required.
//
//nolint:gocognit,wsl_v5 // The two passes deliberately keep validation and traversal separate.
func ValidateExecutionGraph(components map[string]Component) error {
	for id, component := range components {
		if component.Execution == nil {
			if err := component.ValidateExecution(); err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
			continue
		}
		for _, dependency := range component.Execution.Dependencies {
			if _, ok := components[dependency]; !ok {
				return Validation("execution dependency is not registered: " + dependency)
			}
		}
		if err := component.ValidateExecution(); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
	}

	visiting, complete := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return Validation("execution dependencies contain a cycle")
		}
		if complete[id] {
			return nil
		}
		visiting[id] = true
		if components[id].Execution == nil {
			delete(visiting, id)
			complete[id] = true
			return nil
		}
		for _, dependency := range components[id].Execution.Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		complete[id] = true
		return nil
	}
	for id := range components {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

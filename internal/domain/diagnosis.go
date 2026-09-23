package domain

import (
	"sort"
	"strings"
	"time"
)

// Diagnosis is a read-time explanation of recorded observations, not a root-cause claim.
type Diagnosis struct {
	Composition  string              `json:"composition"`
	Generation   int64               `json:"generation"`
	Phase        Phase               `json:"phase"`
	ObservedAt   time.Time           `json:"observed_at"`
	State        string              `json:"state"`
	Blockers     []DiagnosticFinding `json:"blockers"`
	Notes        []DiagnosticFinding `json:"notes"`
	Verification string              `json:"verification"`
	EvidenceID   string              `json:"evidence_id,omitempty"`
}

type DiagnosticFinding struct {
	Code       string    `json:"code"`
	Scope      string    `json:"scope"`
	Message    string    `json:"message"`
	NextStep   string    `json:"next_step"`
	ObservedAt time.Time `json:"observed_at"`
}

// Diagnose reports only facts supported by the composition and optional latest check.
// A condition may be downstream of another finding; ordering is for inspection,
// not a claim that the first finding caused all others.
func Diagnose(c Composition, latest *VerificationEvidence) Diagnosis {
	d := Diagnosis{Composition: c.ID, Generation: c.Generation, Phase: c.Phase, ObservedAt: c.UpdatedAt, State: "unknown", Blockers: []DiagnosticFinding{}, Notes: []DiagnosticFinding{}, Verification: "unknown_coverage"}
	if c.Phase == PhaseDestroying || c.Phase == PhaseDestroyed {
		diagnoseCleanup(c, &d)
		return d
	}

	diagnoseBaseline(c, &d)
	diagnoseEvidence(c, latest, &d)
	diagnoseWorkloads(c, &d)
	diagnoseConditions(c, &d)
	if c.LastError != nil && !findingHasMessage(d.Blockers, c.LastError.Message) {
		d.add("operation_failed", "composition", c.LastError.Message, "Inspect the latest operation and lifecycle events.", c.UpdatedAt)
	}

	switch {
	case len(d.Blockers) > 0:
		d.State = "blocked"
	case c.Phase == PhaseReady || c.Phase == PhaseCompleted || c.Phase == PhaseSuspended:
		d.State = "healthy"
	default:
		d.State = "pending"
	}

	return d
}

func (d *Diagnosis) add(code, scope, message, next string, observed time.Time) {
	d.Blockers = append(d.Blockers, DiagnosticFinding{Code: code, Scope: scope, Message: message, NextStep: next, ObservedAt: observed})
}

func diagnoseCleanup(c Composition, d *Diagnosis) {
	d.State = "cleanup"
	if c.Phase != PhaseDestroying {
		return
	}

	if c.LastError != nil {
		d.add("cleanup_failed", "composition", c.LastError.Message, "Inspect cleanup events and retry the controller operation.", c.UpdatedAt)
	} else {
		d.Notes = append(d.Notes, DiagnosticFinding{Code: "cleanup_pending", Scope: "composition", Message: "Resource removal has not been confirmed.", NextStep: "Wait for the destroyed phase or inspect lifecycle events.", ObservedAt: c.UpdatedAt})
	}
}

func diagnoseBaseline(c Composition, d *Diagnosis) {
	if c.BaselineObservation != nil && c.BaselineObservation.State != "current" {
		d.add("baseline_unavailable", "baseline", "The inherited baseline execution cannot be observed; verification is unavailable.", "Restore baseline observation and wait for a fresh check.", c.BaselineObservation.ObservedAt)
	}
}

func diagnoseEvidence(c Composition, latest *VerificationEvidence, d *Diagnosis) {
	if latest != nil {
		d.EvidenceID = latest.ID
		d.Verification = EvidenceFreshness(c, *latest)
		switch d.Verification {
		case "stale", "unavailable":
			d.add("verification_not_current", "verification", "The latest verification does not cover the currently observed composition.", "Wait for a fresh verification check and inspect its result.", latest.LastCheckedAt)
		case "failed":
			message := "The latest verification check failed."
			if latest.Error != nil && latest.Error.Message != "" {
				message = latest.Error.Message
			}

			d.add("verification_failed", "verification", message, "Inspect the probe result and application logs; a failed check does not identify an application root cause.", latest.LastCheckedAt)
		}
	}

	if d.Verification == "unknown_coverage" {
		d.Notes = append(d.Notes, DiagnosticFinding{Code: "verification_coverage_unknown", Scope: "verification", Message: "Current fingerprint coverage could not be confirmed.", NextStep: "Inspect verification history and wait for a supported fresh check.", ObservedAt: c.UpdatedAt})
	}
}

func diagnoseWorkloads(c Composition, d *Diagnosis) {
	names := make([]string, 0, len(c.Components))
	for name := range c.Components {
		names = append(names, name)
	}

	sort.Strings(names)
	for _, name := range names {
		component := c.Components[name]
		if component.Status == "ready" || component.Status == "succeeded" {
			continue
		}

		if component.Source != "override" && component.ExecutionID == "" {
			continue // inherited baseline status is not an owned workload observation
		}

		code, next := "workload_pending", "Inspect workload events and bounded application logs."
		if component.Status == "failed" || component.ExecutionState == ExecutionFailed {
			code, next = "workload_failed", "Inspect workload events, image access and bounded application logs."
		} else if component.Status == "suspended" || component.ExecutionState == ExecutionSuspended {
			code, next = "workload_suspended", "Inspect execution prerequisites and schedule policy."
		}

		message := "Workload has not reported readiness."
		for _, condition := range c.Conditions {
			if condition.Type == "WorkloadReady/"+name && condition.Message != "" {
				message = condition.Message
				break
			}
		}

		d.add(code, "component/"+name, message, next, c.UpdatedAt)
	}
}

func diagnoseConditions(c Composition, d *Diagnosis) {
	for _, condition := range c.Conditions {
		if condition.Status {
			continue
		}

		switch condition.Type {
		case "MessagingReady":
			d.add("messaging_pending", "messaging", nonempty(condition.Message, "Isolated messaging is not ready."), "Inspect the owned subscription and operator-approved binding.", c.UpdatedAt)
		case "RoutesConfigured":
			if len(d.Blockers) == 0 || !hasWorkloadBlocker(d.Blockers) {
				d.add("routing_pending", "routing", nonempty(condition.Message, "Routes are not configured."), "Inspect route acceptance and destination readiness.", c.UpdatedAt)
			}
		case "RouteVerified":
			if c.Phase == PhaseReady && c.VerificationLevel == "reachability" {
				d.Notes = append(d.Notes, DiagnosticFinding{Code: "reachability_only", Scope: "routing", Message: "HTTP reachability has passed, but override selection and service hops were not observed.", NextStep: "Use a routing-aware verification contract for hop evidence.", ObservedAt: c.UpdatedAt})
			} else if len(d.Blockers) == 0 {
				d.add("verification_pending", "routing", nonempty(condition.Message, "Routing has not been verified."), "Inspect route evidence and rerun verification after prerequisites recover.", c.UpdatedAt)
			}
		}
	}
}

func hasWorkloadBlocker(findings []DiagnosticFinding) bool {
	for _, finding := range findings {
		if strings.HasPrefix(finding.Code, "workload_") {
			return true
		}
	}

	return false
}

func findingHasMessage(findings []DiagnosticFinding, message string) bool {
	for _, finding := range findings {
		if finding.Message == message {
			return true
		}
	}

	return false
}

func nonempty(value, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}

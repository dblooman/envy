package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

// PlanCreate checks the same catalog, image, messaging and approval inputs as
// Create. It allocates nothing and never reserves capacity; Create rechecks.
//
//nolint:gocognit // The review deliberately gathers all selected, inherited and unknown states.
func (s *Service) PlanCreate(ctx context.Context, input domain.CreateRequest) (domain.PreviewPlan, error) {
	plan := domain.PreviewPlan{
		Project: input.Project, Baseline: input.Baseline, Installation: s.cfg.Installation,
		VerificationLevel: "unknown", ResourceDemand: "not_resolved",
		MessageIsolation: input.MessageIsolation, Selected: []domain.PlannedWorkload{},
		Inherited: map[string]domain.BaselineBinding{}, Blockers: []domain.PlanningBlocker{}, Cautions: []domain.PlanningBlocker{},
	}
	req, ttl, err := NormalizeCreate(input, s.cfg)
	if err != nil {
		plan.Blockers = append(plan.Blockers, planningBlocker(err, "", "request"))
		return plan, nil
	}

	plan.Lifetime = ttl.String()
	req.Overrides, err = s.resolveOverrides(ctx, req.Project, req.Overrides)
	if err != nil {
		plan.Blockers = append(plan.Blockers, planningBlocker(err, "", "build registry"))
		return plan, nil
	}

	prepared, err := s.prepareCreate(ctx, req)
	if err != nil {
		plan.Blockers = append(plan.Blockers, planningBlocker(err, matchingComponent(err, req.Overrides), "catalog and source cluster"))
		return plan, nil
	}

	plan.Ready = true
	plan.BaselineRevision = prepared.baseline.Revision
	plan.Namespace = prepared.baseline.Routing.Namespace
	plan.Gateway = prepared.baseline.Routing.Gateway
	plan.VerificationLevel = "none"
	if prepared.baseline.Verification.Kind != "none" {
		plan.VerificationLevel = "reachability"
		if prepared.baseline.Verification.Kind == "envy-chain" {
			plan.VerificationLevel = "routing_when_verified"
		} else {
			plan.Cautions = append(plan.Cautions, domain.PlanningBlocker{Code: "routing_proof_unknown", Source: "verification contract", Message: "HTTP reachability does not demonstrate selected downstream routing.", NextAction: "Run the application acceptance procedure with scoped routing evidence."})
		}
	}

	plan.Cautions = append(plan.Cautions, domain.PlanningBlocker{Code: "shared_side_effects", Source: "baseline contract", Message: "Inherited services and undeclared external dependencies remain shared.", NextAction: "Review stateful side effects and dependency isolation before use."})
	plan.ResourceDemand = fmt.Sprintf("estimated quota ceiling: %d Pods for %d selected workloads including rollout overlap; create enforces current capacity", 2*max(1, len(req.Overrides))+2, len(req.Overrides))
	for id, binding := range prepared.baseline.Components {
		if _, selected := req.Overrides[id]; !selected {
			plan.Inherited[id] = binding
		}
	}

	for _, id := range domain.OverrideNames(req.Overrides) {
		profile := prepared.profiles[id]
		override := req.Overrides[id]
		workload := domain.PlannedWorkload{Component: id, Kind: profile.WorkloadKind(), Image: override.Image, Immutable: validPreviewImage(override.Image), BuildID: override.BuildID}
		if !workload.Immutable {
			plan.Cautions = append(plan.Cautions, domain.PlanningBlocker{Code: "mutable_image_reference", Component: id, Source: "selected image", Message: "The direct image reference is not digest-pinned; it can resolve differently later.", NextAction: "Pin an immutable digest or select a published build for reproducible previews."})
		}

		if profile.Execution != nil {
			workload.Dependencies = append([]string(nil), profile.Execution.Dependencies...)
		}

		if preview, ok := prepared.previews[id]; ok {
			workload.ProfileRevision = preview.Revision
			if preview.CompositePolicy != nil {
				workload.SharedDependencies = append([]string(nil), preview.CompositePolicy.SharedDependencies...)
				workload.ServiceAccount = preview.CompositePolicy.ServiceAccount
				workload.PodCPUCeiling = preview.CompositePolicy.MaxPodCPU
				workload.PodMemoryCeiling = preview.CompositePolicy.MaxPodMemory
				if len(workload.SharedDependencies) > 0 {
					plan.Cautions = append(plan.Cautions, domain.PlanningBlocker{Code: "external_readiness_unknown", Component: id, Source: "operator composite policy", Message: "The plan declares shared external dependencies but does not verify their protocol readiness.", NextAction: "Confirm the operator's external dependency acceptance evidence."})
				}
			}

			if plan.ExpectedPreviewRevisions == nil {
				plan.ExpectedPreviewRevisions = map[string]int64{}
			}

			plan.ExpectedPreviewRevisions[id] = preview.Revision
		}

		plan.Selected = append(plan.Selected, workload)
	}

	return plan, nil
}

func matchingComponent(err error, overrides map[string]domain.ComponentOverride) string {
	for _, name := range domain.OverrideNames(overrides) {
		if strings.Contains(err.Error(), name) || len(overrides) == 1 {
			return name
		}
	}

	return ""
}

func planningBlocker(err error, component, source string) domain.PlanningBlocker {
	blocker := domain.PlanningBlocker{Code: "invalid_request", Component: component, Source: source, Message: err.Error(), NextAction: "Correct the request and review the plan again."}
	if product, ok := errors.AsType[*domain.Error](err); ok {
		blocker.Code, blocker.NextAction = blockerForCode(product.Code)
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "permission") || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized"):
		blocker.Code, blocker.NextAction = "permission_denied", "Grant the named read permission to the installation, then retry."
	case strings.Contains(message, "unsupported") || strings.Contains(message, "does not support"):
		blocker.Code, blocker.NextAction = "unsupported_shape", "Select a supported workload shape or update its approved contract."
	case strings.Contains(message, "requires preview discovery") || strings.Contains(message, "no preview profile"):
		blocker.Code, blocker.NextAction = "approval_required", "Discover the named workload and approve its preview profile."
	case strings.Contains(message, "cannot read secret") || strings.Contains(message, "cannot read configmap"):
		blocker.Code, blocker.NextAction = "permission_denied", "Grant named get permission or provision the missing dependency, then discover again."
	case strings.Contains(message, "ready rollout"):
		blocker.Code, blocker.NextAction = "source_not_ready", "Wait for the named source Deployment rollout to become ready."
	}

	return blocker
}

func blockerForCode(code string) (string, string) {
	switch code {
	case "permission_denied":
		return "permission_denied", "Grant the named source-read permission to the installation, then retry."
	case "not_found":
		return "missing_contract", "Register the named baseline or component, then review the plan again."
	case "conflict":
		return "stale_approval", "Inspect the changed baseline or source workload and obtain fresh approval."
	case "unavailable":
		return "dependency_unknown", "Restore the unavailable integration and retry planning."
	default:
		return "invalid_request", "Correct the request and review the plan again."
	}
}

package application

import (
	"context"
	"regexp"

	"github.com/dblooman/envy/internal/domain"
)

type previewRepository interface {
	PreviewProfile(context.Context, string, string, string) (*domain.PreviewProfile, error)
	ApprovePreview(context.Context, domain.PreviewProfile, int64) (domain.PreviewProfile, error)
}

func (s *Service) DiscoverPreview(ctx context.Context, project, baseline, component string, selection domain.PreviewSelection) (domain.PreviewReport, error) {
	var out domain.PreviewReport
	if s.cfg.PreviewDiscoverer == nil {
		return out, &domain.Error{Code: "unavailable", Message: "preview discovery is unavailable"}
	}

	b, err := s.store.Baseline(ctx, project, baseline)
	if err != nil {
		return out, err
	}

	c, err := s.store.Component(ctx, project, component)
	if err != nil {
		return out, err
	}

	if !c.Overridable {
		return out, domain.Validation("component does not permit overrides")
	}

	if _, ok := b.Components[component]; !ok {
		return out, domain.Validation("component is not bound in baseline")
	}

	report, err := s.cfg.PreviewDiscoverer.DiscoverPreview(ctx, b, c, selection)
	if err != nil {
		return report, err
	}

	for _, message := range report.Blockers {
		finding := planningBlocker(domain.Validation(message), component, "source workload discovery")
		if finding.Code == "invalid_request" {
			finding.Code = "unsupported_shape"
			finding.NextAction = "Adjust the named source workload or its approved preview policy, then discover again."
		}

		report.Findings = append(report.Findings, finding)
	}

	return report, nil
}

func (s *Service) InspectPreview(ctx context.Context, project, baseline, component string) (domain.PreviewProfile, error) {
	r, ok := s.store.(previewRepository)
	if !ok {
		return domain.PreviewProfile{}, domain.NotFound("preview profile not found")
	}

	p, err := r.PreviewProfile(ctx, project, baseline, component)
	if err != nil {
		return domain.PreviewProfile{}, err
	}

	if p == nil {
		return domain.PreviewProfile{}, domain.NotFound("preview profile not found")
	}

	return *p, nil
}

func (s *Service) ApprovePreview(ctx context.Context, project, baseline, component string, a domain.PreviewApproval) (domain.PreviewProfile, error) {
	var zero domain.PreviewProfile
	if !a.ConfirmConnectivity || a.Inspection == "" || a.ExpectedRevision < 0 {
		return zero, domain.Validation("approval requires inspection, expected_revision and confirmed service connectivity")
	}

	report, err := s.DiscoverPreview(ctx, project, baseline, component, a.Selection)
	if err != nil {
		return zero, err
	}

	if report.Inspection != a.Inspection {
		return zero, &domain.Error{Code: "conflict", Message: "source changed since discovery; discover again"}
	}

	if len(report.Blockers) > 0 {
		return zero, domain.Validation("preview discovery has blockers; resolve them before approval")
	}

	r, ok := s.store.(previewRepository)
	if !ok {
		return zero, &domain.Error{Code: "unavailable", Message: "preview profile storage unavailable"}
	}

	return r.ApprovePreview(ctx, domain.PreviewProfile{Project: project, Baseline: baseline, Component: component, Selection: report.Selection, SourceUID: report.Source.UID, Contract: report.Contract, Dependencies: report.Dependencies}, a.ExpectedRevision)
}

func (s *Service) resolvePreview(ctx context.Context, b domain.Baseline, c domain.Component, expected int64) (*domain.PreviewSnapshot, error) {
	r, ok := s.store.(previewRepository)
	if !ok {
		if expected != 0 || domain.IsDeploymentProfile(c.Profile) {
			return nil, domain.Validation("no preview profile exists")
		}

		return nil, nil
	}

	profile, err := r.PreviewProfile(ctx, b.Project, b.ID, c.ID)
	if err != nil {
		return nil, err
	}

	if profile == nil {
		if domain.IsDeploymentProfile(c.Profile) {
			return nil, domain.Validation("deployment-derived component requires preview discovery and approval")
		}

		if expected != 0 {
			return nil, domain.Validation("no preview profile exists")
		}

		return nil, nil
	}

	if expected != 0 && expected != profile.Revision {
		return nil, &domain.Error{Code: "conflict", Message: "preview profile revision changed"}
	}

	report, err := s.DiscoverPreview(ctx, b.Project, b.ID, c.ID, profile.Selection)
	if err != nil {
		return nil, err
	}

	if len(report.Blockers) > 0 || report.Source.UID != profile.SourceUID || report.Contract != profile.Contract {
		return nil, &domain.Error{Code: "conflict", Message: "deployed workload no longer matches approved preview contract; discover and approve again"}
	}

	snapshot := report.Snapshot
	snapshot.Revision = profile.Revision
	return &snapshot, nil
}

var previewImage = regexp.MustCompile(`^[^\s@]+@sha256:[a-f0-9]{64}$`)

func validPreviewImage(image string) bool { return previewImage.MatchString(image) }
func validatePreviewGuards(guards map[string]int64, overrides map[string]domain.ComponentOverride) error {
	for component, revision := range guards {
		if _, ok := overrides[component]; !ok || revision < 1 {
			return domain.Validation("expected_preview_revisions requires selected components and positive revisions")
		}
	}

	return nil
}

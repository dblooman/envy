package application

import (
	"context"
	"github.com/dblooman/envy/internal/domain"
	"slices"
	"strings"
)

type onboardingRepository interface {
	CheckCatalog(context.Context, domain.CatalogManifest) error
	ApplyCatalog(context.Context, domain.CatalogManifest) error
}

func (s *Service) Onboard(ctx context.Context, m domain.CatalogManifest, apply bool) (domain.CatalogReport, error) {
	report := domain.CatalogReport{Warnings: []string{}}
	if m.APIVersion != "envy/v1" {
		return report, domain.Validation("api_version must be envy/v1")
	}
	if !domain.ValidCatalogID(m.Project.ID) || strings.TrimSpace(m.Project.Name) == "" || len(m.Project.Name) > 128 {
		return report, domain.Validation("project requires a DNS-label ID and a name of 1–128 characters")
	}
	if len(m.Components) < 1 || len(m.Components) > 20 || len(m.Components) != len(m.Baseline.Components) {
		return report, domain.Validation("configuration must include exactly the baseline's 1–20 component profiles")
	}
	profiles := map[string]domain.Component{}
	for i, c := range m.Components {
		if c.Project == "" {
			c.Project = m.Project.ID
		}
		if c.Project != m.Project.ID {
			return report, domain.Validation("component project must match configuration project")
		}
		if err := ValidateComponent(c); err != nil {
			return report, err
		}
		if _, ok := profiles[c.ID]; ok {
			return report, domain.Validation("duplicate component profile: " + c.ID)
		}
		if _, ok := m.Baseline.Components[c.ID]; !ok {
			return report, domain.Validation("component has no baseline binding: " + c.ID)
		}
		profiles[c.ID] = c
		m.Components[i] = c
	}
	slices.SortFunc(m.Components, func(a, b domain.Component) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	if m.Baseline.Project == "" {
		m.Baseline.Project = m.Project.ID
	}
	if m.Baseline.Project != m.Project.ID {
		return report, domain.Validation("baseline project must match configuration project")
	}
	b, err := s.validateBaseline(ctx, m.Baseline, profiles)
	if err != nil {
		return report, err
	}
	m.Baseline = b
	repo, ok := s.store.(onboardingRepository)
	if !ok {
		return report, &domain.Error{Code: "unavailable", Message: "catalog onboarding is unavailable", Retryable: true}
	}
	if err = repo.CheckCatalog(ctx, m); err != nil {
		return report, err
	}
	report.Configuration = m
	report.Checks = []domain.Condition{{Type: "ConfigurationValid", Status: true}, {Type: "CatalogCompatible", Status: true}, {Type: "BaselineConnectivity", Status: true, Message: "Ready HTTP Services, sidecars, Gateway host coverage, routing ownership and baseline ingress checked"}}
	if b.Verification.Kind == "http" {
		report.Warnings = append(report.Warnings, "HTTP reachability does not verify context propagation or override selection; run application-specific checks through the composition URL")
	}
	if apply {
		if err = repo.ApplyCatalog(ctx, m); err != nil {
			return report, err
		}
		report.Applied = true
	}
	return report, nil
}

package domain

import (
	"net/url"
	"time"
)

// OnboardingDraft is preparation state, never an approved catalog entry. Its
// schema deliberately excludes environment and selected configuration values.
type OnboardingDraft struct {
	Project       string                      `json:"project"`
	Revision      int64                       `json:"revision"`
	Stage         int                         `json:"stage"`
	Configuration CatalogManifest             `json:"configuration"`
	Selections    map[string]PreviewSelection `json:"selections,omitempty"`
	UpdatedAt     time.Time                   `json:"updated_at,omitempty"`
}

func (d OnboardingDraft) Validate() error {
	if !ValidCatalogID(d.Project) || d.Revision < 0 || d.Stage < 0 || d.Stage > 3 {
		return Validation("draft requires a valid project, revision and stage")
	}

	if d.Configuration.Project.ID != "" && d.Configuration.Project.ID != d.Project {
		return Validation("draft configuration project must match the draft project")
	}

	if d.Configuration.Baseline.Project != "" && d.Configuration.Baseline.Project != d.Project {
		return Validation("draft baseline project must match the draft project")
	}

	if len(d.Configuration.Baseline.PubSub) != 0 {
		return Validation("drafts cannot store messaging configuration")
	}

	if len(d.Configuration.Components) > 20 || len(d.Selections) > 20 {
		return Validation("drafts support at most twenty components")
	}

	if err := d.validateComponents(); err != nil {
		return err
	}

	if err := d.validateSelections(); err != nil {
		return err
	}

	return noCredentialURL(d.Configuration.Baseline.Endpoint)
}

func (d OnboardingDraft) validateComponents() error {
	for _, c := range d.Configuration.Components {
		if c.Project != "" && c.Project != d.Project {
			return Validation("draft component project must match the draft project")
		}

		if len(c.Env) != 0 {
			return Validation("drafts cannot store environment values")
		}

		if err := noCredentialURL(c.Repository); err != nil {
			return err
		}
	}

	return nil
}

func (d OnboardingDraft) validateSelections() error {
	for component, selection := range d.Selections {
		if !ValidCatalogID(component) || (selection.Deployment != "" && !ValidCatalogID(selection.Deployment)) || (selection.Container != "" && !ValidCatalogID(selection.Container)) {
			return Validation("draft selections require valid named resources")
		}

		if len(selection.Env) != 0 || len(selection.ConfigMapKeys) != 0 {
			return Validation("drafts cannot store selected environment or ConfigMap values")
		}
	}

	return nil
}

func noCredentialURL(raw string) error {
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Validation("draft URLs must not contain credentials")
	}

	return nil
}

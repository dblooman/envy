package domain

import "testing"

func TestOnboardingDraftRejectsSecretBearingFields(t *testing.T) {
	base := OnboardingDraft{Project: "shop", Configuration: CatalogManifest{Project: Project{ID: "shop"}, Components: []Component{{ID: "api", Project: "shop"}}}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}

	withEnv := base
	withEnv.Configuration.Components = []Component{{ID: "api", Project: "shop", Env: map[string]string{"TOKEN": "private"}}}
	if withEnv.Validate() == nil {
		t.Fatal("accepted environment values")
	}

	withSelection := base
	withSelection.Selections = map[string]PreviewSelection{"api": {ConfigMapKeys: map[string]map[string]string{"config": {"KEY": "private"}}}}
	if withSelection.Validate() == nil {
		t.Fatal("accepted selected configuration values")
	}

	withURL := base
	withURL.Configuration.Baseline.Endpoint = "https://user:pass@example.test"
	if withURL.Validate() == nil {
		t.Fatal("accepted URL credentials")
	}
}

package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
)

type onboardingAPIFake struct {
	*fakeService
	draft domain.OnboardingDraft
}

func (f *onboardingAPIFake) PlanCreate(_ context.Context, req domain.CreateRequest) (domain.PreviewPlan, error) {
	return domain.PreviewPlan{Ready: true, Project: req.Project, Baseline: req.Baseline, Selected: []domain.PlannedWorkload{}, Inherited: map[string]domain.BaselineBinding{}, Blockers: []domain.PlanningBlocker{}, Cautions: []domain.PlanningBlocker{}}, nil
}

func (f *onboardingAPIFake) GetOnboardingDraft(_ context.Context, project string) (domain.OnboardingDraft, error) {
	if f.draft.Project != project {
		return domain.OnboardingDraft{}, domain.NotFound("draft not found")
	}

	return f.draft, nil
}

func (f *onboardingAPIFake) SaveOnboardingDraft(_ context.Context, draft domain.OnboardingDraft) (domain.OnboardingDraft, error) {
	draft.Revision++
	f.draft = draft
	return draft, nil
}

func (f *onboardingAPIFake) DeleteOnboardingDraft(_ context.Context, _ string, _ int64) error {
	f.draft = domain.OnboardingDraft{}
	return nil
}

func TestOnboardingPlanAndDraftRESTClient(t *testing.T) {
	f := &onboardingAPIFake{fakeService: &fakeService{}}
	server := httptest.NewServer(NewHandler(f, "secret", nil))
	defer server.Close()
	c, err := client.New(server.URL, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := c.PlanCreate(context.Background(), domain.CreateRequest{Project: "shop", Baseline: "staging", Name: "check", Overrides: map[string]domain.ComponentOverride{}})
	if err != nil || !plan.Ready || plan.Project != "shop" || f.createCalls != 0 {
		t.Fatalf("plan caused create or failed: %+v %v", plan, err)
	}

	draft := domain.OnboardingDraft{Project: "shop", Configuration: domain.CatalogManifest{Project: domain.Project{ID: "shop"}}}
	draft, err = c.SaveOnboardingDraft(context.Background(), draft)
	if err != nil || draft.Revision != 1 {
		t.Fatalf("draft save: %+v %v", draft, err)
	}

	loaded, err := c.GetOnboardingDraft(context.Background(), "shop")
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("draft load: %+v %v", loaded, err)
	}

	if err := c.DeleteOnboardingDraft(context.Background(), "shop", 1); err != nil {
		t.Fatal(err)
	}

	if response := request(NewHandler(f, "secret", nil), "PUT", "/v1/projects/shop/onboarding-draft", `{"project":"shop","revision":0,"configuration":{},"secret_data":"private"}`, "secret"); response.Code != 400 {
		t.Fatalf("unknown draft field accepted: %s", response.Body.String())
	}
}

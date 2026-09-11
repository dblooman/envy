package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

type featureService struct {
	*fakeService
	filter domain.ActivityFilter
}

func (s *featureService) Activity(_ context.Context, f domain.ActivityFilter) (domain.ActivityPage, error) {
	s.filter = f
	return domain.ActivityPage{Items: []domain.Activity{{ID: "1", Action: "composition.create", Actor: domain.Principal{Kind: "service", ID: "agent"}, Channel: "mcp", Outcome: "accepted", ResourceType: "composition", ResourceID: "abc"}}}, nil
}
func (s *featureService) Revisions(context.Context, string, string, int) (domain.RevisionsPage, error) {
	return domain.RevisionsPage{Items: []domain.CompositionRevision{{Composition: "abc", Generation: 1}}}, nil
}
func (s *featureService) Revision(context.Context, string, int64) (domain.CompositionRevision, error) {
	return domain.CompositionRevision{Composition: "abc", Generation: 1}, nil
}
func (s *featureService) ExportRecipe(context.Context, application.ExportRecipeRequest) (domain.Recipe, error) {
	return domain.Recipe{APIVersion: domain.RecipeVersion}, nil
}
func (s *featureService) ValidateRecipe(_ context.Context, r domain.Recipe) (domain.Recipe, error) {
	return r, nil
}
func (s *featureService) RecreateRecipe(context.Context, application.RecreateRecipeRequest) (application.RecreateRecipeResult, error) {
	return application.RecreateRecipeResult{Composition: domain.Composition{ID: "abc"}, Bindings: []domain.FrontendBindingView{}, BindingErrors: []string{}}, nil
}

func TestActivityRevisionAndRecipeRoutes(t *testing.T) {
	s := &featureService{fakeService: &fakeService{}}
	h := NewHandler(s, "secret", nil)
	w := request(h, "GET", "/v1/activity?project=demo&actor=agent&action=composition.create&limit=7", "", "secret")
	if w.Code != 200 || s.filter.Project != "demo" || s.filter.Actor != "agent" || s.filter.Limit != 7 {
		t.Fatalf("activity: %d %s %+v", w.Code, w.Body.String(), s.filter)
	}
	w = request(h, "GET", "/v1/compositions/abc/revisions/1", "", "secret")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"generation":1`) {
		t.Fatalf("revision: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/v1/recipes/recreate", `{"recipe":{"api_version":"envy/recipe-v1"},"name":"copy","idempotency_key":"key"}`, "secret")
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"binding_errors":[]`) {
		t.Fatalf("recipe: %d %s", w.Code, w.Body.String())
	}
}

func TestSameOriginSPAAndAPINotFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>Envy</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	h := NewConfiguredHandler(&fakeService{}, AuthConfig{Mode: "none"}, Installation{WebDir: dir}, nil, nil)
	w := request(h, "GET", "/compositions/abc", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Envy") {
		t.Fatalf("SPA: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/missing.js", "", "")
	if w.Code != 404 {
		t.Fatalf("missing asset: %d", w.Code)
	}
	w = request(h, "GET", "/v1/not-real", "", "")
	if w.Code != 404 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API fallback: %d %s", w.Code, w.Body.String())
	}
}

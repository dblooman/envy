package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestRecipeTombstoneExportAndPartialBindingRetry(t *testing.T) {
	ctx := context.Background()
	sha := strings.Repeat("a", 40)
	build := strings.Repeat("b", 64)
	old := domain.Composition{ID: "old", Project: "demo", Baseline: "staging", BaselineRevision: "rev1", Phase: domain.PhaseDestroyed, CreatedAt: time.Now().Add(-2 * time.Hour), Overrides: map[string]domain.ComponentOverride{"service-b": {BuildID: build, Image: "resolved-image", Source: &domain.Build{ID: build}}}, Endpoints: map[string]domain.Endpoint{"public": {URL: "http://old.example", Ready: true}}}
	old.ExpiresAt = old.CreatedAt.Add(time.Hour)
	bindCalls := 0
	createCalls := 0
	bindingName := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing auth")
		}

		switch {
		case r.URL.Path == "/v1/compositions/old":
			json.NewEncoder(w).Encode(old)
		case r.Method == "GET":
			json.NewEncoder(w).Encode(domain.FrontendBindingView{Binding: domain.FrontendBinding{Composition: "old", Frontend: "web", Project: "demo", Revision: sha, Repository: "https://example.com/web", URL: "https://old.example", Check: &domain.FrontendCheck{Status: "passed"}}})
		case r.Method == "POST":
			createCalls++
			var request domain.CreateRequest
			json.NewDecoder(r.Body).Decode(&request)
			if r.Header.Get("Idempotency-Key") != "resume-key" || request.ExpectedBaselineRevision != "rev1" || request.Overrides["service-b"].Source != nil || request.Overrides["service-b"].Image != "" {
				t.Errorf("invalid create: %+v", request)
			}

			json.NewEncoder(w).Encode(domain.Composition{ID: "new", Phase: domain.PhaseCreated, Project: "demo", Overrides: request.Overrides})
		case r.Method == "PUT":
			bindCalls++
			if bindingName != "" && bindingName != r.URL.Path {
				t.Error("binding changed on retry")
			}

			bindingName = r.URL.Path
			var req domain.BindFrontendRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Composition != "new" {
				t.Error("old composition rebound")
			}

			if bindCalls == 1 {
				w.WriteHeader(503)
				w.Write([]byte(`{"error":{"code":"unavailable","message":"temporary"}}`))
				return
			}

			json.NewEncoder(w).Encode(domain.FrontendBindingView{Binding: domain.FrontendBinding{Composition: "new"}})
		}
	}))
	defer server.Close()
	c, _ := New(server.URL, "secret", nil)
	recipe, err := c.ExportRecipe(ctx, "old", []domain.RecipeSelection{{Name: "web", Revision: sha}})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(recipe)
	for _, forbidden := range []string{"resolved-image", "http://old", "passed", "workload", "expires_at"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("export leaked %s", forbidden)
		}
	}

	if recipe.TTL != "1h0m0s" || len(recipe.Frontends) != 1 {
		t.Fatalf("wrong intent: %+v", recipe)
	}

	first, err := c.RecreateRecipe(ctx, recipe, "resume", "resume-key")
	if err != nil || first.Composition.ID != "new" || len(first.BindingErrors) != 1 {
		t.Fatalf("lost accepted composition: %+v %v", first, err)
	}

	second, err := c.RecreateRecipe(ctx, recipe, "resume", "resume-key")
	if err != nil || len(second.BindingErrors) != 0 || len(second.Bindings) != 1 || createCalls != 2 {
		t.Fatalf("retry: %+v %v", second, err)
	}

	if _, err := c.RecreateRecipe(ctx, recipe, "resume", ""); err == nil || createCalls != 2 {
		t.Fatal("missing key caused mutations")
	}

	old.Overrides["service-b"] = domain.ComponentOverride{Image: "envy/service-b:v2"}
	if _, err := c.ExportRecipe(ctx, "old", nil); err == nil {
		t.Fatal("mutable tag exported")
	}
}

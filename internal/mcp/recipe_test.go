package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRecipeToolsThroughSDK(t *testing.T) {
	build := strings.Repeat("a", 64)
	recipe := domain.Recipe{APIVersion: domain.RecipeVersion, Project: "demo", Baseline: "staging", BaselineRevision: "rev1", TTL: "1h", Overrides: map[string]domain.ComponentOverride{"service-b": {BuildID: build}}, Frontends: []domain.RecipeFrontend{}}
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		out := domain.Composition{ID: "old", Project: "demo", Baseline: "staging", BaselineRevision: "rev1", Phase: domain.PhaseDestroyed, CreatedAt: time.Unix(0, 0), ExpiresAt: time.Unix(3600, 0), Overrides: recipe.Overrides, Components: map[string]domain.ComponentObservation{}, Endpoints: map[string]domain.Endpoint{}, Conditions: []domain.Condition{}}
		if r.Method == "POST" {
			var in domain.CreateRequest
			json.NewDecoder(r.Body).Decode(&in)
			if in.ExpectedBaselineRevision != "rev1" || r.Header.Get("Idempotency-Key") != "recipe-retry" {
				t.Error("guard or retry key missing")
			}

			out.ID = "new"
			out.Phase = domain.PhaseCreated
		}

		json.NewEncoder(w).Encode(out)
	}))
	defer api.Close()
	c, _ := client.New(api.URL, "secret", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, b := sdk.NewInMemoryTransports()
	server, err := NewServer(c).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	caller, err := sdk.NewClient(&sdk.Implementation{Name: "recipe-test", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	for _, in := range []struct {
		name string
		args any
	}{
		{"export_recipe", ExportRecipeInput{ID: "old"}},
		{"validate_recipe", ValidateRecipeInput{Recipe: recipe}},
		{"recreate_recipe", RecreateRecipeInput{Recipe: recipe, Name: "new", IdempotencyKey: "recipe-retry"}},
	} {
		out, err := caller.CallTool(ctx, &sdk.CallToolParams{Name: in.name, Arguments: in.args})
		if err != nil || out.IsError || out.StructuredContent == nil {
			t.Fatalf("%s: %+v %v", in.name, out, err)
		}
	}

	if calls != 2 {
		t.Fatalf("structural validation should not make API requests: %d", calls)
	}
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestCLIFrontendRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/compositions/lookup":
			commit := r.URL.Query().Get("commit_sha")
			if commit == "found-commit" {
				json.NewEncoder(w).Encode(domain.Composition{
					ID:    "cmp-frontend-test",
					Phase: domain.PhaseReady,
					Endpoints: map[string]domain.Endpoint{
						"public": {URL: "https://cmp-frontend-test.preview.example.com", Ready: true},
					},
				})
				return
			}
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]any{"error": &domain.Error{Code: "not_found", Message: "composition not found"}})

		case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/v1/compositions/cmp-frontend-test"):
			json.NewEncoder(w).Encode(domain.Composition{
				ID:          "cmp-frontend-test",
				Phase:       domain.PhaseReady,
				FrontendURL: "https://my-app.pages.dev",
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	env := func(k string) string {
		return map[string]string{
			"ENVY_API_URL":   server.URL,
			"ENVY_API_TOKEN": "secret",
		}[k]
	}

	// 1. Success case: found composition, outputs environment
	var out, diag bytes.Buffer
	code := Run(context.Background(), []string{
		"frontend", "run",
		"--project", "shop",
		"--commit-sha", "found-commit",
		"--record-frontend-url", "https://my-app.pages.dev",
	}, &out, &diag, env)

	if code != 0 || diag.Len() != 0 {
		t.Fatalf("frontend run failed: code=%d diag=%s", code, &diag)
	}
	var res FrontendResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("bad frontend result json: %v out=%s", err, out.String())
	}
	if res.CompositionID != "cmp-frontend-test" {
		t.Fatalf("expected cmp-frontend-test, got %s", res.CompositionID)
	}
	if res.GraphQLURL != "https://cmp-frontend-test.preview.example.com/graphql" {
		t.Fatalf("expected graphql url, got %s", res.GraphQLURL)
	}
	if res.Environment["VITE_GRAPHQL_URL"] != "https://cmp-frontend-test.preview.example.com/graphql" {
		t.Fatalf("missing VITE_GRAPHQL_URL in environment: %+v", res.Environment)
	}

	// 2. Missing composition case: clear failure without silent fallback
	out.Reset()
	diag.Reset()
	code = Run(context.Background(), []string{
		"frontend", "run",
		"--project", "shop",
		"--commit-sha", "missing-commit",
	}, &out, &diag, env)

	if code == 0 {
		t.Fatalf("expected failure for missing composition, got success: %s", out.String())
	}
	if !strings.Contains(diag.String(), "no active composition found") {
		t.Fatalf("expected clear missing composition diagnostic, got %s", diag.String())
	}
}

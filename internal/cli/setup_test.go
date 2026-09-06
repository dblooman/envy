package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestCLIMultiOverrideCreateUpdateLookup(t *testing.T) {
	var createCalled, updateCalled, lookupCalled atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/compositions":
			createCalled.Add(1)
			var req domain.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("bad create body: %v", err)
			}
			if len(req.Overrides) != 2 || req.Overrides["graphql"].Image != "ghcr.io/org/graphql:v1" || req.Overrides["backend"].Image != "ghcr.io/org/backend:v1" {
				t.Errorf("expected 2 overrides, got %+v", req.Overrides)
			}
			if req.FrontendURL != "https://preview.pages.dev" {
				t.Errorf("expected frontend URL, got %s", req.FrontendURL)
			}
			if len(req.Revisions) != 1 || req.Revisions["backend"].CommitSHA != "abc1234" || req.Revisions["backend"].PRNumber != "42" {
				t.Errorf("expected revisions, got %+v", req.Revisions)
			}
			json.NewEncoder(w).Encode(domain.Composition{
				ID:          "cmp-multi-1",
				Phase:       domain.PhaseReady,
				Generation:  1,
				FrontendURL: req.FrontendURL,
				Overrides:   req.Overrides,
				Revisions:   req.Revisions,
				Endpoints: map[string]domain.Endpoint{
					"public": {URL: "https://cmp-multi-1.preview.example.com", Ready: true},
				},
			})

		case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/v1/compositions/"):
			updateCalled.Add(1)
			var req domain.UpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("bad update body: %v", err)
			}
			if req.ExpectedGeneration != 1 {
				t.Errorf("expected gen 1, got %d", req.ExpectedGeneration)
			}
			if req.FrontendURL == nil || *req.FrontendURL != "https://updated.pages.dev" {
				t.Errorf("expected updated frontend URL, got %v", req.FrontendURL)
			}
			json.NewEncoder(w).Encode(domain.Composition{
				ID:          "cmp-multi-1",
				Phase:       domain.PhaseReady,
				Generation:  2,
				FrontendURL: *req.FrontendURL,
			})

		case r.Method == "GET" && r.URL.Path == "/v1/compositions/lookup":
			lookupCalled.Add(1)
			commit := r.URL.Query().Get("commit_sha")
			if commit != "abc1234" {
				t.Errorf("expected commit abc1234, got %s", commit)
			}
			json.NewEncoder(w).Encode(domain.Composition{
				ID:    "cmp-multi-1",
				Phase: domain.PhaseReady,
				Endpoints: map[string]domain.Endpoint{
					"public": {URL: "https://cmp-multi-1.preview.example.com", Ready: true},
				},
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

	// 1. Create with multiple overrides, revisions, and frontend URL
	var out, diag bytes.Buffer
	code := Run(context.Background(), []string{
		"composition", "create",
		"--name", "multi-preview",
		"--override", "graphql=ghcr.io/org/graphql:v1",
		"--override", "backend=ghcr.io/org/backend:v1",
		"--revision", "backend=repo@abc1234#42",
		"--frontend-url", "https://preview.pages.dev",
	}, &out, &diag, env)
	if code != 0 || diag.Len() != 0 {
		t.Fatalf("create failed: code=%d err=%s", code, &diag)
	}
	if createCalled.Load() != 1 {
		t.Fatalf("expected 1 create call, got %d", createCalled.Load())
	}

	// 2. Update with new frontend URL
	out.Reset()
	diag.Reset()
	code = Run(context.Background(), []string{
		"composition", "update", "cmp-multi-1",
		"--expected-generation", "1",
		"--frontend-url", "https://updated.pages.dev",
	}, &out, &diag, env)
	if code != 0 || diag.Len() != 0 {
		t.Fatalf("update failed: code=%d err=%s", code, &diag)
	}
	if updateCalled.Load() != 1 {
		t.Fatalf("expected 1 update call, got %d", updateCalled.Load())
	}

	// 3. Lookup composition
	out.Reset()
	diag.Reset()
	code = Run(context.Background(), []string{
		"composition", "lookup",
		"--commit-sha", "abc1234",
	}, &out, &diag, env)
	if code != 0 || diag.Len() != 0 {
		t.Fatalf("lookup failed: code=%d err=%s", code, &diag)
	}
	if lookupCalled.Load() != 1 {
		t.Fatalf("expected 1 lookup call, got %d", lookupCalled.Load())
	}
}

func TestCLISetupAndDoctorCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/projects":
			var p domain.Project
			_ = json.NewDecoder(r.Body).Decode(&p)
			json.NewEncoder(w).Encode(p)

		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/v1/projects/") && strings.Contains(r.URL.Path, "/components"):
			var c domain.Component
			_ = json.NewDecoder(r.Body).Decode(&c)
			json.NewEncoder(w).Encode(c)

		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/v1/projects/") && strings.Contains(r.URL.Path, "/baselines"):
			var b domain.Baseline
			_ = json.NewDecoder(r.Body).Decode(&b)
			json.NewEncoder(w).Encode(b)

		case r.Method == "GET" && r.URL.Path == "/v1/projects":
			json.NewEncoder(w).Encode(map[string]any{
				"items": []domain.Project{{ID: "shop", Name: "Shop Platform"}},
			})

		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/projects/") && strings.Contains(r.URL.Path, "/components"):
			json.NewEncoder(w).Encode(map[string]any{
				"items": []domain.Component{
					{ID: "graphql", Project: "shop", Overridable: true},
					{ID: "backend", Project: "shop", Overridable: true},
				},
			})

		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/projects/") && strings.Contains(r.URL.Path, "/baselines"):
			json.NewEncoder(w).Encode(map[string]any{
				"items": []domain.Baseline{
					{
						ID:      "staging",
						Project: "shop",
						Routing: domain.BaselineRouting{EntryComponent: "graphql"},
					},
				},
			})

		case r.Method == "POST" && r.URL.Path == "/v1/compositions":
			json.NewEncoder(w).Encode(domain.Composition{
				ID:    "cmp-probe-1",
				Phase: domain.PhaseReady,
				Endpoints: map[string]domain.Endpoint{
					"public": {URL: "https://cmp-probe-1.preview.example.com", Ready: true},
				},
			})

		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/compositions/cmp-probe-1"):
			json.NewEncoder(w).Encode(domain.Composition{
				ID:    "cmp-probe-1",
				Phase: domain.PhaseReady,
				Endpoints: map[string]domain.Endpoint{
					"public": {URL: "https://cmp-probe-1.preview.example.com", Ready: true},
				},
			})

		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/v1/compositions/cmp-probe-1"):
			json.NewEncoder(w).Encode(domain.Composition{
				ID:    "cmp-probe-1",
				Phase: domain.PhaseDestroyed,
			})

		default:
			t.Errorf("unhandled route: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	// Create temporary envy.json configuration file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "envy.json")
	cfgContent := `{
		"project": {"id": "shop", "name": "Shop Platform"},
		"baseline": {
			"id": "staging",
			"routing": {"entry_component": "graphql"}
		},
		"components": [
			{"id": "graphql", "overridable": true},
			{"id": "backend", "overridable": true}
		]
	}`
	if err := os.WriteFile(configFile, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	env := func(k string) string {
		return map[string]string{
			"ENVY_API_URL":   server.URL,
			"ENVY_API_TOKEN": "secret",
		}[k]
	}

	// 1. Run delivery setup
	var out, diag bytes.Buffer
	code := Run(context.Background(), []string{"setup", "--file", configFile, "--verify=true"}, &out, &diag, env)
	if code != 0 || diag.Len() != 0 {
		t.Fatalf("setup failed: code=%d diag=%s", code, &diag)
	}
	var setupRes SetupResult
	if err := json.Unmarshal(out.Bytes(), &setupRes); err != nil {
		t.Fatalf("bad setup json: %v out=%s", err, out.String())
	}
	if setupRes.Status != "ready" || !setupRes.ProbeVerified || setupRes.EntryComponent != "graphql" {
		t.Fatalf("setup incomplete: %+v", setupRes)
	}

	// 2. Run delivery doctor
	out.Reset()
	diag.Reset()
	code = Run(context.Background(), []string{"doctor", "--project", "shop", "--probe=true"}, &out, &diag, env)
	if code != 0 || diag.Len() != 0 {
		t.Fatalf("doctor failed: code=%d diag=%s", code, &diag)
	}
	var doctorRes DoctorResult
	if err := json.Unmarshal(out.Bytes(), &doctorRes); err != nil {
		t.Fatalf("bad doctor json: %v out=%s", err, out.String())
	}
	if doctorRes.Status != "ok" || len(doctorRes.Checks) < 4 {
		t.Fatalf("doctor failed checks: %+v", doctorRes)
	}
}

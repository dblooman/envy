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

func TestBuildSelectionAndRevisionCLIUseREST(t *testing.T) {
	buildID := strings.Repeat("a", 64)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing credentials")
		}
		if r.Method == "GET" {
			if r.URL.Path != "/v1/projects/demo/repositories/backend/resolve" || r.URL.Query().Get("ref") != "feature/a#b" || r.URL.Query().Get("component") != "service-b" {
				t.Error("resolution parameters lost")
			}
			json.NewEncoder(w).Encode(domain.RevisionResolution{Commit: domain.GitCommit{SHA: strings.Repeat("b", 40)}, Builds: []domain.Build{}})
			return
		}
		var req struct {
			Overrides          map[string]domain.ComponentOverride `json:"overrides"`
			ExpectedGeneration int64                               `json:"expected_generation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Overrides["service-b"].BuildID != buildID || req.Overrides["service-b"].Image != "" || req.Overrides["service-b"].Source != nil || req.Overrides["service-a"].Image != "envy/service-a:v2" {
			t.Error("mixed build/image request corrupted")
		}
		if r.Method == "PATCH" && req.ExpectedGeneration != 2 {
			t.Error("generation lost")
		}
		json.NewEncoder(w).Encode(domain.Composition{ID: "abc", Phase: domain.PhaseCreated})
	}))
	defer server.Close()
	env := func(k string) string {
		return map[string]string{"ENVY_API_URL": server.URL, "ENVY_API_TOKEN": "secret"}[k]
	}
	for _, args := range [][]string{
		{"source", "resolve", "--project", "demo", "--repository", "backend", "--component", "service-b", "--ref", "feature/a#b"},
		{"composition", "create", "--name", "build-preview", "--build", "service-b=" + buildID, "--override", "service-a=envy/service-a:v2"},
		{"composition", "update", "abc", "--expected-generation", "2", "--build", "service-b=" + buildID, "--override", "service-a=envy/service-a:v2"},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, env); code != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("%v: %d %s", args, code, &diag)
		}
	}
	for _, args := range [][]string{
		{"composition", "create", "--name", "bad", "--build", "service-b=" + buildID, "--override", "service-b=envy/service-b:v2"},
		{"composition", "create", "--name", "bad", "--build", "service-b=invalid"},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, env); code == 0 {
			t.Fatal("invalid selection accepted")
		}
	}
	if calls != 3 {
		t.Fatalf("invalid requests reached API: %d calls", calls)
	}
}

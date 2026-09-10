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

func TestSourceDiscoveryToolsAndBuildSelection(t *testing.T) {
	sha := strings.Repeat("a", 40)
	id := strings.Repeat("b", 64)
	repo := domain.SourceRepository{Project: "demo", ID: "backend", Enabled: true, GitHubRepository: "acme/backend", InstallationID: 1, Images: map[string]string{"service-b": "registry.example.com/service-b"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repositories"):
			json.NewEncoder(w).Encode(client.Page[domain.SourceRepository]{Items: []domain.SourceRepository{repo}})
		case strings.HasSuffix(r.URL.Path, "/branches"):
			json.NewEncoder(w).Encode(client.GitPage[domain.GitBranch]{Items: []domain.GitBranch{{Name: "main", SHA: sha}}, Page: 1})
		case strings.HasSuffix(r.URL.Path, "/commits"):
			json.NewEncoder(w).Encode(client.GitPage[domain.GitCommit]{Items: []domain.GitCommit{{SHA: sha, Message: "history"}}, Page: 1})
		case strings.HasSuffix(r.URL.Path, "/resolve"):
			json.NewEncoder(w).Encode(domain.RevisionResolution{Repository: repo, Commit: domain.GitCommit{SHA: sha, Message: "history"}, Builds: []domain.Build{}, CIURL: "https://github.com/acme/backend/actions"})
		default:
			var req domain.CreateRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Overrides["service-b"].BuildID != id || req.Overrides["service-b"].Image != "" {
				t.Error("build input not preserved")
			}
			json.NewEncoder(w).Encode(domain.Composition{ID: "abc", Phase: domain.PhaseCreated, Overrides: req.Overrides, Components: map[string]domain.ComponentObservation{}, Endpoints: map[string]domain.Endpoint{}, Conditions: []domain.Condition{}})
		}
	}))
	defer server.Close()
	c, err := client.New(server.URL, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, b := sdk.NewInMemoryTransports()
	session, err := NewServer(c).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	caller, err := sdk.NewClient(&sdk.Implementation{Name: "build-tests", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"list_source_repositories", map[string]any{"project": "demo"}},
		{"list_source_branches", map[string]any{"project": "demo", "repository": "backend"}},
		{"list_source_commits", map[string]any{"project": "demo", "repository": "backend", "branch": "main"}},
		{"resolve_source_revision", map[string]any{"project": "demo", "repository": "backend", "component": "service-b", "ref": sha}},
		{"create_composition", map[string]any{"project": "demo", "baseline": "staging", "name": "test", "overrides": map[string]any{"service-b": map[string]any{"build_id": id}}}},
	} {
		out, err := caller.CallTool(ctx, &sdk.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil || out.IsError || out.StructuredContent == nil {
			t.Fatalf("%s: %+v %v", call.name, out, err)
		}
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// This is an actual SDK protocol client over both in-memory newline JSON and a
// subprocess's stdio. The HTTP boundary is deliberately exercised in each case.
func TestToolsThroughSDKClient(t *testing.T) {
	for _, transport := range []string{"memory", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			var creates, deletes, endpoints, updates atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("MCP did not authenticate to REST")
				}

				composition := fixtureComposition()
				switch {
				case strings.Contains(r.URL.Path, "/frontend-bindings"):
					if strings.HasSuffix(r.URL.Path, "/resolve") {
						json.NewEncoder(w).Encode(domain.FrontendResolution{Project: "demo", Frontend: "web", Revision: strings.Repeat("a", 40), Composition: "abc123", APIURL: "https://preview.example"})
						return
					}

					view := domain.FrontendBindingView{Binding: domain.FrontendBinding{Project: "demo", Frontend: "web", Revision: strings.Repeat("a", 40), Composition: "abc123", Version: 1}, CheckState: "not_reported"}
					if r.Method == http.MethodPut {
						var req domain.BindFrontendRequest
						json.NewDecoder(r.Body).Decode(&req)
						if req.Composition != "abc123" || req.Repository != "https://example.com/web" {
							t.Error("lost binding input")
						}
					}

					if strings.HasSuffix(r.URL.Path, "/deployment") {
						var req domain.PublishFrontendRequest
						json.NewDecoder(r.Body).Decode(&req)
						if req.ExpectedVersion != 1 || req.URL != "https://web.pages.dev" {
							t.Error("lost publication input")
						}
					}

					if strings.HasSuffix(r.URL.Path, "/check") {
						var req domain.FrontendCheckRequest
						json.NewDecoder(r.Body).Decode(&req)
						if req.ExpectedVersion != 2 || req.CompositionGeneration != 1 || req.Status != "passed" || req.Message != "Browser proof" {
							t.Error("lost check input")
						}
					}

					if strings.HasPrefix(r.URL.Path, "/v1/compositions/") {
						json.NewEncoder(w).Encode(client.Page[domain.FrontendBindingView]{Items: []domain.FrontendBindingView{view}})
					} else {
						json.NewEncoder(w).Encode(view)
					}

					return
				case r.URL.Path == "/v1/compositions" && r.Method == http.MethodGet:
					if r.URL.Query().Get("project") != "demo" {
						t.Error("lost composition project scope")
					}

					json.NewEncoder(w).Encode(client.CompositionsPage{Items: []domain.Composition{composition}})
					return
				case r.URL.Path == "/v1/projects":
					json.NewEncoder(w).Encode(client.Page[domain.Project]{Items: []domain.Project{{ID: "demo", Name: "Demo"}}, NextCursor: "demo"})
					return
				case r.URL.Path == "/v1/projects/demo/components":
					json.NewEncoder(w).Encode(client.Page[domain.Component]{Items: []domain.Component{{ID: "service-b", Project: "demo"}}})
					return
				case r.URL.Path == "/v1/projects/demo/baselines":
					json.NewEncoder(w).Encode(client.Page[domain.Baseline]{Items: []domain.Baseline{{ID: "staging", Project: "demo", Components: map[string]domain.BaselineBinding{}, Verification: domain.VerificationContract{Chain: []string{}}}}})
					return
				case r.URL.Path == "/v1/projects/demo/components/service-b":
					json.NewEncoder(w).Encode(domain.Component{ID: "service-b", Project: "demo"})
					return
				case r.Method == http.MethodPost:
					creates.Add(1)
					if r.Header.Get("Idempotency-Key") != "mcp-retry" {
						t.Error("MCP lost idempotency key")
					}

					var body domain.CreateRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Project != "demo" || !body.MessageIsolation || body.Overrides["service-b"].Image != "envy/service-b:v2" {
						t.Error("MCP create did not preserve input")
					}

					w.WriteHeader(http.StatusAccepted)
				case r.Method == http.MethodPatch:
					updates.Add(1)
					var body domain.UpdateRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ExpectedGeneration != 1 || body.Overrides["service-b"].Image != "envy/service-b:v3" {
						t.Error("MCP update lost input")
					}

					composition.Generation = 2
					composition.Phase = domain.PhaseUpdating
					w.WriteHeader(http.StatusAccepted)
				case r.Method == http.MethodDelete:
					deletes.Add(1)
					composition.Phase = domain.PhaseDestroying
					w.WriteHeader(http.StatusAccepted)
				case r.URL.Path == "/v1/compositions/abc123/components/gateway/logs":
					if r.URL.Query().Get("max_bytes") != "32" || r.URL.Query().Get("tail_lines") != "4" {
						t.Error("MCP lost log bounds")
					}

					json.NewEncoder(w).Encode(domain.ComponentLogs{ID: "abc123", Project: "demo", Component: "gateway", Source: "shared-baseline", Message: "Shared-baseline logs; not composition filtered", Streams: []domain.LogStream{}})
					return
				case r.URL.Path == "/v1/compositions/abc123/events":
					if r.URL.Query().Get("after") != "3" || r.URL.Query().Get("limit") != "2" {
						t.Error("MCP lost event pagination")
					}

					json.NewEncoder(w).Encode(domain.EventsPage{Items: []domain.LifecycleEvent{}, NextCursor: "4"})
					return
				case r.URL.Path == "/v1/compositions/abc123/endpoints":
					endpoints.Add(1)
					json.NewEncoder(w).Encode(map[string]any{"id": composition.ID, "endpoints": composition.Endpoints})
					return
				}

				json.NewEncoder(w).Encode(composition)
			}))
			defer api.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var clientTransport sdk.Transport
			if transport == "memory" {
				rest, err := client.New(api.URL, "test-token", nil)
				if err != nil {
					t.Fatal(err)
				}

				ct, st := sdk.NewInMemoryTransports()
				serverSession, err := NewServer(rest).Connect(ctx, st, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer serverSession.Close()
				clientTransport = ct
			} else {
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStdioHelperProcess$")
				command.Env = append(os.Environ(), "ENVY_MCP_HELPER=1", "ENVY_HELPER_API_URL="+api.URL)
				command.Stderr = os.Stderr
				clientTransport = &sdk.CommandTransport{Command: command, TerminateDuration: time.Second}
			}

			session, err := sdk.NewClient(&sdk.Implementation{Name: "envy-protocol-test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			list, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}

			if len(list.Tools) != 29 {
				t.Fatalf("got %d tools", len(list.Tools))
			}

			for _, tool := range list.Tools {
				if tool.InputSchema == nil || tool.OutputSchema == nil {
					t.Fatalf("tool %s lacks typed schemas", tool.Name)
				}
			}

			for _, call := range []struct {
				name      string
				arguments map[string]any
			}{
				{"list_compositions", map[string]any{"project": "demo"}},
				{"bind_frontend", map[string]any{"project": "demo", "frontend": "web", "revision": strings.Repeat("a", 40), "composition": "abc123", "repository": "https://example.com/web"}},
				{"get_frontend_binding", map[string]any{"project": "demo", "frontend": "web", "revision": strings.Repeat("a", 40)}},
				{"resolve_frontend", map[string]any{"project": "demo", "frontend": "web", "revision": strings.Repeat("a", 40), "timeout_seconds": 1}},
				{"publish_frontend", map[string]any{"project": "demo", "frontend": "web", "revision": strings.Repeat("a", 40), "expected_version": 1, "url": "https://web.pages.dev"}},
				{"report_frontend_check", map[string]any{"project": "demo", "frontend": "web", "revision": strings.Repeat("a", 40), "expected_version": 2, "composition_generation": 1, "status": "passed", "message": "Browser proof"}},
				{"list_frontend_bindings", map[string]any{"id": "abc123"}},
				{"list_projects", map[string]any{"limit": 1}},
				{"list_components", map[string]any{"project": "demo"}},
				{"list_baselines", map[string]any{"project": "demo"}},
				{"get_component", map[string]any{"project": "demo", "component": "service-b"}},
				{"create_composition", map[string]any{"project": "demo", "baseline": "staging", "name": "mcp-test", "message_isolation": true, "overrides": map[string]any{"service-b": map[string]any{"image": "envy/service-b:v2"}}, "idempotency_key": "mcp-retry"}},
				{"get_composition", map[string]any{"id": "abc123"}},
				{"get_component_logs", map[string]any{"id": "abc123", "component": "gateway", "tail_lines": 4, "max_bytes": 32}},
				{"list_composition_events", map[string]any{"id": "abc123", "after": "3", "limit": 2}},
				{"update_composition", map[string]any{"id": "abc123", "expected_generation": 1, "overrides": map[string]any{"service-b": map[string]any{"image": "envy/service-b:v3"}}}},
				{"wait_for_composition", map[string]any{"id": "abc123", "timeout_seconds": 1}},
				{"get_composition_endpoints", map[string]any{"id": "abc123"}},
				{"destroy_composition", map[string]any{"id": "abc123"}},
			} {
				result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: call.name, Arguments: call.arguments})
				if err != nil {
					t.Fatalf("%s: %v", call.name, err)
				}

				if result.IsError || result.StructuredContent == nil || len(result.Content) == 0 {
					t.Fatalf("%s invalid result: %+v", call.name, result)
				}

				data, err := json.Marshal(result.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}

				var got map[string]any
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}

				if call.name == "list_compositions" || call.name == "list_frontend_bindings" || call.name == "list_projects" || call.name == "list_components" || call.name == "list_baselines" {
					if len(got["items"].([]any)) != 1 {
						t.Fatal("missing catalog entries")
					}
				} else if call.name == "get_component" {
					if got["id"] != "service-b" || got["project"] != "demo" {
						t.Fatal("lost catalog scope")
					}
				} else if call.name == "list_composition_events" {
					if got["next_cursor"] != "4" {
						t.Fatal("lost event cursor")
					}
				} else if call.name == "resolve_frontend" {
					if got["api_url"] != "https://preview.example" {
						t.Fatal("lost resolution")
					}
				} else if strings.Contains(call.name, "frontend") {
					if got["binding"].(map[string]any)["composition"] != "abc123" {
						t.Fatal("lost binding")
					}
				} else if got["id"] != "abc123" {
					t.Fatalf("missing structured ID: %s", data)
				}
			}

			if creates.Load() != 1 || deletes.Load() != 1 || endpoints.Load() != 1 || updates.Load() != 1 {
				t.Fatalf("unexpected REST calls create=%d delete=%d endpoints=%d", creates.Load(), deletes.Load(), endpoints.Load())
			}

			bad, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "wait_for_composition", Arguments: map[string]any{"id": "abc123", "timeout_seconds": 61}})
			if err != nil || !bad.IsError {
				t.Fatalf("unbounded wait accepted: %+v %v", bad, err)
			}
		})
	}
}

func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("ENVY_MCP_HELPER") != "1" {
		return
	}

	rest, err := client.New(os.Getenv("ENVY_HELPER_API_URL"), "test-token", nil)
	if err != nil {
		os.Exit(2)
	}

	if err := Run(context.Background(), rest); err != nil {
		os.Exit(3)
	}

	os.Exit(0) // avoid Go test's PASS line on protocol stdout
}

func fixtureComposition() domain.Composition {
	now := time.Now().UTC()
	return domain.Composition{
		ID: "abc123", Project: "demo", Baseline: "staging", BaselineRevision: "1", Name: "mcp-test",
		Overrides:  map[string]domain.ComponentOverride{"service-b": {Image: "envy/service-b:v2"}},
		Generation: 1, ObservedGeneration: 1, Phase: domain.PhaseReady,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
		Components:      map[string]domain.ComponentObservation{"service-b": {Source: "override", Status: "ready", Image: "envy/service-b:v2"}},
		Endpoints:       map[string]domain.Endpoint{"public": {URL: "http://cmp-abc123.envy.localhost:8080", Ready: true}},
		Conditions:      []domain.Condition{{Type: "RequestRoutingVerified", Status: true, Message: "verified"}},
		LatestOperation: domain.Operation{ID: "operation", Kind: "create", Status: "succeeded"},
	}
}

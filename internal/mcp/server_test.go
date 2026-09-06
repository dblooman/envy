package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
				case r.Method == http.MethodPost:
					creates.Add(1)
					if r.Header.Get("Idempotency-Key") != "mcp-retry" {
						t.Error("MCP lost idempotency key")
					}
					var body domain.CreateRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Project != "demo" || body.Overrides["service-b"].Image != "envy/service-b:v2" {
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
			if len(list.Tools) != 6 {
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
				{"create_composition", map[string]any{"project": "demo", "baseline": "staging", "name": "mcp-test", "overrides": map[string]any{"service-b": map[string]any{"image": "envy/service-b:v2"}}, "idempotency_key": "mcp-retry"}},
				{"get_composition", map[string]any{"id": "abc123"}},
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
				if got["id"] != "abc123" {
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

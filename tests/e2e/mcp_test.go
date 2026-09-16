//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPCompositionThroughStdio(t *testing.T) {
	h := newHarness(t)
	binary := os.Getenv("ENVY_MCP_BINARY")
	if binary == "" {
		t.Fatal("ENVY_MCP_BINARY is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary)
	command.Env = os.Environ()
	command.Stderr = os.Stderr
	session, err := sdk.NewClient(&sdk.Implementation{Name: "envy-kind-acceptance", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: command, TerminateDuration: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	const expectedTools = 33
	if len(tools.Tools) != expectedTools {
		t.Fatalf("expected %d semantic tools, got %d", expectedTools, len(tools.Tools))
	}
	call := func(name string, args map[string]any) composition {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if result.IsError || result.StructuredContent == nil {
			t.Fatalf("%s: %+v", name, result)
		}
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var c composition
		if err = json.Unmarshal(data, &c); err != nil || c.ID == "" {
			t.Fatalf("invalid structured result %s: %v", data, err)
		}
		return c
	}
	c := call("create_composition", map[string]any{"project": "demo", "baseline": "staging", "name": "mcp-acceptance", "overrides": map[string]any{"service-b": map[string]string{"image": "envy/service-b:v2"}}, "ttl": "10m", "idempotency_key": "mcp-e2e"})
	for i := 0; i < 3 && c.Phase != "ready"; i++ {
		c = call("wait_for_composition", map[string]any{"id": c.ID, "timeout_seconds": 60})
	}
	if c.Phase != "ready" {
		t.Fatalf("MCP composition did not become ready: %+v", c)
	}
	c = call("get_composition", map[string]any{"id": c.ID})
	endpoints := call("get_composition_endpoints", map[string]any{"id": c.ID})
	if !endpoints.Endpoints["public"].Ready {
		t.Fatal("MCP endpoint not ready")
	}
	chain, err := h.chain(endpoints.Endpoints["public"].URL, c.ID, "v2", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MCP-created request chain: %+v", chain)

	h.baseline()
	updated := call("update_composition", map[string]any{"id": c.ID, "expected_generation": c.Generation, "overrides": map[string]any{"service-b": map[string]string{"image": "envy/service-b:v3"}}})
	if updated.Generation != c.Generation+1 || updated.Endpoints["public"].URL != endpoints.Endpoints["public"].URL {
		t.Fatal("MCP update changed identity or failed to increment generation")
	}
	for i := 0; i < 3 && updated.Phase != "ready"; i++ {
		updated = call("wait_for_composition", map[string]any{"id": c.ID, "timeout_seconds": 60})
	}
	if updated.Phase != "ready" {
		t.Fatalf("MCP update did not become ready: %+v", updated)
	}
	if _, err = h.chain(endpoints.Endpoints["public"].URL, c.ID, "v3", ""); err != nil {
		t.Fatal(err)
	}

	h.baseline()
	for _, component := range []string{"service-b", "gateway"} {
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "get_component_logs", Arguments: map[string]any{"id": c.ID, "component": component, "max_bytes": 4096}})
		if err != nil || result.IsError {
			t.Fatalf("MCP logs: %+v %v", result, err)
		}
		data, _ := json.Marshal(result.StructuredContent)
		var logs domain.ComponentLogs
		if err = json.Unmarshal(data, &logs); err != nil || len(logs.Streams) == 0 || logs.CompositionFiltered {
			t.Fatalf("MCP log result: %s %v", data, err)
		}
		readable := false
		for _, stream := range logs.Streams {
			if stream.Error == nil && stream.Text != "" && stream.Container == component {
				readable = true
			}
		}
		if !readable {
			t.Fatalf("MCP snapshot has no readable application logs: %s", data)
		}
		if component == "gateway" && logs.Source != "shared-baseline" {
			t.Fatal("MCP inherited logs lost shared label")
		}
	}
	events, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "list_composition_events", Arguments: map[string]any{"id": c.ID, "limit": 1}})
	if err != nil || events.IsError {
		t.Fatalf("MCP events: %+v %v", events, err)
	}
	data, _ := json.Marshal(events.StructuredContent)
	var page domain.EventsPage
	if err = json.Unmarshal(data, &page); err != nil || len(page.Items) != 1 || page.NextCursor == "" || page.Items[0].Type != "create_requested" {
		t.Fatalf("MCP event page: %s %v", data, err)
	}
	call("destroy_composition", map[string]any{"id": c.ID})
	for i := 0; i < 3 && c.Phase != "destroyed"; i++ {
		c = call("wait_for_composition", map[string]any{"id": c.ID, "timeout_seconds": 60})
	}
	if c.Phase != "destroyed" {
		t.Fatalf("MCP cleanup did not finish: %+v", c)
	}
	h.absent(c.ID, endpoints.Endpoints["public"].URL)
}

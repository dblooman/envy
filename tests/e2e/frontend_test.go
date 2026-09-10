//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFrontendBindingCLIAndMCP(t *testing.T) {
	h := newHarness(t)
	c := h.create("frontend-binding", "envy/service-b:v2", "15m", "")
	t.Cleanup(func() { h.destroy(c.ID); h.wait(c.ID, "destroyed") })
	revision := strings.Repeat("f", 40)
	key := []string{"--project", "demo", "--frontend", "web", "--revision", revision}
	cli := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Getenv("ENVY_CLI_BINARY"), append([]string{"frontend"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("frontend CLI: %s %v", out, err)
		}
		return out
	}
	args := append([]string{"bind", "--composition", c.ID, "--repository", "https://example.com/web"}, key...)
	first := cli(args...)
	repeat := cli(args...)
	var binding, replayed domain.FrontendBindingView
	if json.Unmarshal(first, &binding) != nil || json.Unmarshal(repeat, &replayed) != nil || binding.Binding.CreatedAt != replayed.Binding.CreatedAt {
		t.Fatal("binding retry changed identity")
	}
	c = h.wait(c.ID, "ready")
	var receipt domain.FrontendResolution
	if json.Unmarshal(cli(append([]string{"resolve", "--timeout", "10s"}, key...)...), &receipt) != nil || receipt.Composition != c.ID {
		t.Fatal("CLI resolution lost binding")
	}
	if _, err := h.chain(receipt.APIURL, c.ID, "v2", ""); err != nil {
		t.Fatal(err)
	}
	h.baseline()
	cli(append([]string{"publish", "--expected-version", "1", "--url", "https://web.pages.dev"}, key...)...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Getenv("ENVY_MCP_BINARY"))
	cmd.Stderr = os.Stderr
	session, err := sdk.NewClient(&sdk.Implementation{Name: "envy-frontend-acceptance", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any) []byte {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s: %+v %v", name, result, err)
		}
		data, _ := json.Marshal(result.StructuredContent)
		return data
	}
	// This is explicitly a caller report used to exercise the API, not a claimed browser run.
	check := call("report_frontend_check", map[string]any{"project": "demo", "frontend": "web", "revision": revision, "expected_version": 2, "composition_generation": 1, "status": "failed", "message": "Protocol acceptance only; browser verification not performed by this test"})
	if json.Unmarshal(check, &binding) != nil || binding.CheckState != "current" || binding.Binding.Check.Status != "failed" {
		t.Fatalf("check lost: %s", check)
	}
	call("list_frontend_bindings", map[string]any{"id": c.ID})
	h.controller("0")
	h.controller("1")
	if json.Unmarshal(cli(append([]string{"get"}, key...)...), &binding) != nil || binding.Binding.Check == nil {
		t.Fatal("restart lost binding evidence")
	}
	h.destroy(c.ID)
	h.wait(c.ID, "destroyed")
	code, body, err := h.request("GET", "/v1/projects/demo/frontend-bindings/web/"+revision+"/resolve", nil, "")
	if err != nil || code != 410 {
		t.Fatalf("destroyed resolution %d %s %v", code, body, err)
	}
	h.absent(c.ID, receipt.APIURL)
	t.Log("Verified durable frontend binding via CLI and actual MCP stdio, resolved ingress routing, baseline inheritance, restart persistence and no resolution after deletion")
}

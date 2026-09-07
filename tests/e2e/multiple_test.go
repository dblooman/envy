//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMultipleOverridesAcrossRESTMCPAndCLI(t *testing.T) {
	h := newHarness(t)
	stopped := false
	defer func() {
		if stopped {
			h.controller("1")
		}
	}()
	baseline := h.baseline()
	decode := func(body []byte) composition {
		t.Helper()
		var c composition
		if err := json.Unmarshal(body, &c); err != nil || c.ID == "" {
			t.Fatalf("invalid composition: %s %v", body, err)
		}
		return c
	}
	cli := func(args ...string) composition {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Getenv("ENVY_CLI_BINARY"), append([]string{"composition"}, args...)...)
		cmd.Env = os.Environ()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		body, err := cmd.Output()
		if err != nil {
			t.Fatalf("CLI: %v %s", err, stderr.String())
		}
		return decode(body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Getenv("ENVY_MCP_BINARY"))
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr
	session, err := sdk.NewClient(&sdk.Implementation{Name: "envy-multiple-test", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	mcp := func(tool string, args any) composition {
		t.Helper()
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("MCP %s: %+v %v", tool, result, err)
		}
		body, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		return decode(body)
	}
	req := domain.CreateRequest{Project: "demo", Baseline: "staging", Name: "multiple-rest", TTL: "10m", Overrides: map[string]domain.ComponentOverride{"service-a": {Image: "envy/service-a:v2"}, "service-b": {Image: "envy/service-b:v2"}}}
	status, body, err := h.request("POST", "/v1/compositions", req, "multiple-rest-retry")
	if err != nil || status != 202 {
		t.Fatalf("multiple create %d %s %v", status, body, err)
	}
	a := decode(body)
	t.Cleanup(func() { h.destroy(a.ID); h.absent(a.ID, a.Endpoints["public"].URL) })
	// Restart before readiness to exercise recovery of partially ensured maps.
	h.controller("0")
	h.controller("1")
	a = h.wait(a.ID, "ready")
	status, body, err = h.request("POST", "/v1/compositions", req, "multiple-rest-retry")
	if err != nil || status != 202 || decode(body).ID != a.ID {
		t.Fatal("multi-override idempotency failed")
	}
	b := mcp("create_composition", map[string]any{"project": "demo", "baseline": "staging", "name": "multiple-mcp-entry", "ttl": "10m", "overrides": map[string]any{"gateway": map[string]string{"image": "envy/gateway:v2"}, "service-b": map[string]string{"image": "envy/service-b:v3"}}})
	t.Cleanup(func() { h.destroy(b.ID); h.absent(b.ID, b.Endpoints["public"].URL) })
	c := cli("create", "--name", "multiple-cli", "--override", "gateway=envy/gateway:v2", "--override", "service-a=envy/service-a:v2", "--override", "service-b=envy/service-b:v3", "--ttl", "10m")
	t.Cleanup(func() { h.destroy(c.ID); h.absent(c.ID, c.Endpoints["public"].URL) })
	b = h.wait(b.ID, "ready")
	c = h.wait(c.ID, "ready")
	verify := func(c composition, overrides map[string]string) []hop {
		t.Helper()
		code, chain, err := h.traffic(c.Endpoints["public"].URL, "")
		if err != nil || code != 200 || len(chain) != 3 {
			t.Fatalf("preview %s: %d %+v %v", c.ID, code, chain, err)
		}
		for i, hop := range chain {
			version, overridden := overrides[hop.Service]
			if !overridden {
				version = "v1"
			}
			deployment := "baseline"
			if overridden {
				deployment = c.ID
			}
			if hop.Service != baseline[i].Service || hop.Version != version || hop.Composition != c.ID || hop.DeploymentComposition != deployment {
				t.Fatalf("unexpected multi hop %+v", hop)
			}
			if overridden == (hop.WorkloadID == baseline[i].WorkloadID) {
				t.Fatal("wrong workload inheritance")
			}
		}
		for i, hop := range h.baseline() {
			if hop.WorkloadID != baseline[i].WorkloadID {
				t.Fatal("baseline pod identity changed")
			}
		}
		return chain
	}
	av := map[string]string{"service-a": "v2", "service-b": "v2"}
	bv := map[string]string{"gateway": "v2", "service-b": "v3"}
	cv := map[string]string{"gateway": "v2", "service-a": "v2", "service-b": "v3"}
	before := verify(a, av)
	verify(b, bv)
	verify(c, cv)
	for _, component := range []string{"service-a", "service-b"} {
		status, body, err = h.request("GET", "/v1/compositions/"+a.ID+"/components/"+component+"/logs", nil, "")
		var logs domain.ComponentLogs
		if err != nil || status != 200 || json.Unmarshal(body, &logs) != nil || logs.Source != "override" || len(logs.Streams) == 0 {
			t.Fatalf("component-specific logs: %d %s %v", status, body, err)
		}
	}
	updated := cli("update", a.ID, "--expected-generation", strconv.FormatInt(a.Generation, 10), "--override", "service-a=envy/service-a:v2", "--override", "service-b=envy/service-b:v3")
	if updated.Endpoints["public"].URL != a.Endpoints["public"].URL {
		t.Fatal("update changed preview URL")
	}
	a = h.wait(a.ID, "ready")
	av["service-b"] = "v3"
	after := verify(a, av)
	if before[1].WorkloadID != after[1].WorkloadID || before[2].WorkloadID == after[2].WorkloadID {
		t.Fatal("image update rolled the wrong set of workloads")
	}
	// Preserve the whole override map across restart and an unhealthy selected pod.
	h.controller("0")
	stopped = true
	h.kubectl("-n", "envy-"+a.ID, "scale", "deployment/service-a", "--replicas=0")
	h.kubectl("-n", "envy-"+a.ID, "wait", "--for=delete", "pod", "-l", "envy.dev/component=service-a", "--timeout=60s")
	eventually(t, 20*time.Second, "failed override does not fall back", func() error {
		code, _, err := h.traffic(a.Endpoints["public"].URL, "")
		if err != nil {
			return err
		}
		if code == 200 {
			return fmt.Errorf("unhealthy composition still succeeded")
		}
		return nil
	})
	verify(b, bv)
	verify(c, cv)
	h.controller("1")
	stopped = false
	eventually(t, 90*time.Second, "all override traffic recovers", func() error {
		code, chain, err := h.traffic(a.Endpoints["public"].URL, "")
		if err != nil {
			return err
		}
		if code != 200 || len(chain) != 3 || chain[1].Composition != a.ID || chain[1].Version != "v2" {
			return fmt.Errorf("override still recovering: HTTP %d", code)
		}
		return nil
	})
	a = h.wait(a.ID, "ready")
	verify(a, av)
	// Destroy through MCP, then prove both surviving compositions and baseline.
	mcp("destroy_composition", map[string]any{"id": a.ID})
	h.absent(a.ID, a.Endpoints["public"].URL)
	verify(b, bv)
	verify(c, cv)
	t.Log("Verified three concurrent multi-override compositions, overridden ingress, per-component logs, unchanged workload update, restart recovery, failure isolation, and complete deletion")
}

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestLogsAndDurableEvents(t *testing.T) {
	h := newHarness(t)
	c := h.create("diagnostics", "envy/service-b:v2", "10m", "diagnostics-e2e")
	t.Cleanup(func() { h.destroy(c.ID); h.wait(c.ID, "destroyed") })
	c = h.wait(c.ID, "ready")
	path := "/v1/compositions/" + c.ID
	chain, err := h.chain(c.Endpoints["public"].URL, c.ID, "v2", "")
	if err != nil {
		t.Fatal(err)
	}
	logs := func(component, query string) domain.ComponentLogs {
		t.Helper()
		code, data, err := h.request("GET", path+"/components/"+component+"/logs"+query, nil, "")
		if err != nil || code != 200 {
			t.Fatalf("logs %s: %d %s %v", component, code, data, err)
		}
		var out domain.ComponentLogs
		if err = json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, component := range []string{"service-b", "gateway"} {
		out := logs(component, "?max_bytes=4096&tail_lines=10")
		source, version, index := "override", "v2", 2
		if component == "gateway" {
			source, version, index = "shared-baseline", "v1", 0
		}
		if out.Source != source || out.CompositionFiltered || out.Partial || len(out.Streams) != 1 || out.Streams[0].WorkloadID != chain[index].WorkloadID || out.Streams[0].Container != component || !strings.Contains(out.Streams[0].Text, "version="+version) {
			t.Fatalf("incorrect logs: %+v", out)
		}
	}
	limited := logs("service-b", "?max_bytes=8")
	total := 0
	for _, stream := range limited.Streams {
		total += len(stream.Text)
	}
	if total > 8 || !limited.Truncated {
		t.Fatalf("unbounded log snapshot: %+v", limited)
	}
	for suffix, want := range map[string]int{"/components/unknown/logs": 404, "/components/service-b/logs?max_bytes=262145": 400, "/components/service-b/logs?container=istio-proxy": 400, "/events?after=-1": 400} {
		code, b, err := h.request("GET", path+suffix, nil, "")
		if err != nil || code != want {
			t.Fatalf("%s: %d %s %v", suffix, code, b, err)
		}
	}
	allEvents := func() []domain.LifecycleEvent {
		t.Helper()
		var all []domain.LifecycleEvent
		after := ""
		for i := 0; i < 100; i++ {
			code, data, err := h.request("GET", path+"/events?limit=2&after="+after, nil, "")
			if err != nil || code != 200 {
				t.Fatalf("events: %d %s %v", code, data, err)
			}
			var page domain.EventsPage
			if err = json.Unmarshal(data, &page); err != nil {
				t.Fatal(err)
			}
			all = append(all, page.Items...)
			if page.NextCursor == "" {
				return all
			}
			if page.NextCursor == after {
				t.Fatal("cursor did not advance")
			}
			after = page.NextCursor
		}
		t.Fatal("unbounded event history for short test")
		return nil
	}
	before := allEvents()
	if len(before) < 2 || before[0].Type != "create_requested" {
		t.Fatalf("missing create history: %+v", before)
	}
	h.controller("0")
	h.controller("1")
	h.wait(c.ID, "ready")
	after := allEvents()
	if len(after) < len(before) {
		t.Fatal("restart lost events")
	}
	for i := range before {
		if before[i].ID != after[i].ID {
			t.Fatal("restart replaced history")
		}
	}
	// Identical healthy reconciliation polls must not append events indefinitely.
	stable := after[len(after)-1].ID
	time.Sleep(3 * time.Second) // exercises repeated scheduled observations
	after = allEvents()
	if after[len(after)-1].ID != stable {
		t.Fatal("unchanged polling appended events")
	}
	code, data, err := h.request("PATCH", path, map[string]any{"expected_generation": c.Generation, "overrides": map[string]any{"service-b": map[string]string{"image": "envy/service-b:v3"}}}, "")
	if err != nil || code != 202 {
		t.Fatalf("update: %d %s %v", code, data, err)
	}
	h.wait(c.ID, "ready")
	// Use the compiled adapter, with its actual API credentials and stdout contract.
	binary := os.Getenv("ENVY_CLI_BINARY")
	if binary == "" {
		t.Fatal("ENVY_CLI_BINARY required")
	}
	for _, args := range [][]string{{"logs", c.ID, "--component", "gateway", "--max-bytes", "128"}, {"events", c.ID, "--limit", "1"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		command := exec.CommandContext(ctx, binary, append([]string{"composition"}, args...)...)
		command.Env = os.Environ()
		var out, diag bytes.Buffer
		command.Stdout = &out
		command.Stderr = &diag
		err := command.Run()
		cancel()
		if err != nil || diag.Len() != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("CLI diagnostics %v: %s %s %v", args, &out, &diag, err)
		}
		if args[0] == "logs" && !bytes.Contains(out.Bytes(), []byte(`"source":"shared-baseline"`)) {
			t.Fatal("CLI lost shared log label")
		}
	}
	h.destroy(c.ID)
	h.absent(c.ID, c.Endpoints["public"].URL)
	history := allEvents()
	types := map[string]bool{}
	var previous int64
	for _, event := range history {
		n, err := domain.EventCursor(event.ID)
		if err != nil || n <= previous || event.Composition != c.ID {
			t.Fatalf("bad event ordering/scope: %+v", event)
		}
		previous = n
		types[event.Type] = true
	}
	if !types["create_requested"] || !types["update_requested"] || !types["destroy_requested"] || history[len(history)-1].Phase != domain.PhaseDestroyed {
		t.Fatalf("incomplete tombstone history: %+v", history)
	}
	code, _, err = h.request("GET", path+"/components/service-b/logs", nil, "")
	if err != nil || code != 409 {
		t.Fatalf("destroyed logs status=%d %v", code, err)
	}
	h.baseline()
	t.Logf("verified bounded shared/override logs, CLI diagnostics, and %d durable ordered events", len(history))
}

func TestExpiryHasDistinctLifecycleEvent(t *testing.T) {
	h := newHarness(t)
	c := h.create("event-expiry", "envy/service-b:v2", "1s", "diagnostics-expiry")
	h.wait(c.ID, "destroyed")
	code, data, err := h.request("GET", "/v1/compositions/"+c.ID+"/events", nil, "")
	if err != nil || code != 200 {
		t.Fatalf("expiry events: %d %s %v", code, data, err)
	}
	var page domain.EventsPage
	if err = json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Items {
		if event.Type == "expired" {
			return
		}
	}
	t.Fatal(fmt.Sprintf("missing expiry event: %+v", page))
}

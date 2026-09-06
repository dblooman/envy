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
)

func TestCLIImageUpdatePreservesComposition(t *testing.T) {
	h := newHarness(t)
	binary := os.Getenv("ENVY_CLI_BINARY")
	if binary == "" {
		t.Fatal("ENVY_CLI_BINARY is required")
	}
	cli := func(want int, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, append([]string{"composition"}, args...)...)
		command.Env = os.Environ()
		var out, diag bytes.Buffer
		command.Stdout = &out
		command.Stderr = &diag
		err := command.Run()
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if code != want {
			t.Fatalf("CLI %v: code=%d stdout=%s stderr=%s", args, code, &out, &diag)
		}
		if code == 1 {
			if out.Len() != 0 || !json.Valid(diag.Bytes()) {
				t.Fatalf("bad CLI error: %s %s", &out, &diag)
			}
			return diag.Bytes()
		}
		if !json.Valid(out.Bytes()) || diag.Len() != 0 {
			t.Fatalf("CLI stdout is not pure JSON: %s stderr=%s", &out, &diag)
		}
		return out.Bytes()
	}
	decode := func(b []byte) composition {
		t.Helper()
		var c composition
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	baseline := h.baseline()
	assertBaseline := func() error {
		got, err := h.chain(h.preview, "", "v1", "")
		if err != nil {
			return err
		}
		for i := range got {
			if got[i].WorkloadID != baseline[i].WorkloadID {
				return fmt.Errorf("baseline identity changed")
			}
		}
		return nil
	}
	c := decode(cli(0, "create", "--name", "cli-update", "--image", "envy/service-b:v2", "--ttl", "10m"))
	t.Cleanup(func() { h.destroy(c.ID); h.wait(c.ID, "destroyed") })
	c = h.wait(c.ID, "ready")
	cli(0, "wait", c.ID, "--timeout", "1s")
	endpoint := c.Endpoints["public"].URL
	before, err := h.chain(endpoint, c.ID, "v2", "")
	if err != nil {
		t.Fatal(err)
	}
	ns := "envy-" + c.ID
	identities := h.kubectl("-n", ns, "get", "deployment/service-b", "service/service-b", "-o", "jsonpath={.items[*].metadata.uid}")
	updated := decode(cli(0, "update", c.ID, "--expected-generation", strconv.FormatInt(c.Generation, 10), "--image", "envy/service-b:v3"))
	if updated.ID != c.ID || updated.Generation != c.Generation+1 || updated.Phase != "updating" || updated.Endpoints["public"].URL != endpoint || updated.Endpoints["public"].Ready {
		t.Fatalf("invalid accepted update: %+v", updated)
	}
	stale := cli(1, "update", c.ID, "--expected-generation", strconv.FormatInt(c.Generation, 10), "--image", "envy/service-b:v2")
	if !bytes.Contains(stale, []byte(`"code":"conflict"`)) {
		t.Fatalf("stale update did not conflict: %s", stale)
	}
	// The accepted update must survive a control-plane restart even before rollout.
	h.controller("0")
	if err = assertBaseline(); err != nil {
		t.Fatal(err)
	}
	h.controller("1")
	eventually(t, 180*time.Second, "CLI update becomes ready while baseline stays unchanged", func() error {
		if err := assertBaseline(); err != nil {
			t.Fatal(err)
		}
		got, err := h.get(c.ID)
		if err != nil {
			return err
		}
		if got.Phase != "ready" || got.Generation != updated.Generation || got.ObservedGeneration != updated.Generation {
			return fmt.Errorf("status=%+v", got)
		}
		after, err := h.chain(endpoint, c.ID, "v3", "")
		if err != nil {
			return err
		}
		if after[0].WorkloadID != before[0].WorkloadID || after[1].WorkloadID != before[1].WorkloadID || after[2].WorkloadID == before[2].WorkloadID {
			t.Fatal("update changed inherited pods or did not replace service-b")
		}
		return nil
	})
	if got := h.kubectl("-n", ns, "get", "deployment/service-b", "service/service-b", "-o", "jsonpath={.items[*].metadata.uid}"); got != identities {
		t.Fatal("update replaced owned Deployment or Service")
	}
	cli(0, "inspect", c.ID)
	cli(0, "endpoints", c.ID)
	cli(0, "list", "--project", "demo", "--limit", "2")
	// A broken rolling update may keep v3 serving, but cannot claim the new image
	// is ready or silently send traffic to baseline service-b v1.
	broken := decode(cli(0, "update", c.ID, "--expected-generation", strconv.FormatInt(updated.Generation, 10), "--image", "envy/service-b:missing-update"))
	eventually(t, 120*time.Second, "broken update fails without baseline fallback", func() error {
		if err := assertBaseline(); err != nil {
			t.Fatal(err)
		}
		got, err := h.get(c.ID)
		if err != nil {
			return err
		}
		if got.Phase == "ready" || got.Endpoints["public"].Ready {
			t.Fatal("broken update became ready")
		}
		code, chain, err := h.traffic(endpoint, "")
		if err != nil {
			return err
		}
		if code == 200 {
			if len(chain) != 3 || chain[2].Version != "v3" || chain[2].Composition != c.ID {
				t.Fatalf("broken update fell back: %+v", chain)
			}
		} else if code < 500 {
			return fmt.Errorf("unexpected preview status %d", code)
		}
		if got.Phase != "failed" {
			return fmt.Errorf("phase=%s", got.Phase)
		}
		return nil
	})
	cli(0, "update", c.ID, "--expected-generation", strconv.FormatInt(broken.Generation, 10), "--image", "envy/service-b:v3")
	h.wait(c.ID, "ready")
	if _, err = h.chain(endpoint, c.ID, "v3", ""); err != nil {
		t.Fatal(err)
	}
	deleted := decode(cli(0, "destroy", c.ID))
	cli(1, "update", c.ID, "--expected-generation", strconv.FormatInt(deleted.Generation, 10), "--image", "envy/service-b:v2")
	h.absent(c.ID, endpoint)
	cli(0, "wait", c.ID, "--timeout", "1s")
	if err = assertBaseline(); err != nil {
		t.Fatal(err)
	}
	t.Logf("CLI v2→v3 update kept URL %s and Deployment/Service UIDs %s; failed update repaired", endpoint, identities)
}

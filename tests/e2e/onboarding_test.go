//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestShopOnboardingAndTwentyCompositions(t *testing.T) {
	h := newHarness(t)
	data, err := os.ReadFile("../../examples/shop/application.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest domain.CatalogManifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Baseline.Endpoint = strings.Replace(h.preview, "baseline.envy.localhost", "shop.envy.localhost", 1)
	data, _ = json.Marshal(manifest)
	file := filepath.Join(t.TempDir(), "application.json")
	if err = os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cli := func(action string) domain.CatalogReport {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Getenv("ENVY_CLI_BINARY"), "catalog", action, "--file", file)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("catalog %s: %s %v", action, out, err)
		}
		var report domain.CatalogReport
		if json.Unmarshal(out, &report) != nil {
			t.Fatalf("invalid report: %s", out)
		}
		return report
	}
	// The example's business route must converge before live validation runs.
	eventually(t, 30*time.Second, "shop baseline", func() error {
		resp, err := h.http.Get(manifest.Baseline.Endpoint + "/products")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	})
	report := cli("validate")
	if report.Applied || len(report.Warnings) != 1 {
		t.Fatal("read-only validation or verification limitations lost")
	}
	status, body, err := h.request("GET", "/v1/projects/shop/components", nil, "")
	if err != nil || status != 404 {
		t.Fatalf("validate persisted catalog: %d %s %v", status, body, err)
	}
	for range 2 {
		if !cli("apply").Applied {
			t.Fatal("repeatable apply failed")
		}
	}
	conflict := report.Configuration
	conflict.Project.Name = "different immutable project"
	status, body, err = h.request("POST", "/v1/catalog/apply", conflict, "")
	if err != nil || status != 409 {
		t.Fatalf("changed immutable config accepted: %d %s %v", status, body, err)
	}
	type response struct {
		Price   int    `json:"price_minor"`
		Release string `json:"release"`
	}
	type observed struct {
		Body                                                   response
		Storefront, Pricing, StorefrontContext, PricingContext string
	}
	observe := func(endpoint string) (observed, time.Duration, error) {
		start := time.Now()
		res, err := h.http.Get(endpoint + "/products")
		if err != nil {
			return observed{}, time.Since(start), err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return observed{}, time.Since(start), fmt.Errorf("shop HTTP %d", res.StatusCode)
		}
		got := observed{Storefront: res.Header.Get("X-Shop-Storefront-Workload"), Pricing: res.Header.Get("X-Shop-Pricing-Workload"), StorefrontContext: res.Header.Get("X-Shop-Storefront-Context"), PricingContext: res.Header.Get("X-Shop-Pricing-Context")}
		err = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&got.Body)
		return got, time.Since(start), err
	}
	baseline, _, err := observe(manifest.Baseline.Endpoint)
	if err != nil || baseline.Body.Price != 1200 || baseline.Body.Release != "v1" || baseline.Storefront == "" || baseline.Pricing == "" || baseline.StorefrontContext != "" || baseline.PricingContext != "" {
		t.Fatalf("invalid shop baseline: %+v %v", baseline, err)
	}
	req := func(i int) domain.CreateRequest {
		return domain.CreateRequest{Project: "shop", Baseline: "staging", Name: fmt.Sprintf("capacity-%02d", i), TTL: "20m", Overrides: map[string]domain.ComponentOverride{"pricing": {Image: "envy/shop:v2"}}}
	}
	created := make([]domain.Composition, 20)
	t.Cleanup(func() {
		if t.Failed() {
			// Preserve pod termination reasons before successful cleanup removes
			// the workloads that explain a capacity failure.
			captureCapacityDiagnostics(t, h.kubeconfig)
		}
		// Request all deletions before waiting so drains and namespace GC can overlap.
		for _, c := range created {
			if c.ID != "" {
				h.destroy(c.ID)
			}
		}
		for _, c := range created {
			if c.ID != "" {
				h.absent(c.ID, c.Endpoints["public"].URL)
			}
		}
	})
	start := time.Now()
	results := make([]error, 20)
	var wg sync.WaitGroup
	for i := range created {
		wg.Go(func() {
			code, data, err := h.request("POST", "/v1/compositions", req(i), fmt.Sprintf("shop-capacity-%02d", i))
			if err != nil {
				results[i] = err
				return
			}
			if code != 202 {
				results[i] = fmt.Errorf("create HTTP %d: %s", code, data)
				return
			}
			results[i] = json.Unmarshal(data, &created[i])
		})
	}
	wg.Wait()
	for _, err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	status, body, err = h.request("POST", "/v1/compositions", req(21), "")
	if err != nil || status != 429 {
		t.Fatalf("twenty-first composition did not hit cap: %d %s %v", status, body, err)
	}
	readyAt := map[string]time.Duration{}
	eventually(t, 5*time.Minute, "all twenty shop compositions ready", func() error {
		code, data, err := h.request("GET", "/v1/compositions?project=shop&limit=100", nil, "")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("list HTTP %d", code)
		}
		var page struct {
			Items []domain.Composition `json:"items"`
		}
		if err = json.Unmarshal(data, &page); err != nil {
			return err
		}
		ready := 0
		for _, c := range page.Items {
			if c.Phase == domain.PhaseReady && c.Endpoints["public"].Ready {
				if c.VerificationLevel != "reachability" {
					return fmt.Errorf("HTTP contract claimed %q verification", c.VerificationLevel)
				}
				for _, condition := range c.Conditions {
					if condition.Type == "RouteVerified" && condition.Status {
						return fmt.Errorf("HTTP claimed routing proof")
					}
				}
				ready++
				if _, ok := readyAt[c.ID]; !ok {
					readyAt[c.ID] = time.Since(start)
				}
				for i := range created {
					if created[i].ID == c.ID {
						created[i] = c
					}
				}
			}
		}
		if ready != 20 {
			return fmt.Errorf("%d/20 ready", ready)
		}
		return nil
	})
	t.Logf("All twenty compositions ready after %.2fs", time.Since(start).Seconds())
	missing, err := h.http.Get(created[0].Endpoints["public"].URL + "/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != 404 || missing.Header.Get(domain.PreviewRouteHeader) != created[0].ID {
		t.Fatal("application 404 lacks the ingress route marker needed for safe withdrawal checks")
	}

	latencies := []float64{}
	// These application-specific assertions prove routing independently of Envy's
	// deliberately weaker HTTP readiness contract, including unique selected pods.
	for round := range 5 {
		pods := map[string]bool{}
		for _, c := range created {
			got, elapsed, err := observe(c.Endpoints["public"].URL)
			latencies = append(latencies, float64(elapsed.Microseconds())/1000)
			if err != nil || got.Body.Price != 990 || got.Body.Release != "v2" || got.Storefront != baseline.Storefront || got.Pricing == baseline.Pricing || got.Pricing == "" || pods[got.Pricing] || got.StorefrontContext != c.ID || got.PricingContext != c.ID {
				t.Fatalf("routing at capacity round=%d id=%s got=%+v err=%v", round, c.ID, got, err)
			}
			pods[got.Pricing] = true
			if shared, _, err := observe(manifest.Baseline.Endpoint); err != nil || shared != baseline {
				t.Fatalf("baseline changed at capacity: %+v %v", shared, err)
			}
		}
	}
	h.controller("0")
	got, _, err := observe(created[0].Endpoints["public"].URL)
	h.controller("1")
	if err != nil || got.PricingContext != created[0].ID {
		t.Fatal("preview depended on control-plane availability")
	}
	readiness := []float64{}
	for _, elapsed := range readyAt {
		readiness = append(readiness, elapsed.Seconds())
	}
	slices.Sort(readiness)
	slices.Sort(latencies)
	metrics := map[string]any{"compositions": 20, "preview_requests": len(latencies), "baseline_requests": len(latencies), "ready_p50_seconds": readiness[9], "ready_p95_seconds": readiness[18], "request_p50_ms": latencies[49], "request_p95_ms": latencies[94], "request_max_ms": latencies[99]}
	data, _ = json.MarshalIndent(metrics, "", "  ")
	t.Logf("Development capacity measurements: %s", data)
	if dir := os.Getenv("ENVY_STATE_DIR"); dir != "" {
		if err = os.WriteFile(filepath.Join(dir, "capacity.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func captureCapacityDiagnostics(t *testing.T, kubeconfig string) {
	t.Helper()
	dir := os.Getenv("ENVY_STATE_DIR")
	if dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, item := range []struct {
		name string
		args []string
	}{
		{"capacity-pods.json", []string{"get", "pods", "-A", "-o", "json"}},
		{"capacity-events.json", []string{"get", "events", "-A", "-o", "json"}},
		{"capacity-server.log", []string{"logs", "-n", "envy-system", "deployment/envy-server", "--tail=500"}},
	} {
		cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", kubeconfig}, item.args...)...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("capture %s: %v", item.name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, item.name), data, 0o600); err != nil {
			t.Logf("save %s: %v", item.name, err)
		}
	}
}

//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestVerificationEvidenceHTTPAndChain(t *testing.T) {
	h := newHarness(t)
	read := func(id string) domain.VerificationPage {
		t.Helper()
		code, data, err := h.request("GET", "/v1/compositions/"+id+"/verification", nil, "")
		if err != nil || code != 200 {
			t.Fatalf("verification: %d %s %v", code, data, err)
		}
		var page domain.VerificationPage
		if err = json.Unmarshal(data, &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	diagnose := func(id string) domain.Diagnosis {
		t.Helper()
		code, data, err := h.request("GET", "/v1/compositions/"+id+"/diagnosis", nil, "")
		if err != nil || code != 200 {
			t.Fatalf("diagnosis: %d %s %v", code, data, err)
		}
		var out domain.Diagnosis
		if err = json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	chain := h.create("verification-chain", "envy/service-b:v2", "10m", "")
	t.Cleanup(func() { h.destroy(chain.ID); h.wait(chain.ID, "destroyed") })
	chain = h.wait(chain.ID, "ready")
	page := read(chain.ID)
	if len(page.Items) == 0 || page.Items[0].Kind != "envy-chain" || page.Items[0].Outcome != "passed" || page.Items[0].Freshness != "current" || len(page.Items[0].Hops) != 3 {
		t.Fatalf("missing chain proof: %+v", page)
	}
	if d := diagnose(chain.ID); d.State != "healthy" || d.Verification != "current" || len(d.Blockers) != 0 {
		t.Fatalf("healthy chain diagnosis: %+v", d)
	}
	code, data, err := h.request("PATCH", "/v1/compositions/"+chain.ID, map[string]any{"expected_generation": chain.Generation, "overrides": map[string]any{"service-b": map[string]string{"image": "envy/service-b:v3"}}}, "")
	if err != nil || code != 202 {
		t.Fatalf("update: %d %s %v", code, data, err)
	}
	chain = h.wait(chain.ID, "ready")
	page = read(chain.ID)
	if page.Items[0].Generation != chain.Generation || page.Items[0].Freshness != "current" || page.Items[0].Hops[2].Version != "v3" || len(page.Items) < 2 || page.Items[1].Freshness != "stale" {
		t.Fatalf("wrong revision evidence: %+v", page)
	}
	beforeDrift := page.Items[0].BaselineFingerprint
	h.kubectl("-n", "envy-baseline", "set", "image", "deployment/service-a", "service-a=envy/service-a:v2")
	t.Cleanup(func() {
		h.kubectl("-n", "envy-baseline", "set", "image", "deployment/service-a", "service-a=envy/service-a:v1")
	})
	h.kubectl("-n", "envy-baseline", "rollout", "status", "deployment/service-a", "--timeout=120s")
	eventually(t, 120*time.Second, "inherited image drift refreshes proof without changing preview generation", func() error {
		checks := read(chain.ID)
		if len(checks.Items) < 3 || checks.Items[0].Generation != chain.Generation || checks.Items[0].Freshness != "current" || checks.Items[0].BaselineFingerprint == beforeDrift || checks.Items[1].Freshness != "stale" {
			return fmt.Errorf("baseline drift did not refresh and retain evidence: %+v", checks)
		}
		return nil
	})
	h.kubectl("-n", "envy-baseline", "set", "image", "deployment/service-a", "service-a=envy/service-a:v1")
	h.kubectl("-n", "envy-baseline", "rollout", "status", "deployment/service-a", "--timeout=120s")
	h.destroy(chain.ID)
	h.wait(chain.ID, "destroyed")
	if retained := read(chain.ID); len(retained.Items) < 2 {
		t.Fatal("evidence lost on destruction")
	}
	raw, err := os.ReadFile("../../examples/shop/application.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest domain.CatalogManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Baseline.Endpoint = strings.Replace(h.preview, "baseline.envy.localhost", "shop.envy.localhost", 1)
	code, data, err = h.request("POST", "/v1/catalog/apply", manifest, "")
	if err != nil || code != 200 {
		t.Fatalf("shop registration: %d %s %v", code, data, err)
	}
	code, data, err = h.request("POST", "/v1/compositions", domain.CreateRequest{Project: "shop", Baseline: "staging", Name: "verification-http", TTL: "10m", Overrides: map[string]domain.ComponentOverride{"pricing": {Image: "envy/shop:v2"}}}, "")
	if err != nil || code != 202 {
		t.Fatalf("HTTP preview: %d %s %v", code, data, err)
	}
	var httpPreview composition
	if err = json.Unmarshal(data, &httpPreview); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.destroy(httpPreview.ID); h.wait(httpPreview.ID, "destroyed") })
	h.wait(httpPreview.ID, "ready")
	page = read(httpPreview.ID)
	if len(page.Items) == 0 || page.Items[0].Kind != "http" || page.Items[0].Outcome != "passed" || page.Items[0].Freshness != "current" || len(page.Items[0].Hops) != 0 || len(page.Items[0].Probes) != 2 {
		t.Fatalf("HTTP evidence: %+v", page)
	}
	if d := diagnose(httpPreview.ID); d.State != "healthy" || d.Verification != "current" {
		t.Fatalf("HTTP reachability diagnosis: %+v", d)
	}
}

//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

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
	chain := h.create("verification-chain", "envy/service-b:v2", "10m", "")
	t.Cleanup(func() { h.destroy(chain.ID); h.wait(chain.ID, "destroyed") })
	chain = h.wait(chain.ID, "ready")
	page := read(chain.ID)
	if len(page.Items) == 0 || page.Items[0].Kind != "envy-chain" || page.Items[0].Outcome != "passed" || len(page.Items[0].Hops) != 3 {
		t.Fatalf("missing chain proof: %+v", page)
	}
	code, data, err := h.request("PATCH", "/v1/compositions/"+chain.ID, map[string]any{"expected_generation": chain.Generation, "overrides": map[string]any{"service-b": map[string]string{"image": "envy/service-b:v3"}}}, "")
	if err != nil || code != 202 {
		t.Fatalf("update: %d %s %v", code, data, err)
	}
	chain = h.wait(chain.ID, "ready")
	page = read(chain.ID)
	if page.Items[0].Generation != chain.Generation || page.Items[0].Hops[2].Version != "v3" {
		t.Fatalf("wrong revision evidence: %+v", page)
	}
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
	if len(page.Items) == 0 || page.Items[0].Kind != "http" || page.Items[0].Outcome != "passed" || len(page.Items[0].Hops) != 0 || len(page.Items[0].Probes) != 2 {
		t.Fatalf("HTTP evidence: %+v", page)
	}
}

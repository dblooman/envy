//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestRegisteredApplication(t *testing.T) {
	h := newHarness(t)
	original := h.baseline()
	// These are externally deployed, borrowed workloads. Catalog registration
	// itself never creates or adopts them.
	manifest, err := os.ReadFile("../../deploy/kubernetes/baseline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	data := strings.ReplaceAll(string(manifest), "envy-baseline", "envy-orders")
	data = strings.ReplaceAll(data, "baseline.envy.localhost", "orders.envy.localhost")
	path := filepath.Join(t.TempDir(), "orders.yaml")
	if err = os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	h.kubectl("apply", "-f", path)
	for _, id := range []string{"gateway", "service-a", "service-b"} {
		h.kubectl("-n", "envy-orders", "rollout", "status", "deployment/"+id, "--timeout=90s")
	}
	endpoint := strings.Replace(h.preview, "baseline.envy.localhost", "orders.envy.localhost", 1)
	var borrowed []hop
	eventually(t, 30*time.Second, "orders baseline reachable", func() error { var err error; borrowed, err = h.chain(endpoint, "", "v1", ""); return err })
	post := func(path string, value any, status int) []byte {
		t.Helper()
		code, body, err := h.request(http.MethodPost, path, value, "")
		if err != nil || code != status {
			t.Fatalf("register %s: %d %s %v", path, code, body, err)
		}
		return body
	}
	post("/v1/projects", domain.Project{ID: "orders", Name: "Orders"}, 201)
	profiles := map[string]domain.Component{}
	for _, id := range []string{"gateway", "service-a", "service-b"} {
		c := domain.Component{ID: id, Project: "orders", Protocol: "http", Port: 8080, Profile: "http-small", HealthPath: "/healthz", ReadinessPath: "/readyz", Overridable: id == "service-a"}
		if id == "service-a" {
			c.Env = map[string]string{"DOWNSTREAM_URL": "http://service-b.envy-orders.svc.cluster.local:8080"}
		}
		profiles[id] = c
		post("/v1/projects/orders/components", c, 201)
	}
	post("/v1/projects/orders/components", profiles["service-a"], 409)
	b := domain.Baseline{ID: "staging", Project: "orders", Revision: "orders-v1", Endpoint: endpoint, Routing: domain.BaselineRouting{Namespace: "envy-orders", Gateway: "envy-preview", EntryComponent: "gateway"}, Verification: domain.VerificationContract{Kind: "envy-chain", Chain: []string{"gateway", "service-a", "service-b"}}, Components: map[string]domain.BaselineBinding{}}
	for id := range profiles {
		b.Components[id] = domain.BaselineBinding{ServiceHost: id + ".envy-orders.svc.cluster.local", Port: 8080, Image: "envy/" + id + ":v1"}
	}
	post("/v1/projects/orders/baselines", b, 201)
	b.ID = "collision"
	b.Endpoint = strings.Replace(endpoint, "orders.envy.localhost", "other.envy.localhost", 1)
	// Unknown ingress rejects the registration before any resources are mutated.
	post("/v1/projects/orders/baselines", b, 400)
	cbody := post("/v1/compositions", domain.CreateRequest{Project: "orders", Baseline: "staging", Name: "middle-component", Overrides: map[string]domain.ComponentOverride{"service-a": {Image: "envy/service-a:v2"}}, TTL: "10m"}, 202)
	var c composition
	if err = json.Unmarshal(cbody, &c); err != nil {
		t.Fatal(err)
	}
	defer func() { h.destroy(c.ID); h.absent(c.ID, c.Endpoints["public"].URL) }()
	// A demo composition stays active while another application's domain changes.
	demo := h.create("catalog-concurrent", "envy/service-b:v2", "10m", "")
	defer func() { h.destroy(demo.ID); h.absent(demo.ID, demo.Endpoints["public"].URL) }()
	c = h.wait(c.ID, "ready")
	demo = h.wait(demo.ID, "ready")
	verify := func() {
		t.Helper()
		code, chain, err := h.traffic(c.Endpoints["public"].URL, "composition=hostile,tenant=x")
		if err != nil || code != 200 || len(chain) != 3 {
			t.Fatalf("orders preview %d %+v %v", code, chain, err)
		}
		for i, hop := range chain {
			version := "v1"
			deployment := "baseline"
			if i == 1 {
				version = "v2"
				deployment = c.ID
			}
			if hop.Service != borrowed[i].Service || hop.Version != version || hop.Composition != c.ID || hop.DeploymentComposition != deployment {
				t.Fatalf("unexpected orders hop %+v", hop)
			}
			if i != 1 && hop.WorkloadID != borrowed[i].WorkloadID {
				t.Fatal("inherited orders pod changed")
			}
			if i == 1 && hop.WorkloadID == borrowed[i].WorkloadID {
				t.Fatal("middle override fell back to baseline")
			}
		}
		if _, err = h.chain(demo.Endpoints["public"].URL, demo.ID, "v2", ""); err != nil {
			t.Fatal(err)
		}
		baseline, err := h.chain(endpoint, "", "v1", "composition="+c.ID)
		if err != nil {
			t.Fatal(err)
		}
		for i := range baseline {
			if baseline[i].WorkloadID != borrowed[i].WorkloadID {
				t.Fatal("borrowed baseline changed")
			}
		}
		for i, hop := range h.baseline() {
			if hop.WorkloadID != original[i].WorkloadID {
				t.Fatal("demo baseline changed")
			}
		}
	}
	verify()
	h.controller("0")
	h.controller("1")
	h.wait(c.ID, "ready")
	verify()
	code, logs, err := h.request("GET", "/v1/compositions/"+c.ID+"/components/service-a/logs", nil, "")
	if err != nil || code != 200 || !strings.Contains(string(logs), "service-a") {
		t.Fatalf("registered override logs: %d %s %v", code, logs, err)
	}
	t.Log(fmt.Sprintf("Registered orders, overrode service-a, retained both routing domains and baseline identities: %s", c.ID))
}

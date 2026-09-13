package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/demo/protocol"
	"github.com/dblooman/envy/internal/domain"
)

func acceptHTTPSComposition(t *testing.T, ctx context.Context, ns, suffix, token string, cert []byte, client *http.Client) {
	t.Helper()
	run := func(input []byte, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		cmd.Stdin = bytes.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("kubectl %v: %v: %s", args, err, out)
		}
		return out
	}
	appNS := ns + "-app"
	// The namespace is unique to this installation. Always clean borrowed fixtures,
	// including when provisioning or verification fails.
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(clean, "kubectl", "delete", "namespace", appNS, "--ignore-not-found=true", "--wait=false").CombinedOutput()
		if err != nil {
			t.Errorf("baseline cleanup: %v: %s", err, out)
		}
	})
	manifest, err := os.ReadFile("../../deploy/kubernetes/baseline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(manifest), "envy-baseline", appNS)
	text = strings.ReplaceAll(text, "baseline.envy.localhost", "baseline."+suffix)
	text = strings.ReplaceAll(text, "envy-local", "helm-smoke")
	for _, part := range strings.Split(text, "\n---\n") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(part), &obj); err != nil {
			t.Fatal(err)
		}
		if obj["kind"] == "Namespace" {
			obj["metadata"].(map[string]any)["labels"] = map[string]string{"istio.io/rev": "default"}
		}
		if obj["kind"] == "Gateway" {
			obj["spec"].(map[string]any)["servers"] = []any{map[string]any{"port": map[string]any{"number": 443, "name": "https", "protocol": "HTTPS"}, "hosts": []string{"*." + suffix}, "tls": map[string]string{"mode": "SIMPLE", "credentialName": ns + "-tls"}}}
		}
		body, _ := json.Marshal(obj)
		run(body, "create", "-f", "-")
	}
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"gateways": []string{appNS + "/envy-preview"}}})
	run(nil, "-n", ns, "patch", "virtualservice/https-acceptance", "--type=merge", "-p", string(patch))
	run(nil, "-n", ns, "delete", "gateway/https-acceptance")
	for _, name := range []string{"gateway", "service-a", "service-b"} {
		run(nil, "-n", appNS, "rollout", "status", "deployment/"+name, "--timeout=90s")
	}
	ca, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]string{"name": "acceptance-ca", "namespace": ns}, "data": map[string]string{"ca.crt": string(cert)}})
	run(ca, "create", "-f", "-")
	helm, err := filepath.Abs("../../.envy/bin/helm")
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the chart's real CA mount and server configuration, not an injected
	// verification client. Reuse credentials without exposing their contents.
	cmd := exec.CommandContext(ctx, helm, "upgrade", ns, "../../deploy/helm/envy", "-n", ns, "--reuse-values", "--set", "auth.mode=token", "--set", "runtime.caConfigMap.name=acceptance-ca", "--set", "runtime.previewBaseURL=https://"+suffix, "--set", "runtime.baselineHost=baseline."+suffix, "--wait", "--timeout=120s")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("configure control-plane CA: %v: %s", err, out)
	}
	apiURL := "https://api." + suffix
	request := func(method, path string, value any, want int) []byte {
		t.Helper()
		var body []byte
		if value != nil {
			body, _ = json.Marshal(value)
		}
		req, _ := http.NewRequestWithContext(ctx, method, apiURL+path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, res.StatusCode, want, out)
		}
		return out
	}
	// Rollout readiness precedes Envoy endpoint convergence.
	deadline := time.Now().Add(30 * time.Second)
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", apiURL+"/v1/session", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("API did not converge after CA rollout")
		}
		time.Sleep(250 * time.Millisecond)
	}
	request("POST", "/v1/projects", domain.Project{ID: "tls", Name: "TLS acceptance"}, 201)
	baseline := domain.Baseline{ID: "staging", Project: "tls", Revision: "tls-v1", Endpoint: "https://baseline." + suffix, Routing: domain.BaselineRouting{Namespace: appNS, Gateway: "envy-preview", EntryComponent: "gateway"}, Verification: domain.VerificationContract{Kind: "envy-chain", Chain: []string{"gateway", "service-a", "service-b"}}, Components: map[string]domain.BaselineBinding{}}
	for _, name := range baseline.Verification.Chain {
		request("POST", "/v1/projects/tls/components", domain.Component{ID: name, Project: "tls", Protocol: "http", Port: 8080, Profile: "http-small", HealthPath: "/healthz", ReadinessPath: "/readyz", Overridable: name == "service-b"}, 201)
		baseline.Components[name] = domain.BaselineBinding{ServiceHost: name + "." + appNS + ".svc.cluster.local", Port: 8080, Image: "envy/" + name + ":v1"}
	}
	deadline = time.Now().Add(35 * time.Second)
	for {
		res, err := client.Get(baseline.Endpoint)
		status := 0
		var detail []byte
		if err == nil {
			status = res.StatusCode
			detail, _ = io.ReadAll(io.LimitReader(res.Body, 4096))
			res.Body.Close()
			if status == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("baseline HTTPS route did not converge: status=%d body=%s error=%v", status, detail, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
	request("POST", "/v1/projects/tls/baselines", baseline, 201)
	traffic := func(endpoint, id, version string) []protocol.Hop {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		req.Header.Set("baggage", "composition=hostile,tenant=test")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body protocol.Response
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil || res.StatusCode != 200 || len(body.Chain) != 3 {
			t.Fatalf("invalid chain at %s: %d %+v %v", endpoint, res.StatusCode, body, err)
		}
		for i, hop := range body.Chain {
			want := "v1"
			if i == 2 {
				want = version
			}
			if hop.Service != baseline.Verification.Chain[i] || hop.Version != want || hop.Composition != id || hop.WorkloadID == "" {
				t.Fatalf("incorrect TLS hop: %+v", hop)
			}
		}
		return body.Chain
	}
	original := traffic(baseline.Endpoint, "", "v1")
	var composition domain.Composition
	if err := json.Unmarshal(request("POST", "/v1/compositions", domain.CreateRequest{Project: "tls", Baseline: "staging", Name: "https-override", TTL: "10m", Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "envy/service-b:v2"}}}, 202), &composition); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if t.Failed() {
			clean, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = exec.CommandContext(clean, "kubectl", "delete", "namespace", "envy-"+composition.ID, "--ignore-not-found=true", "--wait=false").Run()
		}
	}()
	wait := func(phase domain.Phase) {
		t.Helper()
		deadline := time.Now().Add(120 * time.Second)
		for {
			if err := json.Unmarshal(request("GET", "/v1/compositions/"+composition.ID, nil, 200), &composition); err != nil {
				t.Fatal(err)
			}
			if composition.Phase == phase {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("expected %s: %+v", phase, composition)
			}
			traffic(baseline.Endpoint, "", "v1")
			time.Sleep(time.Second)
		}
	}
	wait(domain.PhaseReady)
	endpoint := composition.Endpoints["public"]
	if !endpoint.Ready || !strings.HasPrefix(endpoint.URL, "https://") {
		t.Fatalf("invalid HTTPS endpoint: %+v", endpoint)
	}
	preview := traffic(endpoint.URL, composition.ID, "v2")
	for i := range preview {
		if i < 2 && preview[i].WorkloadID != original[i].WorkloadID {
			t.Fatal("inherited workload changed")
		}
		if i == 2 && preview[i].WorkloadID == original[i].WorkloadID {
			t.Fatal("override fell back to baseline")
		}
	}
	request("DELETE", "/v1/compositions/"+composition.ID, nil, 202)
	wait(domain.PhaseDestroyed)
	res, err := client.Get(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("destroyed HTTPS endpoint returned %d", res.StatusCode)
	}
	remaining := run(nil, "get", "namespace", "envy-"+composition.ID, "--ignore-not-found=true", "-o", "name")
	if len(bytes.TrimSpace(remaining)) != 0 {
		t.Fatalf("owned namespace remains: %s", remaining)
	}
	after := traffic(baseline.Endpoint, "", "v1")
	for i := range after {
		if after[i].WorkloadID != original[i].WorkloadID {
			t.Fatal("baseline changed during lifecycle")
		}
	}
	t.Logf("HTTPS composition %s verified and destroyed with configured control-plane CA", composition.ID)
}

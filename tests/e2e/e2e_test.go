//go:build e2e

// Package e2e exercises the public API and selected mesh data plane. It requires
// the disposable environment created by make test-e2e.
package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type composition struct {
	ID                 string `json:"id"`
	Phase              string `json:"phase"`
	Generation         int64  `json:"generation"`
	ObservedGeneration int64  `json:"observed_generation"`
	Endpoints          map[string]struct {
		URL   string `json:"url"`
		Ready bool   `json:"ready"`
	} `json:"endpoints"`
}

type hop struct {
	Service               string `json:"service"`
	Version               string `json:"version"`
	Composition           string `json:"composition"`
	WorkloadID            string `json:"workload_id"`
	DeploymentComposition string `json:"deployment_composition"`
}

type harness struct {
	t                               *testing.T
	api, token, preview, kubeconfig string
	http                            *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	api := os.Getenv("ENVY_API_URL")
	if api == "" {
		t.Fatal("ENVY_API_URL is required; run make test-e2e")
	}
	tokenPath := os.Getenv("ENVY_API_TOKEN_FILE")
	var token []byte
	if tokenPath != "" {
		var err error
		token, err = os.ReadFile(tokenPath)
		if err != nil {
			t.Fatalf("read API token file: %v", err)
		}
	}
	port := os.Getenv("ENVY_PREVIEW_PORT")
	if port == "" {
		port = "18080"
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(host, ".envy.localhost") {
			addr = net.JoinHostPort("127.0.0.1", port)
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, addr)
	}}
	scheme := "http"
	if caFile := os.Getenv("ENVY_TEST_CA_FILE"); caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Fatal("invalid test CA")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		scheme = "https"
	}
	h := &harness{t: t, api: strings.TrimRight(api, "/"), token: strings.TrimSpace(string(token)), preview: scheme + "://baseline.envy.localhost:" + port, kubeconfig: os.Getenv("KUBECONFIG"), http: &http.Client{Transport: transport, Timeout: 12 * time.Second}}
	if h.kubeconfig == "" {
		t.Fatal("KUBECONFIG must explicitly select the disposable kind cluster")
	}
	t.Cleanup(transport.CloseIdleConnections)
	return h
}

func (h *harness) request(method, path string, body any, key string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.api+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := h.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, b, err
}

func (h *harness) create(name, image, ttl, key string) composition {
	h.t.Helper()
	code, b, err := h.request("POST", "/v1/compositions", map[string]any{"project": "demo", "baseline": "staging", "name": name, "overrides": map[string]any{"service-b": map[string]string{"image": image}}, "ttl": ttl}, key)
	if err != nil || code != http.StatusAccepted {
		h.t.Fatalf("create: status=%d body=%s err=%v", code, b, err)
	}
	var c composition
	if err = json.Unmarshal(b, &c); err != nil || c.ID == "" {
		h.t.Fatalf("create response: %s (%v)", b, err)
	}
	h.t.Logf("created %s (%s)", c.ID, name)
	return c
}

func (h *harness) get(id string) (composition, error) {
	code, b, err := h.request("GET", "/v1/compositions/"+url.PathEscape(id), nil, "")
	if err != nil {
		return composition{}, err
	}
	if code != 200 {
		return composition{}, fmt.Errorf("GET %s: %d %s", id, code, b)
	}
	var c composition
	err = json.Unmarshal(b, &c)
	return c, err
}

func eventually(t *testing.T, timeout time.Duration, description string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last error
	for {
		last = check()
		if last == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", description, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (h *harness) wait(id, phase string) composition {
	h.t.Helper()
	var result composition
	eventually(h.t, 180*time.Second, "composition "+id+" becomes "+phase, func() error {
		c, err := h.get(id)
		if err != nil {
			return err
		}
		result = c
		if c.Phase != phase {
			return fmt.Errorf("phase=%s", c.Phase)
		}
		if phase == "ready" && (!c.Endpoints["public"].Ready || c.Generation != c.ObservedGeneration) {
			return fmt.Errorf("inconsistent ready response: %+v", c)
		}
		return nil
	})
	return result
}

func (h *harness) traffic(endpoint, baggage string) (int, []hop, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	if baggage != "" {
		req.Header.Add("baggage", baggage)
		req.Header.Add("baggage", "composition=cmp_forged_duplicate")
	}
	res, err := h.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, res.Body)
		return res.StatusCode, nil, nil
	}
	var result struct {
		Chain []hop `json:"chain"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&result)
	return res.StatusCode, result.Chain, err
}

func (h *harness) chain(endpoint, id, bVersion, baggage string) ([]hop, error) {
	code, chain, err := h.traffic(endpoint, baggage)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("traffic status=%d", code)
	}
	if len(chain) != 3 {
		return nil, fmt.Errorf("want three hops, got %+v", chain)
	}
	names := []string{"gateway", "service-a", "service-b"}
	versions := []string{"v1", "v1", bVersion}
	for i, s := range chain {
		if s.Service != names[i] || s.Version != versions[i] || s.Composition != id || s.WorkloadID == "" {
			return nil, fmt.Errorf("unexpected hop %d: %+v", i, s)
		}
		deployment := "baseline"
		if i == 2 && id != "" {
			deployment = id
		}
		if s.DeploymentComposition != deployment {
			return nil, fmt.Errorf("hop %d deployment=%q want %q", i, s.DeploymentComposition, deployment)
		}
	}
	return chain, nil
}

func (h *harness) baseline() []hop {
	h.t.Helper()
	chain, err := h.chain(h.preview, "", "v1", "")
	if err != nil {
		h.t.Fatal(err)
	}
	return chain
}

func (h *harness) kubectl(args ...string) string {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", h.kubeconfig}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("kubectl %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func (h *harness) controller(replicas string) {
	h.kubectl("-n", "envy-system", "scale", "deployment/"+testServerDeployment(), "--replicas="+replicas)
	if replicas == "0" {
		h.kubectl("-n", "envy-system", "wait", "--for=delete", "pod", "-l", testServerSelector(), "--timeout=90s")
	} else {
		h.kubectl("-n", "envy-system", "rollout", "status", "deployment/"+testServerDeployment(), "--timeout=90s")
		// Pod readiness does not establish NodePort/EndpointSlice convergence.
		// Drop connections to the intentionally terminated process and observe
		// the public API before issuing subsequent mutations.
		eventually(h.t, 30*time.Second, "API reachable after controller restart", func() error {
			h.http.CloseIdleConnections()
			code, _, err := h.request("GET", "/v1/projects", nil, "")
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("API returned HTTP %d", code)
			}
			return nil
		})
	}
}

func (h *harness) destroy(id string) {
	h.t.Helper()
	code, b, err := h.request("DELETE", "/v1/compositions/"+id, nil, "")
	if err != nil || code != 202 {
		h.t.Fatalf("delete: %d %s %v", code, b, err)
	}
}

func (h *harness) absent(id, endpoint string) {
	h.t.Helper()
	h.wait(id, "destroyed")
	eventually(h.t, 30*time.Second, "old hostname returns 404", func() error {
		code, _, err := h.traffic(endpoint, "")
		if err != nil {
			return err
		}
		if code != 404 {
			return fmt.Errorf("status=%d", code)
		}
		return nil
	})
	objects := h.kubectl("get", "namespace", "-l", "envy.dev/composition="+id, "-o", "name")
	if objects != "" {
		h.t.Fatalf("owned namespace remains: %s", objects)
	}
}

func TestCompositionLifecycle(t *testing.T) {
	h := newHarness(t)
	baseline := h.baseline()
	first := h.create("first", "envy/service-b:v2", "30m", "e2e-first")
	// The successful API response must be durable even if no provider work ran yet.
	h.controller("0")
	h.baseline()
	h.controller("1")
	first = h.wait(first.ID, "ready")
	second := h.create("second", "envy/service-b:v2", "30m", "e2e-second")
	second = h.wait(second.ID, "ready")
	for i := 0; i < 12; i++ {
		for _, c := range []composition{first, second} {
			if _, err := h.chain(c.Endpoints["public"].URL, c.ID, "v2", "unrelated=hello,composition=cmp_forged"); err != nil {
				t.Fatal(err)
			}
		}
		got, err := h.chain(h.preview, "", "v1", "composition="+first.ID)
		if err != nil {
			t.Fatal(err)
		}
		for j := range got {
			if got[j].WorkloadID != baseline[j].WorkloadID {
				t.Fatalf("baseline workload was replaced: %+v -> %+v", baseline, got)
			}
		}
	}
	// A second request with the same key must not allocate another namespace.
	replay := h.create("first", "envy/service-b:v2", "30m", "e2e-first")
	if replay.ID != first.ID {
		t.Fatal("idempotency key allocated a different composition")
	}
	code, _, err := h.request("POST", "/v1/compositions", map[string]any{"project": "demo", "baseline": "staging", "name": "different", "overrides": map[string]any{"service-b": map[string]string{"image": "envy/service-b:v2"}}, "ttl": "30m"}, "e2e-first")
	if err != nil || code != 409 {
		t.Fatalf("idempotency conflict status=%d err=%v", code, err)
	}
	unknown := strings.Replace(h.preview, "baseline.", "cmp-unknown.", 1)
	code, _, err = h.traffic(unknown, "")
	if err != nil || code != 404 {
		t.Fatalf("unknown hostname status=%d err=%v", code, err)
	}

	// Take a previously working override offline while the reconciler is stopped.
	// Istio must report the failed override instead of using its baseline catch-all.
	h.controller("0")
	ns := h.kubectl("get", "namespace", "-l", "envy.dev/composition="+first.ID, "-o", "jsonpath={.items[0].metadata.name}")
	h.kubectl("-n", ns, "scale", "deployment/service-b", "--replicas=0")
	h.kubectl("-n", ns, "wait", "--for=delete", "pod", "-l", "envy.dev/component=service-b", "--timeout=90s")
	eventually(t, 30*time.Second, "unhealthy override fails visibly", func() error {
		code, chain, err := h.traffic(first.Endpoints["public"].URL, "")
		if err != nil {
			return err
		}
		if code >= 500 {
			return nil
		}
		return fmt.Errorf("status=%d chain=%+v", code, chain)
	})
	h.baseline()
	h.controller("1")
	first = h.wait(first.ID, "ready")
	eventually(t, 60*time.Second, "override recovers after control-plane restart", func() error { _, err := h.chain(first.Endpoints["public"].URL, first.ID, "v2", ""); return err })

	broken := h.create("unavailable-image", "envy/service-b:does-not-exist", "30m", "e2e-broken")
	eventually(t, 120*time.Second, "unavailable image reports failure", func() error {
		c, err := h.get(broken.ID)
		if err != nil {
			return err
		}
		if c.Phase == "ready" {
			t.Fatal("unavailable image became ready")
		}
		if c.Phase != "failed" {
			return fmt.Errorf("phase=%s", c.Phase)
		}
		return nil
	})
	h.destroy(broken.ID)
	h.wait(broken.ID, "destroyed")
	h.destroy(first.ID)
	h.destroy(first.ID)
	h.absent(first.ID, first.Endpoints["public"].URL)
	if _, err := h.chain(second.Endpoints["public"].URL, second.ID, "v2", ""); err != nil {
		t.Fatal(err)
	}
	h.destroy(second.ID)
	h.absent(second.ID, second.Endpoints["public"].URL)
	for i, s := range h.baseline() {
		if s.WorkloadID != baseline[i].WorkloadID {
			t.Fatal("cleanup replaced baseline workloads")
		}
	}
}

func TestExpiryAcrossRestart(t *testing.T) {
	h := newHarness(t)
	c := h.create("expires-while-stopped", "envy/service-b:v2", "5s", "e2e-expiry")
	h.controller("0")
	// This delay tests an elapsed TTL, not Kubernetes readiness.
	time.Sleep(6 * time.Second)
	h.controller("1")
	h.wait(c.ID, "destroyed")
	h.absent(c.ID, c.Endpoints["public"].URL)
	h.baseline()
}

func testServerDeployment() string {
	if v := os.Getenv("ENVY_TEST_SERVER_DEPLOYMENT"); v != "" {
		return v
	}
	return "envy-server"
}

func testServerSelector() string {
	if v := os.Getenv("ENVY_TEST_SERVER_SELECTOR"); v != "" {
		// Helm migration and preflight Jobs share application labels, but
		// scaling the server must not wait for completed Job pods to vanish.
		return v + ",!job-name"
	}
	return "app=envy-server"
}

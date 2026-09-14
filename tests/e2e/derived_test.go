//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/dblooman/envy/internal/domain"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDerivedPreviewsWithArgo(t *testing.T) {
	if os.Getenv("ENVY_DERIVED_ARGO_TEST") != "1" {
		t.Skip("run make test-derived-e2e in a disposable cluster")
	}
	h := newHarness(t)
	// Load digest aliases into kind's image store, using the already built fixture images.
	pin := func(version string) string {
		t.Helper()
		node := os.Getenv("ENVY_CLUSTER_NAME") + "-control-plane"
		output, err := exec.Command("docker", "exec", node, "ctr", "-n", "k8s.io", "images", "ls").Output()
		if err != nil {
			t.Fatal(err)
		}
		tag := "docker.io/envy/service-b:" + version
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 2 && fields[0] == tag {
				image := "docker.io/envy/service-b@" + fields[2]
				if out, e := exec.Command("docker", "exec", node, "ctr", "-n", "k8s.io", "images", "tag", "--force", tag, image).CombinedOutput(); e != nil {
					t.Fatalf("pin fixture %s %v", out, e)
				}
				return image
			}
		}
		t.Fatal("fixture image missing")
		return ""
	}
	image2, image3 := pin("v2"), pin("v3")
	path := "/v1/projects/demo/baselines/staging/components/service-b/preview-profile"
	discover := func() domain.PreviewReport {
		t.Helper()
		code, body, err := h.request("POST", path+"/discover", domain.PreviewSelection{}, "")
		if err != nil || code != 200 {
			t.Fatalf("discovery %d %s %v", code, body, err)
		}
		var report domain.PreviewReport
		if json.Unmarshal(body, &report) != nil || len(report.Blockers) > 0 {
			t.Fatalf("discovery blockers: %s", body)
		}
		if strings.Contains(string(body), "fake-fixture") {
			t.Fatal("Secret payload leaked")
		}
		return report
	}
	report := discover()
	code, body, err := h.request("POST", path+"/approve", domain.PreviewApproval{Selection: report.Selection, Inspection: report.Inspection, ConfirmConnectivity: true}, "")
	if err != nil || code != 200 {
		t.Fatalf("approval %d %s %v", code, body, err)
	}
	// Exercise CLI parity against the same authoritative profile.
	cli := exec.Command(os.Getenv("ENVY_CLI_BINARY"), "preview-profile", "inspect", "--project", "demo", "--baseline", "staging", "--component", "service-b")
	if output, err := cli.CombinedOutput(); err != nil || !strings.Contains(string(output), `"revision": 1`) && !strings.Contains(string(output), `"revision":1`) {
		t.Fatalf("CLI inspect %s %v", output, err)
	}
	created := []composition{}
	t.Cleanup(func() {
		for _, c := range created {
			_, _, _ = h.request("DELETE", "/v1/compositions/"+c.ID, nil, "")
		}
	})
	create := func(name, image, ttl string) composition {
		t.Helper()
		req := domain.CreateRequest{Project: "demo", Baseline: "staging", Name: name, TTL: ttl, ExpectedPreviewRevisions: map[string]int64{"service-b": 1}, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: image}}}
		code, body, err := h.request("POST", "/v1/compositions", req, name)
		if err != nil || code != 202 {
			t.Fatalf("create %d %s %v", code, body, err)
		}
		var c composition
		if json.Unmarshal(body, &c) != nil {
			t.Fatal("invalid composition")
		}
		created = append(created, c)
		return h.wait(c.ID, "ready")
	}
	a := create("derived-a", image2, "4m")
	b := create("derived-b", image3, "4m")
	for _, check := range []struct{ url, id, version string }{{h.preview, "", "v1"}, {a.Endpoints["public"].URL, a.ID, "v2"}, {b.Endpoints["public"].URL, b.ID, "v3"}} {
		if _, err := h.chain(check.url, check.id, check.version, ""); err != nil {
			t.Fatal(err)
		}
	}
	readCopies := func(id, want string) {
		t.Helper()
		var secrets struct {
			Items []struct {
				Data map[string]string `json:"data"`
			}
		}
		data := h.kubectl("-n", "envy-"+id, "get", "secrets", "-o", "json")
		if json.Unmarshal([]byte(data), &secrets) != nil || len(secrets.Items) != 1 {
			t.Fatal("missing dependency copy")
		}
		if secrets.Items[0].Data["value"] != base64.StdEncoding.EncodeToString([]byte("fake-fixture-"+want)) {
			t.Fatal("wrong captured Secret version")
		}
		var configs struct {
			Items []struct {
				Data map[string]string `json:"data"`
			}
		}
		data = h.kubectl("-n", "envy-"+id, "get", "configmaps", "-l", "envy.dev/component=service-b", "-o", "json")
		if json.Unmarshal([]byte(data), &configs) != nil || len(configs.Items) != 1 || configs.Items[0].Data["value"] != want {
			t.Fatal("wrong captured ConfigMap version")
		}
	}
	readCopies(a.ID, "first")
	readCopies(b.ID, "first")
	h.kubectl("-n", "argocd", "exec", "deployment/preview-git", "--", "sh", "-ec", `sed -i 's/first/second/g; s|envy/service-b:v1|envy/service-b:v2|g' /repos/baseline/baseline.json; git -C /repos/baseline add .; git -C /repos/baseline commit -m advance-main`)
	revision := h.kubectl("-n", "argocd", "exec", "deployment/preview-git", "--", "git", "-C", "/repos/baseline", "rev-parse", "HEAD")
	h.kubectl("-n", "argocd", "annotate", "application/preview-baseline", "argocd.argoproj.io/refresh=hard", "--overwrite")
	eventually(t, 120*time.Second, "Argo advances main", func() error {
		got := h.kubectl("-n", "argocd", "get", "application/preview-baseline", "-o", "jsonpath={.status.sync.revision}:{.status.sync.status}:{.status.health.status}")
		if got != revision+":Synced:Healthy" {
			return fmt.Errorf("sync %s", got)
		}
		return nil
	})
	if _, err := h.chain(h.preview, "", "v2", ""); err != nil {
		t.Fatal(err)
	}
	readCopies(a.ID, "first")
	readCopies(b.ID, "first")
	fresh := discover()
	if fresh.Contract != report.Contract || fresh.Inspection == report.Inspection {
		t.Fatal("routine main change invalidated approval or provenance did not advance")
	}
	c := create("derived-current", image2, "30s")
	readCopies(c.ID, "second")
	// Restart only the control plane; its persistent plan must retain old copies.
	h.kubectl("-n", "envy-system", "rollout", "restart", "deployment/envy-server")
	h.kubectl("-n", "envy-system", "rollout", "status", "deployment/envy-server", "--timeout=80s")
	h.http.CloseIdleConnections()
	eventually(t, 30*time.Second, "API accepts update after restart", func() error {
		code, body, err = h.request("PATCH", "/v1/compositions/"+a.ID, domain.UpdateRequest{ExpectedGeneration: 1, ExpectedPreviewRevisions: map[string]int64{"service-b": 1}, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: image3}}}, "derived-update-a")
		if err != nil {
			return err
		}
		if code != 202 {
			return fmt.Errorf("update %d %s", code, body)
		}
		return nil
	})
	h.wait(a.ID, "ready")
	readCopies(a.ID, "first")
	if _, err := h.chain(a.Endpoints["public"].URL, a.ID, "v3", ""); err != nil {
		t.Fatal(err)
	}
	// A broken override stays explicit; it must never be reported as baseline success.
	h.kubectl("-n", "envy-"+b.ID, "scale", "deployment/service-b", "--replicas=0")
	eventually(t, 60*time.Second, "override heals after external failure", func() error {
		if replicas := h.kubectl("-n", "envy-"+b.ID, "get", "deployment/service-b", "-o", "jsonpath={.spec.replicas}"); replicas != "1" {
			return fmt.Errorf("waiting for desired replicas to be restored")
		}
		_, err := h.chain(b.Endpoints["public"].URL, b.ID, "v3", "")
		return err
	})
	h.wait(c.ID, "destroyed")
	h.absent(c.ID, c.Endpoints["public"].URL)
	eventually(t, 300*time.Second, "both initial previews expire", func() error {
		for _, id := range []string{a.ID, b.ID} {
			got, err := h.get(id)
			if err != nil {
				return err
			}
			if got.Phase != "destroyed" {
				return fmt.Errorf("%s still %s", id, got.Phase)
			}
		}
		return nil
	})
	h.absent(a.ID, a.Endpoints["public"].URL)
	h.absent(b.ID, b.Endpoints["public"].URL)
	if _, err := h.chain(h.preview, "", "v2", ""); err != nil {
		t.Fatal(err)
	}
	// Assert the controller never received cluster-wide Secret read permissions.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", h.kubeconfig, "auth", "can-i", "get", "secrets", "--all-namespaces", "--as=system:serviceaccount:envy-system:envy-server")
	output, _ := command.Output()
	if strings.TrimSpace(string(output)) != "no" {
		t.Fatal("controller has cluster-wide Secret read access")
	}
}

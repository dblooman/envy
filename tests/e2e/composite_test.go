//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestCompositePreviewLifecycle(t *testing.T) {
	if os.Getenv("ENVY_COMPOSITE_TEST") != "1" {
		t.Skip("run make test-composite-e2e in a disposable cluster")
	}

	h := newHarness(t)
	h.preview = strings.Replace(h.preview, "baseline.envy.localhost", "composite.envy.localhost", 1)
	helper := os.Getenv("ENVY_COMPOSITE_HELPER_IMAGE")
	if !strings.Contains(helper, "@sha256:") {
		t.Fatal("digest-pinned helper image is required")
	}

	image2, image3 := compositePinImage(t, "v2"), compositePinImage(t, "v3")
	compositeRegisterCatalog(t, h)
	path := "/v1/projects/composite/baselines/staging/components/service-b/preview-profile"
	selection := domain.PreviewSelection{Deployment: "service-b", Container: "application"}
	code, body, err := h.request(http.MethodPost, path+"/discover", selection, "")
	if err != nil || code != http.StatusOK {
		t.Fatalf("composite discovery: %d %s %v", code, body, err)
	}

	var report domain.PreviewReport
	if err := json.Unmarshal(body, &report); err != nil || len(report.Blockers) != 0 {
		t.Fatalf("composite discovery blockers: %s (%v)", body, err)
	}

	if report.Source.Container != "application" || len(report.Dependencies) != 8 {
		t.Fatalf("discovery did not capture named application and every container dependency: %+v", report)
	}

	for _, secret := range []string{"synthetic-opaque-app-payload", "synthetic-opaque-init-payload", "synthetic-opaque-native-payload", "synthetic-opaque-proxy-payload"} {
		if strings.Contains(string(body), secret) {
			t.Fatal("discovery returned a synthetic Secret payload")
		}
	}

	wrong := selection
	wrong.Container = "proxy"
	code, body, err = h.request(http.MethodPost, path+"/discover", wrong, "")
	if err != nil || code == http.StatusOK && !strings.Contains(string(body), `"blockers":["`) {
		t.Fatalf("policy allowed selecting the proxy as application: %d %s %v", code, body, err)
	}

	var previous domain.PreviewProfile
	code, body, err = h.request(http.MethodGet, path, nil, "")
	if err != nil || code != http.StatusNotFound && code != http.StatusOK {
		t.Fatalf("inspect approval: %d %s %v", code, body, err)
	}

	if code == http.StatusOK {
		if err := json.Unmarshal(body, &previous); err != nil {
			t.Fatal(err)
		}
	}

	code, body, err = h.request(http.MethodPost, path+"/approve", domain.PreviewApproval{
		Selection: report.Selection, Inspection: report.Inspection, ExpectedRevision: previous.Revision, ConfirmConnectivity: true,
	}, "")
	if err != nil || code != http.StatusOK {
		t.Fatalf("composite approval: %d %s %v", code, body, err)
	}

	var approved domain.PreviewProfile
	if err := json.Unmarshal(body, &approved); err != nil || approved.Revision < 1 {
		t.Fatalf("invalid composite approval: %s (%v)", body, err)
	}

	t.Log("named application and supporting execution approved")

	run := fmt.Sprintf("%x", time.Now().UnixNano())
	created := []composition{}
	t.Cleanup(func() {
		if t.Failed() {
			compositeDiagnostics(t, h, created)
		}

		for _, c := range created {
			_, _, _ = h.request(http.MethodDelete, "/v1/compositions/"+c.ID, nil, "")
		}
	})
	create := func(name, image string) composition {
		t.Helper()
		name += "-" + run
		req := domain.CreateRequest{
			Project: "composite", Baseline: "staging", Name: name, TTL: "10m",
			Overrides: map[string]domain.ComponentOverride{},
		}
		if image != "" {
			req.ExpectedPreviewRevisions = map[string]int64{"service-b": approved.Revision}
			req.Overrides["service-b"] = domain.ComponentOverride{Image: image}
		}

		code, body, err := h.request(http.MethodPost, "/v1/compositions", req, name)
		if err != nil || code != http.StatusAccepted {
			t.Fatalf("create composite: %d %s %v", code, body, err)
		}

		var c composition
		if err := json.Unmarshal(body, &c); err != nil || c.ID == "" {
			t.Fatalf("invalid composition: %s (%v)", body, err)
		}

		created = append(created, c)
		return h.wait(c.ID, "ready")
	}
	a := create("composite-a", image2)
	b := create("composite-b", image3)
	inherited := create("composite-inherited", "")
	compositeAssertInheritedLogs(t, h, inherited.ID)
	t.Log("two independent composite previews ready; inherited application logs verified")
	for _, check := range []struct{ url, id, version string }{
		{h.preview, "", "v1"}, {a.Endpoints["public"].URL, a.ID, "v2"}, {b.Endpoints["public"].URL, b.ID, "v3"},
	} {
		if _, err := h.chain(check.url, check.id, check.version, ""); err != nil {
			t.Fatal(err)
		}
	}

	before := compositeAssertWorkload(t, h, a.ID, image2, helper)
	compositeAssertWorkload(t, h, b.ID, image3, helper)
	for container, message := range map[string]string{
		"service-b": "demo listening", "bootstrap": "bootstrap completed",
		"proxy": "proxy fixture listening", "native-helper": "native fixture listening",
	} {
		compositeAssertLogs(t, h, a.ID, container, message, false)
	}

	code, body, err = h.request(http.MethodPatch, "/v1/compositions/"+a.ID, domain.UpdateRequest{
		ExpectedGeneration: 1, ExpectedPreviewRevisions: map[string]int64{"service-b": approved.Revision},
		Overrides: map[string]domain.ComponentOverride{"service-b": {Image: image3}},
	}, "composite-update-"+a.ID)
	if err != nil || code != http.StatusAccepted {
		t.Fatalf("composite update: %d %s %v", code, body, err)
	}

	updated := h.wait(a.ID, "ready")
	if updated.Generation != 2 || updated.Endpoints["public"].URL != a.Endpoints["public"].URL {
		t.Fatal("image update changed URL or did not advance generation")
	}

	after := compositeAssertWorkload(t, h, a.ID, image3, helper)
	before.Containers[1].Image = image3
	if !reflect.DeepEqual(before, after) {
		t.Fatal("application image update changed captured supporting execution or dependencies")
	}

	if _, err := h.chain(a.Endpoints["public"].URL, a.ID, "v3", ""); err != nil {
		t.Fatal(err)
	}

	t.Log("application image update preserved supporting containers and dependencies")

	// Fail a native sidecar, whose status is in initContainerStatuses. The
	// namespace-local emptyDir marker makes the failure persist across restart.
	h.kubectl("-n", "envy-"+b.ID, "exec", "deployment/service-b", "-c", "proxy", "--", "/fixture", "fail-native")
	eventually(t, 100*time.Second, "native sidecar failure is named in composition diagnostics", func() error {
		code, body, err := h.request(http.MethodGet, "/v1/compositions/"+b.ID, nil, "")
		if err != nil || code != http.StatusOK {
			return fmt.Errorf("diagnostics request: status=%d error=%w", code, err)
		}

		var failed composition
		if err := json.Unmarshal(body, &failed); err != nil {
			return err
		}

		if failed.Phase != "failed" || failed.Endpoints["public"].Ready || !strings.Contains(string(body), "native-helper") {
			return fmt.Errorf("failure is not explicit: %s", body)
		}

		return nil
	})
	compositeAssertLogs(t, h, b.ID, "native-helper", "native-helper synthetic failure", true)
	t.Log("native sidecar failure surfaced in readiness, diagnostics, and previous logs")
	if status, _, err := h.traffic(b.Endpoints["public"].URL, ""); err == nil && status == http.StatusOK {
		t.Fatal("failed composite returned successful baseline traffic")
	}

	if _, err := h.chain(a.Endpoints["public"].URL, a.ID, "v3", ""); err != nil {
		t.Fatalf("sibling preview affected by sidecar failure: %v", err)
	}

	if _, err := h.chain(h.preview, "", "v1", ""); err != nil {
		t.Fatalf("baseline affected by sidecar failure: %v", err)
	}

	// Both healthy and failed composite resources must support ordinary cleanup.
	for _, c := range []composition{a, b, inherited} {
		h.destroy(c.ID)
		h.absent(c.ID, c.Endpoints["public"].URL)
	}

	if _, err := h.chain(h.preview, "", "v1", ""); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", h.kubeconfig, "auth", "can-i", "get", "secrets", "--all-namespaces", "--as=system:serviceaccount:envy-system:envy-server")
	output, _ := command.Output()
	if strings.TrimSpace(string(output)) != "no" {
		t.Fatal("composite support granted cluster-wide Secret read access")
	}
}

func compositeAssertInheritedLogs(t *testing.T, h *harness, id string) {
	t.Helper()
	path := fmt.Sprintf("/v1/compositions/%s/components/service-b/logs?tail_lines=20&max_bytes=8192", id)
	code, body, err := h.request(http.MethodGet, path, nil, "")
	if err != nil || code != http.StatusOK {
		t.Fatalf("inherited composite default application logs: %d %s %v", code, body, err)
	}

	var logs domain.ComponentLogs
	if err := json.Unmarshal(body, &logs); err != nil {
		t.Fatal(err)
	}

	if logs.Source != "shared-baseline" || len(logs.Streams) == 0 {
		t.Fatalf("inherited logs were not labelled as shared-baseline: %s", body)
	}

	found := false
	for _, stream := range logs.Streams {
		if stream.Container != "application" {
			t.Fatalf("inherited composite default logs selected %s instead of application", stream.Container)
		}

		found = found || strings.Contains(stream.Text, "demo listening")
	}

	if !found {
		t.Fatalf("inherited application startup message is missing: %s", body)
	}
}

func compositeAssertLogs(t *testing.T, h *harness, id, container, message string, previous bool) {
	t.Helper()
	path := fmt.Sprintf("/v1/compositions/%s/components/service-b/logs?container=%s&tail_lines=20&max_bytes=8192&previous=%t", id, container, previous)
	code, body, err := h.request(http.MethodGet, path, nil, "")
	if err != nil || code != http.StatusOK {
		t.Fatalf("%s container logs: %d %s %v", container, code, body, err)
	}

	var logs domain.ComponentLogs
	if err := json.Unmarshal(body, &logs); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, stream := range logs.Streams {
		if stream.Container != container {
			t.Fatalf("requested %s logs, received %s", container, stream.Container)
		}

		found = found || strings.Contains(stream.Text, message)
	}

	if !found {
		t.Fatalf("missing %s diagnostic message: %s", container, body)
	}
}

func compositeRegisterCatalog(t *testing.T, h *harness) {
	t.Helper()
	// The seeded demo profile remains immutable and retains its single-container
	// safeguard. A separate project explicitly opts into the scoped policy.
	legacyPath := "/v1/projects/demo/baselines/staging/components/service-b/preview-profile/discover"
	code, body, err := h.request(http.MethodPost, legacyPath, domain.PreviewSelection{}, "")
	var legacy domain.PreviewReport
	if err != nil || code != http.StatusOK || json.Unmarshal(body, &legacy) != nil || len(legacy.Blockers) == 0 {
		t.Fatalf("legacy profile lost composite rejection: %d %s %v", code, body, err)
	}

	manifest := domain.CatalogManifest{APIVersion: "envy/v1", Project: domain.Project{ID: "composite", Name: "Synthetic composite acceptance"}}

	for _, id := range []string{"gateway", "service-a", "service-b"} {
		component := domain.Component{Project: "composite", ID: id, Protocol: "http", Port: 8080, Profile: "http-small", HealthPath: "/healthz", ReadinessPath: "/readyz", Overridable: true}
		if id == "service-b" {
			component.Profile = "deployment-composite"
			component.HealthPath, component.ReadinessPath = "", ""
		}

		manifest.Components = append(manifest.Components, component)
	}

	code, body, err = h.request(http.MethodGet, "/v1/projects/demo/baselines", nil, "")
	var baselines struct {
		Items []domain.Baseline `json:"items"`
	}
	if err != nil || code != http.StatusOK || json.Unmarshal(body, &baselines) != nil || len(baselines.Items) != 1 || baselines.Items[0].ID != "staging" {
		t.Fatalf("read synthetic baseline: %d %s %v", code, body, err)
	}

	baseline := baselines.Items[0]
	baseline.Project, baseline.Revision = "composite", "composite-v1"
	baseline.Endpoint = h.preview
	baseline.Routing.Namespace = "envy-composite-baseline"
	for name, binding := range baseline.Components {
		binding.ServiceHost = strings.Replace(binding.ServiceHost, ".envy-baseline.", ".envy-composite-baseline.", 1)
		baseline.Components[name] = binding
	}

	manifest.Baseline = baseline
	code, body, err = h.request(http.MethodPost, "/v1/catalog/apply", manifest, "")
	if err != nil || code != http.StatusOK {
		t.Fatalf("register composite catalog: %d %s %v", code, body, err)
	}
}

func compositePinImage(t *testing.T, version string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	node := os.Getenv("ENVY_CLUSTER_NAME") + "-control-plane"
	output, err := exec.CommandContext(ctx, "docker", "exec", node, "ctr", "-n", "k8s.io", "images", "ls").Output()
	if err != nil {
		t.Fatal(err)
	}

	tag := "docker.io/envy/service-b:" + version
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[0] == tag && strings.HasPrefix(fields[2], "sha256:") {
			image := "docker.io/envy/service-b@" + fields[2]
			if output, err := exec.CommandContext(ctx, "docker", "exec", node, "ctr", "-n", "k8s.io", "images", "tag", "--force", tag, image).CombinedOutput(); err != nil {
				t.Fatalf("pin composite application: %s (%v)", output, err)
			}

			return image
		}
	}

	t.Fatal("fixture application image is missing")
	return ""
}

func compositeAssertWorkload(t *testing.T, h *harness, id, appImage, helperImage string) corev1.PodSpec {
	t.Helper()
	ns := "envy-" + id
	var deployment appsv1.Deployment
	if err := json.Unmarshal([]byte(h.kubectl("-n", ns, "get", "deployment/service-b", "-o", "json")), &deployment); err != nil {
		t.Fatal(err)
	}

	spec := deployment.Spec.Template.Spec
	if len(spec.Containers) != 2 || spec.Containers[0].Name != "proxy" || spec.Containers[1].Name != "service-b" || spec.Containers[1].Image != appImage {
		t.Fatal("application was selected or overridden by position instead of its approved name")
	}

	if len(spec.InitContainers) != 2 || spec.InitContainers[0].Name != "bootstrap" || spec.InitContainers[0].RestartPolicy != nil || spec.InitContainers[1].Name != "native-helper" || spec.InitContainers[1].RestartPolicy == nil || *spec.InitContainers[1].RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Fatal("ordinary and native init container semantics were not preserved")
	}

	if spec.ServiceAccountName != "composite-workload" || spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Fatal("composite account or token policy was not applied")
	}

	var account corev1.ServiceAccount
	if err := json.Unmarshal([]byte(h.kubectl("-n", ns, "get", "serviceaccount/composite-workload", "-o", "json")), &account); err != nil {
		t.Fatal(err)
	}

	if account.Annotations["fixture.envy.dev/identity"] != "synthetic" || account.Annotations["fixture.envy.dev/source-only"] != "" || account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
		t.Fatal("service-account mapping did not preserve only approved annotations")
	}

	if spec.SecurityContext == nil || spec.SecurityContext.RunAsNonRoot == nil || !*spec.SecurityContext.RunAsNonRoot || spec.SecurityContext.RunAsUser == nil || *spec.SecurityContext.RunAsUser != 65532 || spec.SecurityContext.FSGroup == nil || *spec.SecurityContext.FSGroup != 65532 {
		t.Fatal("non-root pod identity was not preserved")
	}

	var configs corev1.ConfigMapList
	var secrets corev1.SecretList
	if err := json.Unmarshal([]byte(h.kubectl("-n", ns, "get", "configmaps", "-l", "envy.dev/component=service-b", "-o", "json")), &configs); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(h.kubectl("-n", ns, "get", "secrets", "-l", "envy.dev/component=service-b", "-o", "json")), &secrets); err != nil {
		t.Fatal(err)
	}

	if len(configs.Items) != 4 || len(secrets.Items) != 4 {
		t.Fatal("dependencies unique to supporting containers were not captured")
	}

	configNames, secretNames := map[string]bool{}, map[string]bool{}
	for _, config := range configs.Items {
		if config.Immutable == nil || !*config.Immutable || !strings.HasPrefix(config.Name, "envy-dep-") {
			t.Fatal("ConfigMap is not an owned immutable copy")
		}

		configNames[config.Name] = true
	}

	for _, secret := range secrets.Items {
		if secret.Immutable == nil || !*secret.Immutable || !strings.HasPrefix(secret.Name, "envy-dep-") {
			t.Fatal("Secret is not an owned immutable copy")
		}

		secretNames[secret.Name] = true
	}

	for _, container := range append(append([]corev1.Container{}, spec.Containers...), spec.InitContainers...) {
		if container.Name != "service-b" && container.Image != helperImage {
			t.Fatalf("supporting container %s did not retain its pinned image", container.Name)
		}

		sc := container.SecurityContext
		if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation || sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem || sc.Capabilities == nil || !reflect.DeepEqual(sc.Capabilities.Drop, []corev1.Capability{"ALL"}) {
			t.Fatalf("container %s lost its security context", container.Name)
		}

		for _, key := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
			request, limit := container.Resources.Requests[key], container.Resources.Limits[key]
			if request.Sign() <= 0 || limit.Sign() <= 0 || request.Cmp(limit) > 0 {
				t.Fatalf("container %s lost bounded resources", container.Name)
			}
		}

		for _, env := range container.Env {
			if env.ValueFrom == nil {
				continue
			}

			ref := env.ValueFrom
			if ref.ConfigMapKeyRef != nil && !configNames[ref.ConfigMapKeyRef.Name] || ref.SecretKeyRef != nil && !secretNames[ref.SecretKeyRef.Name] {
				t.Fatalf("container %s retained a source dependency reference", container.Name)
			}

			if ref.ResourceFieldRef != nil && ref.ResourceFieldRef.ContainerName == "application" {
				t.Fatal("application resourceFieldRef was not renamed")
			}
		}

		for _, ref := range container.EnvFrom {
			if ref.ConfigMapRef != nil && !configNames[ref.ConfigMapRef.Name] || ref.SecretRef != nil && !secretNames[ref.SecretRef.Name] {
				t.Fatalf("container %s retained a source envFrom reference", container.Name)
			}
		}
	}

	for _, volume := range spec.Volumes {
		if volume.ConfigMap != nil && !configNames[volume.ConfigMap.Name] || volume.Secret != nil && !secretNames[volume.Secret.SecretName] {
			t.Fatal("volume dependency was not rewritten")
		}

		if volume.DownwardAPI != nil {
			for _, item := range volume.DownwardAPI.Items {
				want := "service-b"
				if item.Path == "proxy-memory" {
					want = "proxy"
				}

				if item.ResourceFieldRef == nil || item.ResourceFieldRef.ContainerName != want {
					t.Fatal("downward API renamed the wrong container")
				}
			}
		}
	}

	var quotas corev1.ResourceQuotaList
	if err := json.Unmarshal([]byte(h.kubectl("-n", ns, "get", "resourcequota", "-o", "json")), &quotas); err != nil || len(quotas.Items) != 1 {
		t.Fatal("composite resource quota is missing")
	}

	for key, want := range map[corev1.ResourceName]string{
		corev1.ResourceRequestsCPU: "700m", corev1.ResourceRequestsMemory: "448Mi",
		corev1.ResourceLimitsCPU: "5200m", corev1.ResourceLimitsMemory: "2432Mi",
	} {
		got := quotas.Items[0].Spec.Hard[key]
		if got.Cmp(resource.MustParse(want)) != 0 {
			t.Fatalf("quota %s=%s, want %s including init peak, native sidecar, mesh and rolling surge", key, got.String(), want)
		}
	}

	return spec
}

func compositeDiagnostics(t *testing.T, h *harness, compositions []composition) {
	t.Helper()
	state := os.Getenv("ENVY_STATE_DIR")
	if state == "" {
		return
	}

	for _, c := range compositions {
		_, body, _ := h.request(http.MethodGet, "/v1/compositions/"+c.ID, nil, "")
		_ = os.WriteFile(filepath.Join(state, c.ID+"-composition.json"), body, 0o600)
		for _, kind := range []string{"pods", "deployments", "events"} {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			output, err := exec.CommandContext(ctx, "kubectl", "--kubeconfig", h.kubeconfig, "-n", "envy-"+c.ID, "get", kind, "-o", "json").CombinedOutput()
			cancel()
			_ = os.WriteFile(filepath.Join(state, c.ID+"-"+kind+".json"), output, 0o600)
			if err != nil {
				t.Logf("capture %s diagnostics: %v", kind, err)
			}
		}
	}
}

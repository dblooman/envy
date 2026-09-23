package kubernetes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func compositePreviewFixture(t *testing.T) (*Provider, *fake.Clientset, domain.Baseline, domain.Component) {
	t.Helper()
	p, k, b, c := previewFixture(t)
	ctx := context.Background()
	c.Profile = "deployment-composite"
	policy := domain.CompositePreviewPolicy{Revision: 1, ApplicationContainer: "app", Sidecars: []string{"kafka-proxy"}, InitContainers: []string{"identity-init"}, NativeSidecars: []string{"sql-proxy"}, SourceServiceAccount: "source-workload", ServiceAccount: "preview-workload", ServiceAccountAnnotations: map[string]string{"iam.gke.io/gcp-service-account": "preview@example.iam.gserviceaccount.com"}, SharedDependencies: []string{"shared non-production database"}, MaxPodCPU: "3", MaxPodMemory: "2Gi"}
	p.WithPreviewPolicy(PreviewPolicy{Composite: map[string]domain.CompositePreviewPolicy{"shop/staging/pricing": policy}})
	d, err := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	app := d.Spec.Template.Spec.Containers[0]
	support := func(name string) corev1.Container {
		container := *app.DeepCopy()
		container.Name, container.Image = name, "example/"+name+"@sha256:"+strings.Repeat("b", 64)
		container.Env, container.EnvFrom, container.VolumeMounts, container.Ports = nil, nil, nil, nil
		return container
	}
	proxy := support("kafka-proxy")
	native := support("sql-proxy")
	native.RestartPolicy = new(corev1.ContainerRestartPolicyAlways)
	init := support("identity-init")
	init.ReadinessProbe = nil
	d.Spec.Template.Spec.Containers = []corev1.Container{proxy, app}
	d.Spec.Template.Spec.InitContainers = []corev1.Container{native, init}
	d.Spec.Template.Spec.ServiceAccountName = policy.SourceServiceAccount
	if _, err = k.AppsV1().Deployments("staging").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	return p, k, b, c
}

func TestCompositeDiscoveryCapturesAllReferencesAndNamedApplication(t *testing.T) {
	p, k, b, c := compositePreviewFixture(t)
	ctx := context.Background()
	d, _ := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
	d.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{Name: "proxy-secret"}}}
	d.Spec.Template.Spec.InitContainers[0].Env = []corev1.EnvVar{{Name: "SETTINGS", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{Name: "native-settings", Key: "value"}}}, {Name: "APP_MEMORY", ValueFrom: &corev1.EnvVarSource{ResourceFieldRef: &corev1.ResourceFieldSelector{ContainerName: "app", Resource: "limits.memory"}}}, {Name: "MY_MEMORY", ValueFrom: &corev1.EnvVarSource{ResourceFieldRef: &corev1.ResourceFieldSelector{ContainerName: "sql-proxy", Resource: "limits.memory"}}}}
	d.Spec.Template.Spec.InitContainers[1].EnvFrom = []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{Name: "init-settings"}}}
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{Name: "resources", DownwardAPI: &corev1.DownwardAPIVolumeSource{Items: []corev1.DownwardAPIVolumeFile{{Path: "app", ResourceFieldRef: &corev1.ResourceFieldSelector{ContainerName: "app", Resource: "limits.cpu"}}, {Path: "proxy", ResourceFieldRef: &corev1.ResourceFieldSelector{ContainerName: "sql-proxy", Resource: "limits.cpu"}}}}})
	_, _ = k.AppsV1().Deployments("staging").Update(ctx, d, metav1.UpdateOptions{})
	_, _ = k.CoreV1().Secrets("staging").Create(ctx, &corev1.Secret{Name: "proxy-secret", UID: "proxy-uid", ResourceVersion: "1", Data: map[string][]byte{"value": []byte("secret-not-in-report")}}, metav1.CreateOptions{})
	for _, name := range []string{"native-settings", "init-settings"} {
		_, _ = k.CoreV1().ConfigMaps("staging").Create(ctx, &corev1.ConfigMap{Name: name, UID: "config-uid", ResourceVersion: "1", Data: map[string]string{"value": "public"}}, metav1.CreateOptions{})
	}

	report, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
	if err != nil || len(report.Blockers) != 0 {
		t.Fatalf("discovery: %v %v", err, report.Blockers)
	}

	if len(report.Dependencies) != 5 || len(report.SourceReadRules) != 7 || report.Source.Container != "app" || report.Snapshot.ApplicationContainer != c.ID || report.Snapshot.CompositePolicy == nil || report.Snapshot.CompositePolicyKey != "shop/staging/pricing" {
		t.Fatalf("incomplete composite capture: %+v", report)
	}

	if report.CompositePolicyKey != "shop/staging/pricing" || report.CompositePolicy == nil || report.CompositePolicy.ServiceAccount != "preview-workload" || report.CompositePolicy.ServiceAccountAnnotations["iam.gke.io/gcp-service-account"] != "preview@example.iam.gserviceaccount.com" || len(report.CompositePolicy.SharedDependencies) != 1 {
		t.Fatalf("review output omits the approved destination identity or dependency warning: %+v", report)
	}

	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "secret-not-in-report") || strings.Contains(string(encoded), "never-expose-this-secret") {
		t.Fatal("Secret values escaped discovery")
	}

	template, err := decodePreview(&report.Snapshot)
	if err != nil {
		t.Fatal(err)
	}

	app, err := previewApplication(&template, &report.Snapshot)
	if err != nil || app.Name != c.ID || template.Spec.Containers[0].Name != "kafka-proxy" || template.Spec.InitContainers[0].Name != "sql-proxy" || template.Spec.ServiceAccountName != "preview-workload" {
		t.Fatal("container order or identity changed")
	}

	rewritePreview(&template, c.ID)
	if template.Spec.Containers[0].EnvFrom[0].SecretRef.Name != depName(c.ID, "Secret", "proxy-secret") || template.Spec.InitContainers[0].Env[0].ValueFrom.ConfigMapKeyRef.Name != depName(c.ID, "ConfigMap", "native-settings") || template.Spec.InitContainers[1].EnvFrom[0].ConfigMapRef.Name != depName(c.ID, "ConfigMap", "init-settings") {
		t.Fatal("supporting dependencies were not rewritten")
	}

	if template.Spec.InitContainers[0].Env[1].ValueFrom.ResourceFieldRef.ContainerName != c.ID || template.Spec.InitContainers[0].Env[2].ValueFrom.ResourceFieldRef.ContainerName != "sql-proxy" || template.Spec.Volumes[1].DownwardAPI.Items[0].ResourceFieldRef.ContainerName != c.ID || template.Spec.Volumes[1].DownwardAPI.Items[1].ResourceFieldRef.ContainerName != "sql-proxy" {
		t.Fatal("downward resource references target the wrong container")
	}
}

func TestCompositeDiscoveryBlocksMissingDependencyWithoutMutatingSource(t *testing.T) {
	p, k, baseline, component := compositePreviewFixture(t)
	ctx := context.Background()
	if err := k.CoreV1().Secrets("staging").Delete(ctx, "credentials", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}

	k.ClearActions()
	report, err := p.DiscoverPreview(ctx, baseline, component, domain.PreviewSelection{Deployment: "pricing"})
	if err != nil || !strings.Contains(strings.Join(report.Blockers, ";"), "cannot read Secret credentials") {
		t.Fatalf("broken composite dependency not exposed: %+v %v", report, err)
	}

	if report.CompositePolicy == nil || len(report.CompositePolicy.SharedDependencies) == 0 {
		t.Fatal("shared dependency context was lost")
	}

	for _, action := range k.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("discovery mutated or enumerated the source: %s", action.GetVerb())
		}
	}
}

func TestCompositeDiscoveryRejectsUnapprovedExecution(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*appsv1.Deployment)
		message string
	}{
		{"unexpected regular", func(d *appsv1.Deployment) {
			extra := *d.Spec.Template.Spec.Containers[0].DeepCopy()
			extra.Name = "unknown"
			d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, extra)
		}, "not approved"},
		{"missing sidecar", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers = d.Spec.Template.Spec.Containers[1:] }, "approved container is absent"},
		{"duplicate container", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.InitContainers = append(d.Spec.Template.Spec.InitContainers, d.Spec.Template.Spec.InitContainers[0])
		}, "duplicate"},
		{"mutable supporting image", func(d *appsv1.Deployment) { d.Spec.Template.Spec.InitContainers[0].Image = "example/proxy:latest" }, "immutable sha256"},
		{"native becomes ordinary", func(d *appsv1.Deployment) { d.Spec.Template.Spec.InitContainers[0].RestartPolicy = nil }, "restart policy"},
		{"ordinary becomes native", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.InitContainers[1].RestartPolicy = new(corev1.ContainerRestartPolicyAlways)
		}, "restart policy"},
		{"unsafe sidecar", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].SecurityContext.Privileged = new(true) }, "kafka-proxy: security"},
		{"unconfined pod AppArmor", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}}
		}, "Pod security settings exceed"},
		{"unconfined container AppArmor", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
		}, "kafka-proxy: security"},
		{"unconfined init AppArmor", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.InitContainers[1].SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
		}, "identity-init: security"},
		{"unsafe init", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.InitContainers[1].SecurityContext.AllowPrivilegeEscalation = nil
		}, "identity-init: source must disable"},
		{"missing native resource", func(d *appsv1.Deployment) {
			delete(d.Spec.Template.Spec.InitContainers[0].Resources.Limits, corev1.ResourceCPU)
		}, "sql-proxy: cpu"},
		{"sidecar lifecycle", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{} }, "unsupported container setting: lifecycle"},
		{"missing proxy readiness", func(d *appsv1.Deployment) { d.Spec.Template.Spec.InitContainers[0].ReadinessProbe = nil }, "sql-proxy: source must declare"},
		{"unapproved source identity", func(d *appsv1.Deployment) { d.Spec.Template.Spec.ServiceAccountName = "production" }, "source service account"},
		{"unapproved annotation", func(d *appsv1.Deployment) {
			d.Spec.Template.Annotations = map[string]string{"identity.example.com/name": "production"}
		}, "annotations require"},
		{"host network", func(d *appsv1.Deployment) { d.Spec.Template.Spec.HostNetwork = true }, "unsupported Pod setting: hostNetwork"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, k, b, c := compositePreviewFixture(t)
			ctx := context.Background()
			d, _ := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
			tc.change(d)
			_, _ = k.AppsV1().Deployments("staging").Update(ctx, d, metav1.UpdateOptions{})
			report, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
			if err != nil || !strings.Contains(strings.Join(report.Blockers, ";"), tc.message) {
				t.Fatalf("wanted %q, got err=%v blockers=%v", tc.message, err, report.Blockers)
			}
		})
	}
}

func TestCompositePolicyScopeAndEffectivePodBound(t *testing.T) {
	p, _, b, c := compositePreviewFixture(t)
	ctx := context.Background()
	other := b
	other.ID = "other"
	if _, err := p.DiscoverPreview(ctx, other, c, domain.PreviewSelection{}); err == nil {
		t.Fatal("policy escaped baseline scope")
	}

	if _, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{Container: "kafka-proxy"}); err == nil {
		t.Fatal("caller chose a supporting container as application")
	}

	policy := p.previewPolicy.Composite["shop/staging/pricing"]
	policy.MaxPodCPU = "1"
	p.previewPolicy.Composite["shop/staging/pricing"] = policy
	report, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
	if err != nil || !strings.Contains(strings.Join(report.Blockers, ";"), "effective Pod cpu limit") {
		t.Fatalf("native sidecar was omitted from Pod limits: %v %v", err, report.Blockers)
	}

	c.Profile = "deployment"
	report, err = p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
	if err != nil || !strings.Contains(strings.Join(report.Blockers, ";"), "requires one application") {
		t.Fatal("composite policy silently widened legacy profile")
	}
}

func TestCompositeContractFreezesSupportingExecutionAndPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*appsv1.Deployment)
		changed bool
	}{
		{"application image", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[1].Image = "example/app:new" }, false},
		{"application resources", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[1].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("600m")
		}, false},
		{"support image", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Image = "example/proxy@sha256:" + strings.Repeat("c", 64)
		}, true},
		{"support command", func(d *appsv1.Deployment) { d.Spec.Template.Spec.InitContainers[1].Command = []string{"changed"} }, true},
		{"support resources", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.InitContainers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("600m")
		}, true},
		{"support probe", func(d *appsv1.Deployment) { d.Spec.Template.Spec.InitContainers[0].ReadinessProbe.PeriodSeconds = 15 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, k, b, c := compositePreviewFixture(t)
			ctx := context.Background()
			before, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
			if err != nil || len(before.Blockers) != 0 {
				t.Fatalf("before: %v %v", err, before.Blockers)
			}

			d, _ := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
			tc.change(d)
			_, _ = k.AppsV1().Deployments("staging").Update(ctx, d, metav1.UpdateOptions{})
			after, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
			if err != nil || len(after.Blockers) != 0 || (before.Contract != after.Contract) != tc.changed {
				t.Fatalf("after: %v %v changed=%v", err, after.Blockers, before.Contract != after.Contract)
			}
		})
	}

	p, _, b, c := compositePreviewFixture(t)
	before, _ := p.DiscoverPreview(context.Background(), b, c, domain.PreviewSelection{})
	policy := p.previewPolicy.Composite["shop/staging/pricing"]
	policy.Revision++
	p.previewPolicy.Composite["shop/staging/pricing"] = policy
	after, _ := p.DiscoverPreview(context.Background(), b, c, domain.PreviewSelection{})
	if before.Contract == after.Contract {
		t.Fatal("policy revision omitted from approval contract")
	}
}

func TestCompositeDecodeRejectsInvalidCapturedShape(t *testing.T) {
	p, _, b, c := compositePreviewFixture(t)
	report, err := p.DiscoverPreview(context.Background(), b, c, domain.PreviewSelection{})
	if err != nil {
		t.Fatal(err)
	}

	template, err := decodePreview(&report.Snapshot)
	if err != nil {
		t.Fatal(err)
	}

	template.Spec.InitContainers[0].RestartPolicy = nil
	encoded, _ := json.Marshal(template)
	report.Snapshot.TemplateJSON = string(encoded)
	if _, err := decodePreview(&report.Snapshot); err == nil {
		t.Fatal("persisted role mismatch accepted")
	}
}

func TestCompositeBaselineUsesPolicyApplicationName(t *testing.T) {
	p, k, b, c := compositePreviewFixture(t)
	ctx := context.Background()
	_, _ = k.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{Name: "staging", Labels: map[string]string{"istio-injection": "enabled"}}, metav1.CreateOptions{})
	svc, _ := k.CoreV1().Services("staging").Get(ctx, "pricing", metav1.GetOptions{})
	svc.Spec.Ports[0].Name, svc.Spec.Ports[0].Protocol = "http", corev1.ProtocolTCP
	_, _ = k.CoreV1().Services("staging").Update(ctx, svc, metav1.UpdateOptions{})
	d, _ := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
	pod := &corev1.Pod{Name: "baseline", Labels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec, Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}, ContainerStatuses: []corev1.ContainerStatus{{Name: "istio-proxy", Ready: true}}}}
	_, _ = k.CoreV1().Pods("staging").Create(ctx, pod, metav1.CreateOptions{})
	if err := p.ValidateBaseline(ctx, b, map[string]domain.Component{c.ID: c}); err != nil {
		t.Fatalf("named composite app rejected: %v", err)
	}

	c.Profile = "deployment"
	if err := p.ValidateBaseline(ctx, b, map[string]domain.Component{c.ID: c}); err == nil {
		t.Fatal("legacy fallback accepted arbitrary multi-container app")
	}
}

func TestCompositeNamedServiceTargetCanSelectApprovedProxy(t *testing.T) {
	for _, tc := range []struct {
		name            string
		regular, native bool
		blocked         bool
	}{
		{name: "regular proxy", regular: true},
		{name: "native proxy", native: true},
		{name: "ambiguous proxies", regular: true, native: true, blocked: true},
		{name: "one-shot init cannot serve", blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, k, b, c := compositePreviewFixture(t)
			ctx := context.Background()
			d, _ := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
			d.Spec.Template.Spec.Containers[1].Ports = nil
			port := []corev1.ContainerPort{{Name: "web", ContainerPort: 8181}}
			if tc.regular {
				d.Spec.Template.Spec.Containers[0].Ports = port
			}

			if tc.native {
				d.Spec.Template.Spec.InitContainers[0].Ports = port
			}

			// A one-shot init port is always excluded from Service resolution.
			d.Spec.Template.Spec.InitContainers[1].Ports = port
			_, _ = k.AppsV1().Deployments("staging").Update(ctx, d, metav1.UpdateOptions{})
			report, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
			if err != nil || (len(report.Blockers) > 0) != tc.blocked {
				t.Fatalf("discovery: %v %v", err, report.Blockers)
			}

			if !tc.blocked {
				target, err := previewTarget(domain.WorkloadSpec{Preview: &report.Snapshot})
				if err != nil || target != 8181 {
					t.Fatalf("proxy target: %d %v", target, err)
				}
			}
		})
	}
}

func TestCompositeCaptureDoesNotAliasInstallationPolicy(t *testing.T) {
	p, _, b, c := compositePreviewFixture(t)
	report, err := p.DiscoverPreview(context.Background(), b, c, domain.PreviewSelection{})
	if err != nil || len(report.Blockers) > 0 {
		t.Fatalf("discovery: %v %v", err, report.Blockers)
	}

	captured := report.Snapshot.CompositePolicy
	captured.ServiceAccountAnnotations["iam.gke.io/gcp-service-account"] = "changed"
	captured.Sidecars[0] = "changed"
	captured.InitContainers[0] = "changed"
	captured.NativeSidecars[0] = "changed"
	captured.SharedDependencies[0] = "changed"
	if report.CompositePolicy.ServiceAccountAnnotations["iam.gke.io/gcp-service-account"] == "changed" || report.CompositePolicy.Sidecars[0] == "changed" || report.CompositePolicy.InitContainers[0] == "changed" || report.CompositePolicy.NativeSidecars[0] == "changed" || report.CompositePolicy.SharedDependencies[0] == "changed" {
		t.Fatal("review policy aliases internal execution snapshot")
	}

	report.CompositePolicy.ServiceAccountAnnotations["iam.gke.io/gcp-service-account"] = "review-change"
	report.CompositePolicy.Sidecars[0] = "review-change"
	if captured.ServiceAccountAnnotations["iam.gke.io/gcp-service-account"] == "review-change" || captured.Sidecars[0] == "review-change" {
		t.Fatal("review policy mutations alter internal execution snapshot")
	}

	installed := p.previewPolicy.Composite["shop/staging/pricing"]
	if installed.ServiceAccountAnnotations["iam.gke.io/gcp-service-account"] == "changed" || installed.Sidecars[0] == "changed" || installed.InitContainers[0] == "changed" || installed.NativeSidecars[0] == "changed" || installed.SharedDependencies[0] == "changed" {
		t.Fatal("snapshot policy aliases installation configuration")
	}
}

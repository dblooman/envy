package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func previewFixture(t *testing.T) (*Provider, *fake.Clientset, domain.Baseline, domain.Component) {
	t.Helper()
	app := corev1.Container{Name: "app", Image: "example/app:v1", Ports: []corev1.ContainerPort{{Name: "web", ContainerPort: 8080}}, Env: []corev1.EnvVar{{Name: "CHECKOUT_URL", Value: "http://checkout"}, {Name: "PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Name: "credentials", Key: "password"}}}}, EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{Name: "settings"}}}, VolumeMounts: []corev1.VolumeMount{{Name: "secret", MountPath: "/secret", ReadOnly: true}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")}}, SecurityContext: &corev1.SecurityContext{RunAsNonRoot: new(true), RunAsUser: new(int64(65532)), AllowPrivilegeEscalation: new(false)}, ReadinessProbe: &corev1.Probe{HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromString("web")}}}
	d := &appsv1.Deployment{Name: "pricing", Namespace: "staging", UID: "deployment-uid", ResourceVersion: "1", Generation: 1, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "pricing"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "pricing", "app.kubernetes.io/instance": "argo-baseline"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{app}, Volumes: []corev1.Volume{{Name: "secret", Secret: &corev1.SecretVolumeSource{SecretName: "credentials"}}}}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, ReadyReplicas: 1, UpdatedReplicas: 1, Replicas: 1}}
	svc := &corev1.Service{Name: "pricing", Namespace: "staging", Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "pricing"}, Ports: []corev1.ServicePort{{Port: 8081, TargetPort: intstr.FromString("web")}}}}
	secret := &corev1.Secret{Name: "credentials", Namespace: "staging", UID: "secret-uid", ResourceVersion: "1", Data: map[string][]byte{"password": []byte("never-expose-this-secret")}}
	cm := &corev1.ConfigMap{Name: "settings", Namespace: "staging", UID: "cm-uid", ResourceVersion: "1", Data: map[string]string{"CHECKOUT": "http://checkout"}}
	client := fake.NewClientset(d, svc, secret, cm)
	return New(client, "test", func(context.Context) error { return nil }), client, domain.Baseline{ID: "staging", Project: "shop", Routing: domain.BaselineRouting{Namespace: "staging"}, Components: map[string]domain.BaselineBinding{"pricing": {ServiceHost: "pricing.staging.svc.cluster.local", Port: 8081}}}, domain.Component{ID: "pricing", Project: "shop", Port: 8081, Overridable: true}
}
func TestDerivedDiscoveryAndImmutableDependencies(t *testing.T) {
	p, k, b, c := previewFixture(t)
	ctx := context.Background()
	sel := domain.PreviewSelection{Env: map[string]string{"CHECKOUT_URL": "http://checkout.staging.svc.cluster.local"}, ConfigMapKeys: map[string]map[string]string{"settings": {"CHECKOUT": "http://checkout.staging.svc.cluster.local"}}}
	report, err := p.DiscoverPreview(ctx, b, c, sel)
	if err != nil || len(report.Blockers) > 0 {
		t.Fatalf("discovery %v %v", err, report.Blockers)
	}
	for _, value := range []any{report, report.Snapshot} {
		data, _ := json.Marshal(value)
		if strings.Contains(string(data), "never-expose") {
			t.Fatal("Secret payload escaped provider")
		}
	}
	if len(report.Dependencies) != 2 || report.Selection.Container != "app" {
		t.Fatalf("incomplete report %+v", report)
	}
	s := domain.WorkloadSpec{CompositionID: "preview-a", ComponentID: c.ID, Profile: c, Image: "example/app@sha256:" + strings.Repeat("a", 64), OwnershipToken: "owned", WorkloadCount: 1, Preview: &report.Snapshot, Previews: map[string]domain.PreviewSnapshot{c.ID: report.Snapshot}}
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	deployment, _ := k.AppsV1().Deployments(ref.Namespace).Get(ctx, c.ID, metav1.GetOptions{})
	app := deployment.Spec.Template.Spec.Containers[0]
	if app.Image != s.Image || app.Name != c.ID || app.Env[0].Value != sel.Env["CHECKOUT_URL"] || app.Env[1].ValueFrom.SecretKeyRef.Name == "credentials" || deployment.Spec.Template.Labels["app.kubernetes.io/instance"] != "" {
		t.Fatal("template derivation or rewrite failed")
	}
	source, _ := k.CoreV1().Secrets("staging").Get(ctx, "credentials", metav1.GetOptions{})
	source.ResourceVersion = "2"
	source.Data["password"] = []byte("rotated")
	_, _ = k.CoreV1().Secrets("staging").Update(ctx, source, metav1.UpdateOptions{})
	s.Image = "example/app@sha256:" + strings.Repeat("b", 64)
	restarted := New(k, "test", func(context.Context) error { return nil })
	if _, err = restarted.Ensure(ctx, s); err != nil {
		t.Fatalf("restart or image update followed source: %v", err)
	}
	copied, _ := k.CoreV1().Secrets(ref.Namespace).Get(ctx, depName(c.ID, "Secret", "credentials"), metav1.GetOptions{})
	if string(copied.Data["password"]) != "never-expose-this-secret" || copied.Immutable == nil || !*copied.Immutable {
		t.Fatal("copy changed after rotation")
	}
	_ = k.CoreV1().Secrets(ref.Namespace).Delete(ctx, copied.Name, metav1.DeleteOptions{})
	if _, err = restarted.Ensure(ctx, s); err == nil || !strings.Contains(err.Error(), "recreate") {
		t.Fatal("missing old dependency silently adopted rotation")
	}
	newer, err := p.DiscoverPreview(ctx, b, c, sel)
	if err != nil || newer.Contract != report.Contract || newer.Inspection == report.Inspection {
		t.Fatal("rotation should change inspection but preserve approved contract")
	}
}
func TestDerivedContractAndBlockers(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		change                   func(*appsv1.Deployment)
		blocked, contractChanged bool
	}{
		{"image", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Image = "new" }, false, false},
		{"literal", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Env[0].Value = "new" }, false, false},
		{"dependency", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Env[1].ValueFrom.SecretKeyRef.Name = "other"
		}, true, true},
		{"identity", func(d *appsv1.Deployment) { d.Spec.Template.Spec.ServiceAccountName = "cloud" }, true, true},
		{"init", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "migrate", Image: "db"}}
		}, true, true},
		{"storage", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{Name: "disk", PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "db"}})
		}, true, true},
		{"host", func(d *appsv1.Deployment) { d.Spec.Template.Spec.HostNetwork = true }, true, true},
		{"resources", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = resource.MustParse("10Gi")
		}, true, false},
		{"command", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Command = []string{"new-command"} }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, k, b, c := previewFixture(t)
			ctx := context.Background()
			before, _ := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
			d, _ := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
			tc.change(d)
			_, _ = k.AppsV1().Deployments("staging").Update(ctx, d, metav1.UpdateOptions{})
			after, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{})
			if err != nil {
				t.Fatal(err)
			}
			if (len(after.Blockers) > 0) != tc.blocked || (before.Contract != after.Contract) != tc.contractChanged {
				t.Fatalf("blockers %v contractChanged %t", after.Blockers, before.Contract != after.Contract)
			}
		})
	}
}
func TestDerivedAmbiguityVersionRaceAndOwnership(t *testing.T) {
	p, k, b, c := previewFixture(t)
	ctx := context.Background()
	d, _ := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
	d.Name = "second"
	_, _ = k.AppsV1().Deployments("staging").Create(ctx, d, metav1.CreateOptions{})
	if _, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{}); err == nil {
		t.Fatal("ambiguous Service accepted")
	}
	report, err := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{Deployment: "pricing"})
	if err != nil {
		t.Fatal(err)
	}
	source, _ := k.CoreV1().ConfigMaps("staging").Get(ctx, "settings", metav1.GetOptions{})
	source.ResourceVersion = "2"
	_, _ = k.CoreV1().ConfigMaps("staging").Update(ctx, source, metav1.UpdateOptions{})
	s := domain.WorkloadSpec{CompositionID: "race", ComponentID: c.ID, Profile: c, Image: "image", OwnershipToken: "owner", WorkloadCount: 1, Preview: &report.Snapshot, Previews: map[string]domain.PreviewSnapshot{c.ID: report.Snapshot}}
	if _, err := p.Ensure(ctx, s); err == nil {
		t.Fatal("version race accepted")
	}
	secrets, _ := k.CoreV1().Secrets(Namespace(s.CompositionID)).List(ctx, metav1.ListOptions{})
	if len(secrets.Items) != 0 {
		t.Fatal("partially copied dependencies before validating versions")
	}
	fresh, _ := p.DiscoverPreview(ctx, b, c, domain.PreviewSelection{Deployment: "pricing"})
	s.Preview = &fresh.Snapshot
	s.Previews[c.ID] = fresh.Snapshot
	name := depName(c.ID, "Secret", "credentials")
	_, _ = k.CoreV1().Secrets(Namespace(s.CompositionID)).Create(ctx, &corev1.Secret{Name: name}, metav1.CreateOptions{})
	if _, err := p.Ensure(ctx, s); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatal("unowned dependency adopted")
	}
	p.guard = func(context.Context) error { return fmt.Errorf("lost leadership") }
	if _, err := p.Ensure(ctx, s); err == nil {
		t.Fatal("mutated without leadership")
	}
}
func TestDerivedQuotaIncludesRolloutAndMesh(t *testing.T) {
	p, _, b, c := previewFixture(t)
	report, _ := p.DiscoverPreview(context.Background(), b, c, domain.PreviewSelection{})
	hard, err := previewQuota(domain.WorkloadSpec{WorkloadCount: 1, Previews: map[string]domain.PreviewSnapshot{c.ID: report.Snapshot}})
	if err != nil {
		t.Fatal(err)
	}
	expected := resource.MustParse("400m")
	actual := hard[corev1.ResourceRequestsCPU]
	if actual.Cmp(expected) != 0 {
		t.Fatalf("CPU quota %s", actual.String())
	}
}

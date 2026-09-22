package kubernetes

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func compositeRuntimeFixture(t *testing.T) (*Provider, *fake.Clientset, domain.WorkloadSpec) {
	t.Helper()
	p, client, s := fixture()
	policy := domain.CompositePreviewPolicy{
		Revision: 1, ApplicationContainer: "application", Sidecars: []string{"sql-proxy"},
		InitContainers: []string{"prepare"}, NativeSidecars: []string{"native-proxy"},
		SourceServiceAccount: "baseline-api", ServiceAccount: "preview-api",
		ServiceAccountAnnotations: map[string]string{"iam.gke.io/gcp-service-account": "preview@example.iam.gserviceaccount.com"},
		SharedDependencies:        []string{"preview database"}, MaxPodCPU: "4", MaxPodMemory: "4Gi",
	}
	key := "demo/staging/" + s.ComponentID
	p.WithPreviewPolicy(PreviewPolicy{Composite: map[string]domain.CompositePreviewPolicy{key: policy}})
	container := func(name string) corev1.Container {
		return corev1.Container{
			Name: name, Image: "example/" + name + ":v1",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
				Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
			},
			SecurityContext: &corev1.SecurityContext{RunAsNonRoot: new(true), AllowPrivilegeEscalation: new(false)},
			Env:             []corev1.EnvVar{{Name: "TOPIC", Value: "baseline"}},
		}
	}
	app, sidecar, init, native := container(s.ComponentID), container("sql-proxy"), container("prepare"), container("native-proxy")
	app.Ports = []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}
	app.ReadinessProbe = &corev1.Probe{HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromString("http")}}
	sidecar.ReadinessProbe = &corev1.Probe{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(5432)}}
	native.RestartPolicy = new(corev1.ContainerRestartPolicyAlways)
	native.ReadinessProbe = sidecar.ReadinessProbe.DeepCopy()
	template := corev1.PodTemplateSpec{
		Annotations: map[string]string{"envy.dev/target-port": "8080"},
		Spec: corev1.PodSpec{
			ServiceAccountName: policy.ServiceAccount, AutomountServiceAccountToken: new(false),
			Containers: []corev1.Container{sidecar, app}, InitContainers: []corev1.Container{native, init},
		},
	}
	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := domain.PreviewSnapshot{ApplicationContainer: s.ComponentID, Source: domain.PreviewSource{Container: policy.ApplicationContainer}, CompositePolicyKey: key, CompositePolicy: &policy, TemplateJSON: string(raw)}
	s.Preview, s.Previews, s.WorkloadCount = &snapshot, map[string]domain.PreviewSnapshot{s.ComponentID: snapshot}, 1
	s.MessagingEnv = map[string]string{"TOPIC": "preview-topic"}
	return p, client, s
}

func TestCompositeRuntimeSelectsApplicationByName(t *testing.T) {
	p, client, s := compositeRuntimeFixture(t)
	ctx := context.Background()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	deployment, err := client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	containers := deployment.Spec.Template.Spec.Containers
	if len(containers) != 2 || containers[0].Name != "sql-proxy" || containers[0].Image != "example/sql-proxy:v1" || containers[0].Env[0].Value != "baseline" {
		t.Fatalf("supporting container was changed: %+v", containers)
	}

	if containers[1].Name != s.ComponentID || containers[1].Image != s.Image {
		t.Fatalf("override did not select the application: %+v", containers[1])
	}

	env := map[string]string{}
	for _, entry := range containers[1].Env {
		env[entry.Name] = entry.Value
	}

	if env["TOPIC"] != "preview-topic" || env["ENVY_COMPOSITION_ID"] != s.CompositionID {
		t.Fatalf("application environment was not applied: %v", env)
	}

	if deployment.Spec.Template.Spec.ServiceAccountName != "preview-api" || len(deployment.Spec.Template.Spec.InitContainers) != 2 {
		t.Fatal("composite execution contract was not retained")
	}

	account, err := client.CoreV1().ServiceAccounts(ref.Namespace).Get(ctx, "preview-api", metav1.GetOptions{})
	if err != nil || account.Labels[ComponentLabel] != s.ComponentID || account.Annotations["iam.gke.io/gcp-service-account"] != s.Preview.CompositePolicy.ServiceAccountAnnotations["iam.gke.io/gcp-service-account"] {
		t.Fatalf("approved per-component identity was not created: %+v %v", account, err)
	}

	client.ClearActions()
	if _, err := p.Ensure(ctx, s); err != nil || mutations(client.Actions()) != 0 {
		t.Fatalf("unchanged composite is not idempotent: %v, %d writes", err, mutations(client.Actions()))
	}
}

func TestCompositeRuntimePolicyChangeFailsBeforeWrites(t *testing.T) {
	for _, change := range []string{"revoked", "revision", "annotation", "reserved account", "account reused", "project scope", "component scope", "namespace scope"} {
		t.Run(change, func(t *testing.T) {
			p, client, s := compositeRuntimeFixture(t)
			key := s.Preview.CompositePolicyKey
			installed := p.previewPolicy.Composite[key]
			switch change {
			case "revoked":
				delete(p.previewPolicy.Composite, key)
			case "revision":
				installed.Revision++
				p.previewPolicy.Composite[key] = installed
			case "annotation":
				installed.ServiceAccountAnnotations = maps.Clone(installed.ServiceAccountAnnotations)
				installed.ServiceAccountAnnotations["identity.example.com/role"] = "changed"
				p.previewPolicy.Composite[key] = installed
			case "reserved account":
				installed.ServiceAccount = "envy-workload"
				p.previewPolicy.Composite[key] = installed
				s.Preview.CompositePolicy.ServiceAccount = installed.ServiceAccount
			case "account reused":
				other := *s.Preview
				other.ApplicationContainer = "other"
				other.CompositePolicyKey = "demo/staging/other"
				template, err := decodePreview(s.Preview)
				if err != nil {
					t.Fatal(err)
				}

				template.Spec.Containers[1].Name = "other"
				raw, _ := json.Marshal(template)
				other.TemplateJSON = string(raw)
				s.Previews["other"] = other
				p.previewPolicy.Composite[other.CompositePolicyKey] = installed
			case "project scope":
				s.ProjectID = "another-project"
			case "component scope":
				s.Preview.CompositePolicyKey = "demo/staging/another-component"
				p.previewPolicy.Composite[s.Preview.CompositePolicyKey] = installed
				s.Previews[s.ComponentID] = *s.Preview
			case "namespace scope":
				s.BaselineNamespace = "another-namespace"
			}

			if _, err := p.Ensure(context.Background(), s); err == nil {
				t.Fatal("unsafe or revoked execution policy accepted")
			} else if change == "account reused" && !strings.Contains(err.Error(), "reused") {
				t.Fatalf("expected account collision, got %v", err)
			} else if strings.HasSuffix(change, "scope") && !strings.Contains(err.Error(), "scope") {
				t.Fatalf("expected scope mismatch, got %v", err)
			}

			if n := mutations(client.Actions()); n != 0 {
				t.Fatalf("policy failure made %d writes", n)
			}
		})
	}
}

func TestCompositeRuntimePolicySurvivesSnapshotRoundTrip(t *testing.T) {
	p, _, s := compositeRuntimeFixture(t)
	policy := *s.Preview.CompositePolicy
	policy.ServiceAccountAnnotations = map[string]string{}
	p.previewPolicy.Composite[s.Preview.CompositePolicyKey] = policy
	s.Preview.CompositePolicy = &policy
	raw, err := json.Marshal(s.Preview)
	if err != nil {
		t.Fatal(err)
	}

	var captured domain.PreviewSnapshot
	if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatal(err)
	}

	s.Preview, s.Previews[s.ComponentID] = &captured, captured
	if _, err := p.Ensure(context.Background(), s); err != nil {
		t.Fatalf("unchanged policy failed after persistence: %v", err)
	}
}

func TestCompositeRuntimeIgnoresRevokedRemovedSnapshotUntilReadded(t *testing.T) {
	p, client, s := compositeRuntimeFixture(t)
	removed := *s.Preview
	removed.ApplicationContainer, removed.CompositePolicyKey = "removed", "demo/staging/removed"
	template, err := decodePreview(s.Preview)
	if err != nil {
		t.Fatal(err)
	}

	template.Spec.Containers[1].Name = "removed"
	raw, _ := json.Marshal(template)
	removed.TemplateJSON = string(raw)
	s.Previews["removed"] = removed
	s.DesiredComponents = []string{s.ComponentID}
	// The removed component's policy is absent, but its quota snapshot remains
	// while its previous Pods may still be draining.
	if _, err := p.Ensure(context.Background(), s); err != nil {
		t.Fatalf("removed policy prevented remaining workload reconciliation: %v", err)
	}

	if _, ok := s.Previews["removed"]; !ok {
		t.Fatal("retained snapshot was removed")
	}

	s.DesiredComponents = append(s.DesiredComponents, "removed")
	client.ClearActions()
	if _, err := p.Ensure(context.Background(), s); err == nil || !strings.Contains(err.Error(), "revoked") || mutations(client.Actions()) != 0 {
		t.Fatalf("readded component bypassed policy validation: %v", err)
	}
}

func TestCompositeExecutionDriftBlocksReconcileAndReadiness(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*corev1.PodTemplateSpec)
	}{
		{"extra regular container", func(t *corev1.PodTemplateSpec) {
			t.Spec.Containers = append(t.Spec.Containers, corev1.Container{Name: "unapproved", Image: "example/unapproved:v1"})
		}},
		{"extra init container", func(t *corev1.PodTemplateSpec) {
			t.Spec.InitContainers = append(t.Spec.InitContainers, corev1.Container{Name: "unapproved", Image: "example/unapproved:v1"})
		}},
		{"init ordering", func(t *corev1.PodTemplateSpec) {
			t.Spec.InitContainers[0], t.Spec.InitContainers[1] = t.Spec.InitContainers[1], t.Spec.InitContainers[0]
		}},
		{"extra supporting env", func(t *corev1.PodTemplateSpec) {
			t.Spec.Containers[0].Env = append(t.Spec.Containers[0].Env, corev1.EnvVar{Name: "EXTRA", Value: "unapproved"})
		}},
		{"changed supporting command", func(t *corev1.PodTemplateSpec) { t.Spec.InitContainers[0].Command = []string{"unapproved"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, client, s := compositeRuntimeFixture(t)
			ctx := context.Background()
			ref, err := p.Ensure(ctx, s)
			if err != nil {
				t.Fatal(err)
			}

			d, _ := client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
			tc.change(&d.Spec.Template)
			if _, err := client.AppsV1().Deployments(ref.Namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}

			client.ClearActions()
			if _, err := p.Ensure(ctx, s); err == nil || !strings.Contains(err.Error(), "execution drift") || mutations(client.Actions()) != 0 {
				t.Fatalf("drift was silently accepted or rewritten: %v", err)
			}

			obs, err := p.Observe(ctx, ref)
			if err != nil || !obs.Failed || obs.Ready || !strings.Contains(obs.Message, "execution drift") {
				t.Fatalf("drift was not visible in observation: %+v %v", obs, err)
			}
		})
	}
}

func TestCompositeExecutionAllowsApplicationUpdateAndDefaultedAccountAlias(t *testing.T) {
	p, client, s := compositeRuntimeFixture(t)
	ctx := context.Background()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	d, _ := client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	d.Spec.Template.Spec.DeprecatedServiceAccount = d.Spec.Template.Spec.ServiceAccountName
	if _, err := client.AppsV1().Deployments(ref.Namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Ensure(ctx, s); err != nil {
		t.Fatalf("API-defaulted alias was treated as execution drift: %v", err)
	}

	s.Image = "example/app:updated"
	updated, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	if updated.ExecutionFingerprint == "" || updated.ExecutionFingerprint == ref.ExecutionFingerprint {
		t.Fatal("application update did not capture its new expected execution fingerprint")
	}

	d, _ = client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	if d.Spec.Template.Spec.Containers[1].Image != s.Image || d.Spec.Template.Spec.Containers[0].Image != "example/sql-proxy:v1" {
		t.Fatal("application update changed the wrong image")
	}
}

func TestCompositeServiceAccountReconcilesExactMetadata(t *testing.T) {
	p, client, s := compositeRuntimeFixture(t)
	ctx := context.Background()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	api := client.CoreV1().ServiceAccounts(ref.Namespace)
	account, _ := api.Get(ctx, "preview-api", metav1.GetOptions{})
	uid := account.UID
	account.Annotations["iam.gke.io/gcp-service-account"] = "unapproved@example.iam.gserviceaccount.com"
	account.Annotations["identity.example.com/extra-role"] = "unapproved"
	account.Labels["identity.example.com/extra-label"] = "unapproved"
	account.AutomountServiceAccountToken = new(true)
	if _, err := api.Update(ctx, account, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}

	account, _ = api.Get(ctx, "preview-api", metav1.GetOptions{})
	if account.UID != uid || account.Annotations["identity.example.com/extra-role"] != "" || account.Labels["identity.example.com/extra-label"] != "" || *account.AutomountServiceAccountToken || account.Annotations["iam.gke.io/gcp-service-account"] != s.Preview.CompositePolicy.ServiceAccountAnnotations["iam.gke.io/gcp-service-account"] {
		t.Fatalf("identity metadata drift was retained: %+v", account)
	}

	account.Annotations[OwnershipAnnotation] = "foreign-owner"
	if _, err := api.Update(ctx, account, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	client.ClearActions()
	if _, err := p.Ensure(ctx, s); err == nil || !strings.Contains(err.Error(), "ownership") || mutations(client.Actions()) != 0 {
		t.Fatalf("foreign account was adopted: %v, %d writes", err, mutations(client.Actions()))
	}
}

func TestCompositeQuotaAccountsForOrderedInitAndNativeSidecars(t *testing.T) {
	container := func(name, cpu, memory string, native bool) corev1.Container {
		r := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(memory)}
		c := corev1.Container{Name: name, Resources: corev1.ResourceRequirements{Requests: r.DeepCopy(), Limits: r.DeepCopy()}}
		if native {
			c.RestartPolicy = new(corev1.ContainerRestartPolicyAlways)
		}

		return c
	}
	spec := corev1.PodSpec{
		Containers: []corev1.Container{container("app", "100m", "64Mi", false), container("sql", "100m", "64Mi", false), container("istio-proxy", "100", "100Gi", false)},
		InitContainers: []corev1.Container{
			container("early", "1200m", "800Mi", false), container("native-one", "100m", "100Mi", true),
			container("late", "1000m", "250Mi", false), container("native-two", "300m", "200Mi", true),
			container("linkerd-init", "100", "100Gi", false),
		},
	}
	before, _ := json.Marshal(spec)
	r := previewPodResources(spec)
	if r.Requests.Cpu().Cmp(resource.MustParse("1200m")) != 0 || r.Limits.Memory().Cmp(resource.MustParse("800Mi")) != 0 {
		t.Fatalf("incorrect effective resources: %+v", r)
	}

	after, _ := json.Marshal(spec)
	if string(before) != string(after) {
		t.Fatal("quota calculation changed captured resources")
	}

	// Remove injected mesh containers from the persisted template, as discovery
	// does. Quota must then add the captured mesh once per rollout replica.
	spec.Containers = spec.Containers[:2]
	spec.InitContainers = spec.InitContainers[:4]
	policy := domain.CompositePreviewPolicy{ApplicationContainer: "app", Sidecars: []string{"sql"}, InitContainers: []string{"early", "late"}, NativeSidecars: []string{"native-one", "native-two"}, ServiceAccount: "preview-api"}
	spec.ServiceAccountName, spec.AutomountServiceAccountToken = policy.ServiceAccount, new(false)
	raw, _ := json.Marshal(corev1.PodTemplateSpec{Spec: spec})
	snapshot := domain.PreviewSnapshot{ApplicationContainer: "app", Source: domain.PreviewSource{Container: "app"}, CompositePolicyKey: "demo/staging/app", CompositePolicy: &policy, TemplateJSON: string(raw)}
	hard, err := previewQuota(domain.WorkloadSpec{WorkloadCount: 1, Previews: map[string]domain.PreviewSnapshot{"app": snapshot}})
	if err != nil {
		t.Fatal(err)
	}

	for key, want := range map[corev1.ResourceName]string{corev1.ResourceRequestsCPU: "2600m", corev1.ResourceRequestsMemory: "1856Mi", corev1.ResourceLimitsCPU: "6400m", corev1.ResourceLimitsMemory: "3648Mi"} {
		if got := hard[key]; got.Cmp(resource.MustParse(want)) != 0 {
			t.Errorf("%s = %s, want %s", key, got.String(), want)
		}
	}
}

func TestCompositeObservationIncludesSupportingContainers(t *testing.T) {
	p, client, s := compositeRuntimeFixture(t)
	ctx := context.Background()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	d, _ := client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: d.Generation, ReadyReplicas: 1, UpdatedReplicas: 1, Replicas: 1}
	if _, err := client.AppsV1().Deployments(ref.Namespace).UpdateStatus(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	ready := corev1.PodStatus{
		ContainerStatuses:     []corev1.ContainerStatus{{Name: s.ComponentID, Ready: true}, {Name: "sql-proxy", Ready: true}, {Name: "istio-proxy", Ready: true}},
		InitContainerStatuses: []corev1.ContainerStatus{{Name: "native-proxy", Ready: true}, {Name: "prepare", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}},
	}
	pod, err := client.CoreV1().Pods(ref.Namespace).Create(ctx, &corev1.Pod{Name: "preview-pod", Labels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec, Status: ready}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.DiscoveryV1().EndpointSlices(ref.Namespace).Create(ctx, &discoveryv1.EndpointSlice{Name: "preview-endpoint", Labels: map[string]string{"kubernetes.io/service-name": ref.Service}, Endpoints: []discoveryv1.Endpoint{{Conditions: discoveryv1.EndpointConditions{Ready: new(true)}, TargetRef: &corev1.ObjectReference{UID: pod.UID}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		change func(*corev1.PodStatus)
		failed bool
	}{
		{"sql-proxy", func(s *corev1.PodStatus) { s.ContainerStatuses[1].Ready = false }, false},
		{"native-proxy", func(s *corev1.PodStatus) { s.InitContainerStatuses[0].Ready = false }, false},
		{"sql-proxy", func(s *corev1.PodStatus) {
			s.ContainerStatuses[1].State.Waiting = &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}
		}, true},
		{"native-proxy", func(s *corev1.PodStatus) {
			s.InitContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}
		}, true},
		{"prepare", func(s *corev1.PodStatus) { s.InitContainerStatuses[1].State.Terminated.ExitCode = 1 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod.Status = *ready.DeepCopy()
			tc.change(&pod.Status)
			if _, err := client.CoreV1().Pods(ref.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}

			obs, err := p.Observe(ctx, ref)
			if err != nil || obs.Ready || obs.Failed != tc.failed || obs.Image != s.Image || !strings.Contains(obs.Message, tc.name) {
				t.Fatalf("supporting process failure/readiness hidden: %+v %v", obs, err)
			}
		})
	}

	pod.Status = ready
	if _, err := client.CoreV1().Pods(ref.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	obs, err := p.Observe(ctx, ref)
	if err != nil || !obs.Ready || obs.Failed || obs.Image != s.Image || obs.WorkloadID != string(pod.UID) {
		t.Fatalf("healthy composite did not become ready: %+v %v", obs, err)
	}
}

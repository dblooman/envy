package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

func fixture() (*Provider, *fake.Clientset, domain.WorkloadSpec) {
	client := fake.NewClientset()
	next := 0
	client.PrependReactor("create", "*", func(action kt.Action) (bool, runtime.Object, error) {
		obj := action.(kt.CreateAction).GetObject()
		m, _ := meta.Accessor(obj)
		next++
		m.SetUID(types.UID(fmt.Sprintf("uid-%d", next)))
		return false, nil, nil
	})
	return New(client, "test", func(context.Context) error { return nil }), client, domain.WorkloadSpec{Profile: domain.Component{Profile: "http-small", Port: 8080, HealthPath: "/healthz", ReadinessPath: "/readyz"}, CompositionID: "abc123", ComponentID: "service-b", ProjectID: "demo", Image: "envy/service-b:v2", OwnershipToken: "claim-token"}
}
func mutations(actions []kt.Action) int {
	n := 0
	for _, a := range actions {
		switch a.GetVerb() {
		case "create", "update", "patch", "delete":
			n++
		}
	}
	return n
}
func TestEnsureIdempotentAndSafeProfile(t *testing.T) {
	ctx := context.Background()
	p, c, s := fixture()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if ref.NamespaceUID == "" || ref.DeploymentUID == "" || ref.ServiceUID == "" {
		t.Fatalf("missing inventory: %#v", ref)
	}
	d, _ := c.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	pod := d.Spec.Template.Spec
	if pod.ServiceAccountName != "envy-workload" || pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken || !(*pod.SecurityContext.RunAsNonRoot) {
		t.Fatalf("unsafe pod profile: %#v", pod)
	}
	if d.Spec.Template.Labels[CompositionLabel] != s.CompositionID || pod.Containers[0].Image != s.Image {
		t.Fatalf("wrong workload: %#v", d)
	}
	c.ClearActions()
	if _, err = p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}
	if n := mutations(c.Actions()); n != 0 {
		t.Fatalf("unchanged Ensure produced %d mutations", n)
	}
	d.Spec.Template.Spec.Containers[0].Image = "changed"
	_, _ = c.AppsV1().Deployments(ref.Namespace).Update(ctx, d, metav1.UpdateOptions{})
	if _, err = p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}
	d, _ = c.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	if d.Spec.Template.Spec.Containers[0].Image != s.Image {
		t.Fatal("image drift not repaired")
	}
}

func TestEnsureUsesConfiguredInjectionLabels(t *testing.T) {
	client := fake.NewClientset()
	p := NewWithInjection(client, "test", func(context.Context) error { return nil }, map[string]string{"istio.io/rev": "production"})
	_, err := p.Ensure(context.Background(), domain.WorkloadSpec{Profile: domain.Component{Profile: "http-small", Port: 8080, HealthPath: "/healthz", ReadinessPath: "/readyz"}, CompositionID: "custom", ComponentID: "service-b", ProjectID: "demo", Image: "envy/service-b:v2", OwnershipToken: "claim-token"})
	if err != nil {
		t.Fatal(err)
	}
	ns, err := client.CoreV1().Namespaces().Get(context.Background(), Namespace("custom"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ns.Labels["istio.io/rev"] != "production" || ns.Labels["istio-injection"] != "" {
		t.Fatalf("namespace labels=%v", ns.Labels)
	}
}

func TestEnsureWithPodAnnotationsAndNoInjection(t *testing.T) {
	client := fake.NewClientset()
	// empty map means no sidecar injection labels (e.g. Cilium or Linkerd pod annotation mode)
	p := NewWithInjection(client, "test", func(context.Context) error { return nil }, map[string]string{}).
		WithPodAnnotations(map[string]string{"linkerd.io/inject": "enabled"})
	_, err := p.Ensure(context.Background(), domain.WorkloadSpec{
		Profile:        domain.Component{Profile: "http-small", Port: 8080, HealthPath: "/healthz", ReadinessPath: "/readyz"},
		CompositionID:  "linkerd-test",
		ComponentID:    "service-b",
		ProjectID:      "demo",
		Image:          "envy/service-b:v2",
		OwnershipToken: "claim-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	ns, err := client.CoreV1().Namespaces().Get(context.Background(), Namespace("linkerd-test"), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ns.Labels["istio-injection"] != "" {
		t.Fatalf("expected no istio-injection label, got %v", ns.Labels)
	}

	deploy, err := client.AppsV1().Deployments(Namespace("linkerd-test")).Get(context.Background(), "service-b", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deploy.Spec.Template.Annotations["linkerd.io/inject"] != "enabled" {
		t.Fatalf("missing linkerd annotation on pod template: %v", deploy.Spec.Template.Annotations)
	}
}
func TestOwnershipAndLeadershipGuard(t *testing.T) {
	ctx := context.Background()
	p, c, s := fixture()
	_, err := c.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{Name: Namespace(s.CompositionID)}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c.ClearActions()
	if _, err = p.Ensure(ctx, s); err == nil {
		t.Fatal("adopted unrelated namespace")
	}
	if mutations(c.Actions()) != 0 {
		t.Fatal("mutated conflicting namespace")
	}
	p, c, s = fixture()
	checks := 0
	p.guard = func(context.Context) error {
		checks++
		if checks >= 3 {
			return errors.New("leader lost")
		}
		return nil
	}
	ref, err := p.Ensure(ctx, s)
	if err == nil || ref.NamespaceUID == "" {
		t.Fatalf("expected partial inventory on lost leadership: %#v %v", ref, err)
	}
	if n := mutations(c.Actions()); n != 2 {
		t.Fatalf("writes after lost guard: %d", n)
	}
}
func TestDeleteRejectsChangedIdentityAndRecoversMissingUID(t *testing.T) {
	ctx := context.Background()
	p, c, s := fixture()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	wrong := ref
	wrong.NamespaceUID = "replaced"
	if err = p.Delete(ctx, wrong); err == nil {
		t.Fatal("deleted namespace with changed identity")
	}
	wrong = ref
	wrong.OwnershipToken = "wrong"
	if err = p.Delete(ctx, wrong); err == nil {
		t.Fatal("deleted namespace with wrong claim")
	}
	// A crash can occur after namespace creation but before its UID is persisted.
	ref.NamespaceUID = ""
	if err = p.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	absent, err := p.Absent(ctx, ref)
	if err != nil || !absent {
		t.Fatalf("cleanup not observed: %v %v", absent, err)
	}
	if err = p.Delete(ctx, ref); err != nil {
		t.Fatal("repeat delete:", err)
	}
	_ = c
}
func TestObserveRequiresReadyEndpointsAndReportsPullFailure(t *testing.T) {
	ctx := context.Background()
	p, c, s := fixture()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := c.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: d.Generation, ReadyReplicas: 1, UpdatedReplicas: 1, Replicas: 1}
	_, _ = c.AppsV1().Deployments(ref.Namespace).UpdateStatus(ctx, d, metav1.UpdateOptions{})
	obs, err := p.Observe(ctx, ref)
	if err != nil || obs.Ready {
		t.Fatalf("ready without endpoints: %#v %v", obs, err)
	}
	pod, err := c.CoreV1().Pods(ref.Namespace).Create(ctx, &corev1.Pod{Name: "override-pod", Labels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "service-b", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obs, err = p.Observe(ctx, ref)
	if err != nil || !obs.Failed || obs.Ready {
		t.Fatalf("pull failure hidden: %#v %v", obs, err)
	}
	_, err = c.DiscoveryV1().EndpointSlices(ref.Namespace).Create(ctx, &discoveryv1.EndpointSlice{Name: "ready", Labels: map[string]string{"kubernetes.io/service-name": ref.Service}, Endpoints: []discoveryv1.Endpoint{{Conditions: discoveryv1.EndpointConditions{Ready: new(true)}, TargetRef: &corev1.ObjectReference{UID: pod.UID}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obs, err = p.Observe(ctx, ref)
	if err != nil || obs.Ready {
		t.Fatalf("endpoint without proxy incorrectly ready: %+v %v", obs, err)
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "service-b", Ready: true}, {Name: "istio-proxy", Ready: true}}
	_, _ = c.CoreV1().Pods(ref.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	obs, err = p.Observe(ctx, ref)
	if err != nil || !obs.Ready || obs.WorkloadID != string(pod.UID) {
		t.Fatalf("ready endpoint not observed: %#v %v", obs, err)
	}
}

func TestUpdateNeverVerifiesPreviousPod(t *testing.T) {
	ctx := context.Background()
	p, c, s := fixture()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := c.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	old, _ := c.CoreV1().Pods(ref.Namespace).Create(ctx, &corev1.Pod{Name: "old", Labels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec}, metav1.CreateOptions{})
	_, err = c.DiscoveryV1().EndpointSlices(ref.Namespace).Create(ctx, &discoveryv1.EndpointSlice{Name: "ready", Labels: map[string]string{"kubernetes.io/service-name": ref.Service}, Endpoints: []discoveryv1.Endpoint{{Conditions: discoveryv1.EndpointConditions{Ready: new(true)}, TargetRef: &corev1.ObjectReference{UID: old.UID}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c.ClearActions()
	s.Image = "envy/service-b:v3"
	updated, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if updated != ref {
		t.Fatal("update changed resource identities")
	}
	for _, a := range c.Actions() {
		if a.GetVerb() == "update" && a.GetResource().Resource != "deployments" {
			t.Fatalf("unexpected update: %v", a)
		}
		if a.GetVerb() == "create" {
			t.Fatal("update duplicated resources")
		}
	}
	d, _ = c.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: d.Generation, ReadyReplicas: 1, UpdatedReplicas: 1, Replicas: 1}
	_, _ = c.AppsV1().Deployments(ref.Namespace).UpdateStatus(ctx, d, metav1.UpdateOptions{})
	obs, err := p.Observe(ctx, ref)
	if err != nil || obs.Ready {
		t.Fatalf("old pod falsely verified v3: %+v %v", obs, err)
	}
	fresh, _ := c.CoreV1().Pods(ref.Namespace).Create(ctx, &corev1.Pod{Name: "new", Labels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "istio-proxy", Ready: true}}}}, metav1.CreateOptions{})
	slice, _ := c.DiscoveryV1().EndpointSlices(ref.Namespace).Get(ctx, "ready", metav1.GetOptions{})
	slice.Endpoints[0].TargetRef.UID = fresh.UID
	_, _ = c.DiscoveryV1().EndpointSlices(ref.Namespace).Update(ctx, slice, metav1.UpdateOptions{})
	d.Status.Replicas = 2
	_, _ = c.AppsV1().Deployments(ref.Namespace).UpdateStatus(ctx, d, metav1.UpdateOptions{})
	obs, err = p.Observe(ctx, ref)
	if err != nil || obs.Ready {
		t.Fatalf("incomplete rollout verified: %+v %v", obs, err)
	}
	d.Status.Replicas = 1
	_, _ = c.AppsV1().Deployments(ref.Namespace).UpdateStatus(ctx, d, metav1.UpdateOptions{})
	obs, err = p.Observe(ctx, ref)
	if err != nil || !obs.Ready || obs.WorkloadID != string(fresh.UID) {
		t.Fatalf("new pod not verified: %+v %v", obs, err)
	}
}

func TestRegisteredProfileIsUsedWithoutCopyingPodPrivileges(t *testing.T) {
	p, client, s := fixture()
	s.ComponentID = "worker"
	s.Profile.Port = 9090
	s.Profile.ReadinessPath = "/ready"
	s.Profile.HealthPath = "/live"
	s.Profile.Env = map[string]string{"DOWNSTREAM_URL": "http://database.orders.svc.cluster.local:9000", "LISTEN_ADDR": ":9090"}
	ref, err := p.Ensure(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := client.AppsV1().Deployments(ref.Namespace).Get(context.Background(), "worker", metav1.GetOptions{})
	c := d.Spec.Template.Spec.Containers[0]
	if c.Name != "worker" || c.Ports[0].ContainerPort != 9090 || c.ReadinessProbe.HTTPGet.Path != "/ready" || c.LivenessProbe.HTTPGet.Path != "/live" {
		t.Fatal("approved port or probes lost")
	}
	if len(c.Env) != 4 || c.Env[2].Name != "DOWNSTREAM_URL" || c.Env[3].Value != ":9090" {
		t.Fatal("approved environment lost or nondeterministic")
	}
	svc, _ := client.CoreV1().Services(ref.Namespace).Get(context.Background(), "worker", metav1.GetOptions{})
	if svc.Spec.Ports[0].Port != 9090 {
		t.Fatal("wrong Service port")
	}
	if !*c.SecurityContext.ReadOnlyRootFilesystem || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("profile changed privilege boundary")
	}
}

func TestMultipleWorkloadsShareQuotaButKeepDisjointSelectors(t *testing.T) {
	p, client, a := fixture()
	a.ComponentID = "service-a"
	a.WorkloadCount = 3
	b := a
	b.ComponentID = "service-b"
	ctx := context.Background()
	first, err := p.Ensure(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Ensure(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if first.NamespaceUID != second.NamespaceUID || first.DeploymentUID == second.DeploymentUID || first.ServiceUID == second.ServiceUID {
		t.Fatal("workload identities are not separate within a shared namespace")
	}
	sa, _ := client.CoreV1().Services(first.Namespace).Get(ctx, "service-a", metav1.GetOptions{})
	sb, _ := client.CoreV1().Services(first.Namespace).Get(ctx, "service-b", metav1.GetOptions{})
	if sa.Spec.Selector[ComponentLabel] == sb.Spec.Selector[ComponentLabel] {
		t.Fatal("Service selectors overlap")
	}
	q, _ := client.CoreV1().ResourceQuotas(first.Namespace).Get(ctx, "envy-quota", metav1.GetOptions{})
	pods := q.Spec.Hard[corev1.ResourcePods]
	cpu := q.Spec.Hard[corev1.ResourceLimitsCPU]
	memory := q.Spec.Hard[corev1.ResourceLimitsMemory]
	if pods.Value() < 6 || cpu.MilliValue() < 9000 || memory.Value() < 6*1024*1024*1024 {
		t.Fatal("quota cannot accommodate simultaneous rolling updates")
	}
	client.ClearActions()
	if _, err = p.Ensure(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Ensure(ctx, b); err != nil {
		t.Fatal(err)
	}
	if mutations(client.Actions()) != 0 {
		t.Fatal("unchanged workloads churn shared resources")
	}
}

func TestDigestPinnedOverrideRetainsExactArtifact(t *testing.T) {
	ctx := context.Background()
	p, c, s := fixture()
	s.Image = "registry.example.com/team/service-b@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := c.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Template.Spec.Containers[0].Image != s.Image {
		t.Fatal("deployment did not retain immutable image reference")
	}
	s.Image = "registry.example.com/team/service-b@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err = p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}
	deployment, err = c.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Template.Spec.Containers[0].Image != s.Image {
		t.Fatal("update did not use selected digest")
	}
}

func TestWorkloadPullSecretsAreReferencesAndRecheckedBeforeMutation(t *testing.T) {
	p, client, spec := fixture()
	spec.Profile.ImagePullSecrets = []string{"registry"}
	if _, err := p.Ensure(context.Background(), spec); err == nil {
		t.Fatal("unapproved reference accepted")
	}
	if mutations(client.Actions()) != 0 {
		t.Fatal("denied reference caused provider mutation")
	}
	p.WithApprovedPullSecrets([]string{"registry"})
	ref, err := p.Ensure(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	dep, err := client.AppsV1().Deployments(ref.Namespace).Get(context.Background(), ref.Deployment, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(dep.Spec.Template.Spec.ImagePullSecrets) != 1 || dep.Spec.Template.Spec.ImagePullSecrets[0].Name != "registry" {
		t.Fatal("pull reference missing")
	}
	for _, a := range client.Actions() {
		if a.GetResource().Resource == "secrets" {
			t.Fatal("provider accessed credential values")
		}
	}
	client.ClearActions()
	p.WithApprovedPullSecrets(nil)
	if _, err := p.Ensure(context.Background(), spec); err == nil {
		t.Fatal("revoked reference accepted")
	}
	if mutations(client.Actions()) != 0 {
		t.Fatal("revoked reference caused provider mutation")
	}
}

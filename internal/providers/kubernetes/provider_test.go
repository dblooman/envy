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
	return New(client, "test", func(context.Context) error { return nil }), client, domain.WorkloadSpec{CompositionID: "abc123", ComponentID: "service-b", ProjectID: "demo", Image: "envy/service-b:v2", OwnershipToken: "claim-token"}
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
func TestOwnershipAndLeadershipGuard(t *testing.T) {
	ctx := context.Background()
	p, c, s := fixture()
	_, err := c.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: Namespace(s.CompositionID)}}, metav1.CreateOptions{})
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
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: d.Generation, ReadyReplicas: 1, UpdatedReplicas: 1}
	_, _ = c.AppsV1().Deployments(ref.Namespace).UpdateStatus(ctx, d, metav1.UpdateOptions{})
	obs, err := p.Observe(ctx, ref)
	if err != nil || obs.Ready {
		t.Fatalf("ready without endpoints: %#v %v", obs, err)
	}
	pod, err := c.CoreV1().Pods(ref.Namespace).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "override-pod", Labels: d.Spec.Template.Labels}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "service-b", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obs, err = p.Observe(ctx, ref)
	if err != nil || !obs.Failed || obs.Ready {
		t.Fatalf("pull failure hidden: %#v %v", obs, err)
	}
	_, err = c.DiscoveryV1().EndpointSlices(ref.Namespace).Create(ctx, &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "ready", Labels: map[string]string{"kubernetes.io/service-name": ref.Service}}, Endpoints: []discoveryv1.Endpoint{{Conditions: discoveryv1.EndpointConditions{Ready: ptr(true)}, TargetRef: &corev1.ObjectReference{UID: pod.UID}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obs, err = p.Observe(ctx, ref)
	if err != nil || !obs.Ready || obs.WorkloadID != string(pod.UID) {
		t.Fatalf("ready endpoint not observed: %#v %v", obs, err)
	}
}

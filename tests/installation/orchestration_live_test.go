package installation

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/providers/kubeapply"
	kp "github.com/dblooman/envy/internal/providers/kubernetes"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type applyWriteCounter struct {
	next  http.RoundTripper
	count *atomic.Int64
}

func (c applyWriteCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/deployments/") {
		c.count.Add(1)
	}

	return c.next.RoundTrip(r)
}

func TestOrchestrationApplyResources(t *testing.T) {
	if os.Getenv("ENVY_TEST_ORCHESTRATION") != "1" {
		t.Skip("explicit disposable-cluster test")
	}

	name := os.Getenv("ENVY_KUBE_CONTEXT")
	if !strings.HasPrefix(name, "kind-envy-test-") {
		t.Fatal("requires explicit disposable kind-envy-test-* context")
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{CurrentContext: name}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}

	var writes atomic.Int64
	cfg.WrapTransport = func(next http.RoundTripper) http.RoundTripper { return applyWriteCounter{next, &writes} }
	cfg.Timeout = 10 * time.Second
	k, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	guard := func(context.Context) error { return nil }
	p := kp.NewWithInjection(k, "apply-test", guard, map[string]string{})
	s := domain.WorkloadSpec{CompositionID: fmt.Sprintf("apply-%d", time.Now().UnixNano()), ComponentID: "app", OwnershipToken: "test-owner", Image: "example/app:v1", Profile: domain.Component{Profile: "http-small", Port: 8080, ReadinessPath: "/ready", HealthPath: "/health", Env: map[string]string{"REMOVE_ME": "one"}}}
	ns := kp.Namespace(s.CompositionID)
	t.Cleanup(func() { _ = k.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{}) })
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	api := k.AppsV1().Deployments(ns)
	// Recreate the Deployment exactly as the previous server created it, with its
	// historical field-manager name and without the new apply fingerprint.
	d, err := api.Get(ctx, "app", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err = api.Delete(ctx, "app", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		_, e := api.Get(ctx, "app", metav1.GetOptions{})
		if e != nil {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("deployment deletion did not finish")
		}

		time.Sleep(100 * time.Millisecond)
	}

	d.ObjectMeta = metav1.ObjectMeta{Name: "app", Namespace: ns, Labels: d.Labels, Annotations: map[string]string{kp.OwnershipAnnotation: s.OwnershipToken}}
	d.Status = appsv1.DeploymentStatus{}
	if _, err = api.Create(ctx, d, metav1.CreateOptions{FieldManager: "envy-server"}); err != nil {
		t.Fatal(err)
	}

	// Simulate an independent admission/controller adding a field Envy never owns.
	addition := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"app"},"spec":{"template":{"metadata":{"annotations":{"example.test/injected":"preserve"}}}}}`)
	if _, err = api.Patch(ctx, "app", types.ApplyPatchType, addition, metav1.PatchOptions{FieldManager: "other-controller"}); err != nil {
		t.Fatal(err)
	}

	s.Image = "example/app:v2"
	s.Profile.Env = nil
	updated, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	if updated.NamespaceUID != ref.NamespaceUID {
		t.Fatal("namespace identity changed")
	}

	d, err = api.Get(ctx, "app", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if d.Spec.Template.Annotations["example.test/injected"] != "preserve" {
		t.Fatal("unowned annotation lost")
	}

	for _, e := range d.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "REMOVE_ME" {
			t.Fatal("removed owned field retained")
		}
	}

	before := writes.Load()
	if _, err = p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}

	d, _ = api.Get(ctx, "app", metav1.GetOptions{})
	if writes.Load() != before {
		t.Fatal("no-op ensure wrote deployment")
	}

	d.Spec.Template.Spec.Containers[0].Image = "example/app:foreign"
	if _, err = api.Update(ctx, d, metav1.UpdateOptions{FieldManager: "foreign"}); err != nil {
		t.Fatal(err)
	}

	if _, err = p.Ensure(ctx, s); err == nil {
		t.Fatal("foreign conflict forced")
	}

	// A stale resource version is rejected alongside ownership conflicts.
	desired := d.DeepCopy()
	desired.Spec.Template.Spec.Containers[0].Image = "example/app:next"
	if _, err = kubeapply.Apply(ctx, api, desired, d, "apps/v1", "Deployment", kubeapply.RuntimeManager, guard); err == nil {
		t.Fatal("stale observation accepted")
	}

	checkConcurrentReplacement(ctx, t, k, ns, guard)
}

// TestOrchestrationNetworkBoundary requires an enforcing CNI. A positive control
// proves the dependency is reachable before the deny checks are meaningful.
func TestOrchestrationNetworkBoundary(t *testing.T) {
	if os.Getenv("ENVY_TEST_NETWORK_POLICY") != "1" {
		t.Skip("explicit enforcing-CNI test")
	}

	name := os.Getenv("ENVY_KUBE_CONTEXT")
	if !strings.HasPrefix(name, "kind-envy-test-") {
		t.Fatal("requires explicit disposable cluster")
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{CurrentContext: name}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}

	k, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	guard := func(context.Context) error { return nil }
	p := kp.NewWithInjection(k, "network-test", guard, map[string]string{}).WithMesh("cilium")
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	makeWorkload := func(id, component string) domain.WorkloadSpec {
		return domain.WorkloadSpec{CompositionID: "net-" + id + "-" + nonce, ComponentID: component, OwnershipToken: "net-owner", Image: "envy/service-b:v1", Profile: domain.Component{Profile: "http-small", Port: 8080, ReadinessPath: "/readyz", HealthPath: "/healthz"}}
	}
	base := makeWorkload("base", "dependency")
	outside := makeWorkload("outside", "outside")
	ensure := func(s domain.WorkloadSpec) domain.WorkloadRef {
		t.Cleanup(func() {
			_ = k.CoreV1().Namespaces().Delete(context.WithoutCancel(ctx), kp.Namespace(s.CompositionID), metav1.DeleteOptions{})
		})
		ref, e := p.Ensure(ctx, s)
		if e != nil {
			t.Fatal(e)
		}

		for {
			obs, e := p.Observe(ctx, ref)
			if e != nil {
				t.Fatal(e)
			}

			if obs.Ready {
				break
			}

			select {
			case <-ctx.Done():
				t.Fatal("workload not ready", obs.Message)
			case <-time.After(200 * time.Millisecond):
			}
		}

		return ref
	}
	baseRef := ensure(base)
	outsideRef := ensure(outside)
	policy := kp.NamespacePolicy{Mode: "isolated", Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": baseRef.Namespace}}}}}}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: new(corev1.ProtocolUDP), Port: new(intstr.FromInt32(53))}, {Protocol: new(corev1.ProtocolTCP), Port: new(intstr.FromInt32(53))}}}}}
	p.WithNamespacePolicy(policy)
	first := makeWorkload("first", "app")
	first.BaselineNamespace = baseRef.Namespace
	second := makeWorkload("second", "app")
	second.BaselineNamespace = baseRef.Namespace
	firstRef := ensure(first)
	secondRef := ensure(second)
	url := func(ref domain.WorkloadRef) string {
		return "http://" + ref.Service + "." + ref.Namespace + ".svc.cluster.local:8080/healthz"
	}
	probe := func(ns, script string) {
		pod := &corev1.Pod{GenerateName: "boundary-", Namespace: ns, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: new(false), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: new(true), RunAsUser: new(int64(65532)), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{{Name: "probe", Image: "curlimages/curl:8.14.1@sha256:9a1ed35addb45476afa911696297f8e115993df459278ed036182dd2cd22b67b", Command: []string{"sh", "-ec", script}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")}}, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: new(false), ReadOnlyRootFilesystem: new(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}}}}}
		pod, e := k.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
		if e != nil {
			t.Fatal(e)
		}

		for {
			got, e := k.CoreV1().Pods(ns).Get(ctx, pod.Name, metav1.GetOptions{})
			if e != nil {
				t.Fatal(e)
			}

			if got.Status.Phase == corev1.PodSucceeded {
				return
			}

			if got.Status.Phase == corev1.PodFailed {
				logs, _ := k.CoreV1().Pods(ns).GetLogs(pod.Name, &corev1.PodLogOptions{}).DoRaw(ctx)
				t.Fatalf("boundary probe failed: %s", logs)
			}

			select {
			case <-ctx.Done():
				t.Fatal("probe timeout")
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	curl := "curl --silent --show-error --fail --connect-timeout 3 --max-time 5 -o /dev/null "
	probe(baseRef.Namespace, curl+"https://example.com; "+curl+url(firstRef)+"; "+curl+url(secondRef)+"; "+curl+url(outsideRef))
	probe(firstRef.Namespace, curl+url(firstRef)+"; "+curl+url(baseRef)+"; if "+curl+"http://dependency:8080/healthz; then exit 11; fi; if "+curl+url(secondRef)+"; then exit 12; fi; if "+curl+url(outsideRef)+"; then exit 13; fi; if "+curl+"https://example.com; then exit 14; fi")
}

type warningRecorder struct{ warnings []string }

func (w *warningRecorder) HandleWarningHeader(_ int, _ string, text string) {
	w.warnings = append(w.warnings, text)
}

func TestOrchestrationPodSecurity(t *testing.T) {
	if os.Getenv("ENVY_TEST_ORCHESTRATION") != "1" {
		t.Skip("explicit disposable-cluster test")
	}

	name := os.Getenv("ENVY_KUBE_CONTEXT")
	if !strings.HasPrefix(name, "kind-envy-test-") {
		t.Fatal("requires explicit disposable cluster")
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{CurrentContext: name}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}

	warnings := &warningRecorder{}
	cfg.WarningHandler = warnings
	k, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	policy := kp.NamespacePolicy{}.Defaults()
	ns, err := k.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{GenerateName: "envy-psa-", Labels: policy.Labels()}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = k.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{}) })
	pod := &corev1.Pod{Name: "unsafe", Namespace: ns.Name, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "envy/service-b:v1", SecurityContext: &corev1.SecurityContext{Privileged: new(true)}}}}}
	if _, err = k.CoreV1().Pods(ns.Name).Create(ctx, pod, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}); err != nil {
		t.Fatal("warning-only policy rejected workload", err)
	}

	if len(warnings.warnings) == 0 {
		t.Fatal("restricted warning policy emitted no warning")
	}

	policy.PodSecurity.Enforce = "restricted"
	ns.Labels = policy.Labels()
	if _, err = k.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err = k.CoreV1().Pods(ns.Name).Create(ctx, pod, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}); err == nil || !strings.Contains(err.Error(), "violates PodSecurity") {
		t.Fatal("restricted enforcement did not reject unsafe workload", err)
	}
}

func checkConcurrentReplacement(ctx context.Context, t *testing.T, k kubernetes.Interface, ns string, guard func(context.Context) error) {
	t.Helper()
	api := k.AppsV1().Deployments(ns)
	old, err := api.Get(ctx, "app", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err = api.Delete(ctx, old.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: new(old.UID)}}); err != nil {
		t.Fatal(err)
	}

	replacement := old.DeepCopy()
	replacement.ObjectMeta = metav1.ObjectMeta{Name: old.Name, Namespace: ns, Labels: old.Labels, Annotations: old.Annotations}
	replacement.Status = appsv1.DeploymentStatus{}
	deadline := time.Now().Add(10 * time.Second)
	for {
		replacement, err = api.Create(ctx, replacement, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
		if err == nil {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal(err)
		}

		replacement = old.DeepCopy()
		replacement.ObjectMeta = metav1.ObjectMeta{Name: old.Name, Namespace: ns, Labels: old.Labels, Annotations: old.Annotations}
		replacement.Status = appsv1.DeploymentStatus{}
		time.Sleep(100 * time.Millisecond)
	}

	want := replacement.DeepCopy()
	want.Spec.Template.Spec.Containers[0].Image = "example/app:must-not-apply"
	if _, err = kubeapply.Apply(ctx, api, want, old, "apps/v1", "Deployment", kubeapply.RuntimeManager, guard); err == nil {
		t.Fatal("concurrent replacement accepted stale apply")
	}

	got, err := api.Get(ctx, old.Name, metav1.GetOptions{})
	if err != nil || got.UID != replacement.UID || got.Spec.Template.Spec.Containers[0].Image != replacement.Spec.Template.Spec.Containers[0].Image {
		t.Fatal("concurrent replacement was changed", err)
	}
}

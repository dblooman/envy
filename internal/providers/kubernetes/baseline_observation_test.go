package kubernetes

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBaselineFingerprintIgnoresReplicaAndStatusChurn(t *testing.T) {
	p, client, baseline, _ := previewFixture(t)
	ctx := context.Background()
	_, err := client.CoreV1().Pods("staging").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pricing-one", Namespace: "staging", Labels: map[string]string{"app": "pricing"}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", ImageID: "containerd://sha256:one"}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	first, err := p.ObserveBaseline(ctx, baseline, nil)
	if err != nil || first.Fingerprint == "" || first.Components["pricing"].ImageIdentity != "known" || len(first.Components["pricing"].ActualImageIDs["app"]) != 1 {
		t.Fatalf("no observed baseline execution: %+v %v", first, err)
	}

	deployment, err := client.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	deployment.Generation++
	deployment.Spec.Replicas = new(int32(3))
	deployment.Status.ReadyReplicas = 3
	if _, err := client.AppsV1().Deployments("staging").Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	unchanged, err := p.ObserveBaseline(ctx, baseline, nil)
	if err != nil || unchanged.Fingerprint != first.Fingerprint || unchanged.Components["pricing"].DeploymentGeneration == first.Components["pricing"].DeploymentGeneration {
		t.Fatalf("replica/status churn invalidated execution: %+v %v", unchanged, err)
	}

	deployment.Spec.Template.Spec.Containers[0].Image = "example/app:v2"
	if _, err := client.AppsV1().Deployments("staging").Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	changed, err := p.ObserveBaseline(ctx, baseline, nil)
	if err != nil || changed.Fingerprint == first.Fingerprint || changed.Components["pricing"].DeclaredImages["app"] != "example/app:v2" {
		t.Fatalf("image drift was missed: %+v %v", changed, err)
	}

	for _, action := range client.Actions() {
		if action.GetResource().Resource == "secrets" || action.GetResource().Resource == "configmaps" {
			t.Fatal("baseline fingerprint inspected configuration contents")
		}
	}
}

func TestBaselineFingerprintTracksServiceRoutingAndUnknownIdentity(t *testing.T) {
	p, client, baseline, _ := previewFixture(t)
	// An informer may retain stale objects after a watch disconnect; baseline
	// freshness must still use live reads rather than cached listers.
	p.WithObservations(&Observations{})
	ctx := context.Background()
	first, err := p.ObserveBaseline(ctx, baseline, nil)
	if err != nil || first.Components["pricing"].ImageIdentity != "unknown" {
		t.Fatalf("unobserved image identity was invented: %+v %v", first, err)
	}

	service, err := client.CoreV1().Services("staging").Get(ctx, "pricing", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	service.Spec.Ports[0].TargetPort.IntVal = 9090
	service.Spec.Ports[0].TargetPort.StrVal = ""
	if _, err := client.CoreV1().Services("staging").Update(ctx, service, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	changed, err := p.ObserveBaseline(ctx, baseline, nil)
	if err != nil || changed.Fingerprint == first.Fingerprint {
		t.Fatalf("Service destination drift was missed: %+v %v", changed, err)
	}

	// Selected execution is excluded; the registered Service remains in scope.
	overridden, err := p.ObserveBaseline(ctx, baseline, map[string]domain.ComponentOverride{"pricing": {Image: "example/preview:v2"}})
	if err != nil || overridden.Components["pricing"].ExecutionState != "overridden" || overridden.Components["pricing"].ServiceUID != changed.Components["pricing"].ServiceUID {
		t.Fatalf("override scope lost Service routing: %+v %v", overridden, err)
	}
}

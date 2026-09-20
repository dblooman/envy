package kubernetes

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNamespaceIsolationPrecedesWorkloads(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientset()
	policy := NamespacePolicy{Mode: "isolated", Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "envy-system"}}}}}}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}}}}}}
	p := NewWithInjection(client, "test", func(context.Context) error { return nil }, map[string]string{}).WithNamespacePolicy(policy)
	s := domain.WorkloadSpec{CompositionID: "preview", ComponentID: "api", OwnershipToken: "owned", BaselineNamespace: "staging", Image: "example/api:v1", Profile: domain.Component{Profile: "http-small", Port: 8080, ReadinessPath: "/ready", HealthPath: "/health"}}
	if _, err := p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}

	seen := false
	for _, a := range client.Actions() {
		if a.GetVerb() == "create" && a.GetResource().Resource == "networkpolicies" {
			seen = true
		}

		if a.GetVerb() == "create" && a.GetResource().Resource == "deployments" && !seen {
			t.Fatal("workload created before isolation")
		}
	}

	np, err := client.NetworkingV1().NetworkPolicies("envy-preview").Get(ctx, "envy-isolation", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(np.Spec.PolicyTypes) != 2 || np.Spec.Egress[0].To[1].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "staging" {
		t.Fatal("incorrect boundary")
	}

	client.ClearActions()
	if _, err = p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}

	if mutations(client.Actions()) != 0 {
		t.Fatal("unchanged policy wrote resources")
	}

	p.WithNamespacePolicy(NamespacePolicy{Mode: "isolated"})
	client.ClearActions()
	if _, err = p.Ensure(ctx, s); err == nil {
		t.Fatal("missing infrastructure accepted")
	}

	if mutations(client.Actions()) != 0 {
		t.Fatal("incomplete policy mutated cluster")
	}
}

func TestNamespaceLabelsPreserveExternalOwnership(t *testing.T) {
	p, client, s := fixture()
	ctx := context.Background()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	ns, err := client.CoreV1().Namespaces().Get(ctx, ref.Namespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	ns.Labels["example.test/owner"] = "platform"
	if _, err = client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err = p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}

	ns, err = client.CoreV1().Namespaces().Get(ctx, ref.Namespace, metav1.GetOptions{})
	if err != nil || ns.Labels["example.test/owner"] != "platform" {
		t.Fatal("external namespace label lost", err)
	}

	ns.Labels["pod-security.kubernetes.io/warn"] = "privileged"
	if _, err = client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	client.ClearActions()
	if _, err = p.Ensure(ctx, s); err == nil {
		t.Fatal("conflicting PSA label accepted")
	}

	if mutations(client.Actions()) != 0 {
		t.Fatal("mutated namespace with conflicting PSA labels")
	}
}

func TestInternalResourceNamesRemainValidComponents(t *testing.T) {
	for _, name := range []string{"envy-quota", "envy-workload", "envy-isolation"} {
		t.Run(name, func(t *testing.T) {
			p, client, s := fixture()
			s.ComponentID = name
			ctx := context.Background()
			ref, err := p.Ensure(ctx, s)
			if err != nil {
				t.Fatal(err)
			}

			d, err := client.AppsV1().Deployments(ref.Namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil || d.Spec.Template.Labels[ComponentLabel] != name || d.Spec.Selector.MatchLabels[ComponentLabel] != name {
				t.Fatal("component lost its workload identity", err)
			}
		})
	}
}

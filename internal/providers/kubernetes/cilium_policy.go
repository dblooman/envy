package kubernetes

import (
	"context"
	"fmt"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/providers/kubeapply"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var ciliumPolicies = schema.GroupVersionResource{Group: "cilium.io", Version: "v2", Resource: "ciliumnetworkpolicies"}

func (p *Provider) WithPolicyClient(client dynamic.Interface) *Provider {
	p.policyClient = client
	return p
}

// Cilium Gateway API uses reserved:ingress, which a Kubernetes namespace
// selector cannot express. This narrow additive policy permits that identity.
func (p *Provider) ensureCiliumIngress(ctx context.Context, s domain.WorkloadSpec, ns string) error {
	if p.namespacePolicy.Mode != "isolated" || !p.namespacePolicy.CiliumIngress {
		return nil
	}

	if p.mesh != "cilium" || p.policyClient == nil {
		return fmt.Errorf("cilium_ingress requires the Cilium mesh and policy API")
	}

	want := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "cilium.io/v2", "kind": "CiliumNetworkPolicy", "spec": map[string]any{"endpointSelector": map[string]any{}, "ingress": []any{map[string]any{"fromEntities": []any{"ingress"}}}}}}
	metadata := p.sharedMetadata(s, "envy-cilium-ingress", ns)
	want.SetName(metadata.Name)
	want.SetNamespace(ns)
	want.SetLabels(metadata.Labels)
	want.SetAnnotations(metadata.Annotations)
	api := p.policyClient.Resource(ciliumPolicies).Namespace(ns)
	got, err := api.Get(ctx, want.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return err
		}

		kubeapply.Stamp(want)
		_, err = api.Create(ctx, want, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
		return err
	}

	if err != nil {
		return err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return err
	}

	if !kubeapply.Changed(want, got) {
		return nil
	}

	_, err = kubeapply.Apply(ctx, api, want, got, "cilium.io/v2", "CiliumNetworkPolicy", kubeapply.RuntimeManager, p.writable)
	return err
}

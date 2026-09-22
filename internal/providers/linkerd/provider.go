// Package linkerd combines Linkerd producer routes with Envoy Gateway ingress.
package linkerd

import (
	"context"
	"fmt"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/mesh"
	"github.com/dblooman/envy/internal/providers/gatewayapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	kube "k8s.io/client-go/kubernetes"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

var serviceProfiles = schema.GroupVersionResource{Group: "linkerd.io", Version: "v1alpha2", Resource: "serviceprofiles"}

type Provider struct {
	*gatewayapi.Provider
	dynamic dynamic.Interface
}

func New(client gatewayclient.Interface, k kube.Interface, dyn dynamic.Interface, installation string, guard func(context.Context) error, class string) *Provider {
	profile, _ := mesh.Resolve("linkerd")
	return &Provider{Provider: gatewayapi.NewProfile(client, installation, guard, class, profile).WithKubernetes(k), dynamic: dyn}
}

func (p *Provider) conflicts(ctx context.Context, hosts map[string]bool) error {
	list, err := p.dynamic.Resource(serviceProfiles).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("inspect Linkerd ServiceProfiles: %w", err)
	}

	for _, sp := range list.Items {
		if hosts[sp.GetName()] {
			return domain.Validation("Linkerd ServiceProfile " + sp.GetNamespace() + "/" + sp.GetName() + " takes precedence over Envy HTTPRoutes; remove it before onboarding")
		}
	}

	return nil
}

func (p *Provider) ValidateBaseline(ctx context.Context, b domain.Baseline, c map[string]domain.Component) error {
	hosts := map[string]bool{}
	for _, binding := range b.Components {
		hosts[binding.ServiceHost] = true
	}

	if err := p.conflicts(ctx, hosts); err != nil {
		return err
	}

	return p.Provider.ValidateBaseline(ctx, b, c)
}

func (p *Provider) Reconcile(ctx context.Context, s domain.RouteSnapshot) (domain.RouteObservation, error) {
	hosts := map[string]bool{}
	for _, d := range s.Domains {
		hosts[d.ServiceHost] = true
	}

	for _, e := range s.MeshEntries {
		hosts[e.Domain.ServiceHost] = true
	}

	if err := p.conflicts(ctx, hosts); err != nil {
		return domain.RouteObservation{}, err
	}

	return p.Provider.Reconcile(ctx, s)
}

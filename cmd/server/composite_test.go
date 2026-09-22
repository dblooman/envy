package main

import (
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/mesh"
	kubeprovider "github.com/dblooman/envy/internal/providers/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestCatalogProvidersReceiveCompositePolicy(t *testing.T) {
	for _, name := range []string{"istio", "cilium", "linkerd"} {
		t.Run(name, func(t *testing.T) {
			profile, err := mesh.Resolve(name)
			if err != nil {
				t.Fatal(err)
			}

			config := serverFileConfig{Preview: kubeprovider.PreviewPolicy{Composite: map[string]domain.CompositePreviewPolicy{
				"shop/staging/pricing": {Revision: 1, ApplicationContainer: "application", SourceServiceAccount: "default", ServiceAccount: "preview-pricing", SharedDependencies: []string{"No external dependencies"}, MaxPodCPU: "2", MaxPodMemory: "2Gi"},
			}}}
			labels := map[string]string{}
			if name == "istio" {
				labels["istio-injection"] = "enabled"
			}

			client := fake.NewClientset(&corev1.Namespace{Name: "baseline", Labels: labels})
			providers, err := newProviders(&rest.Config{Host: "https://unused.invalid"}, client, config, profile, "installation")
			if err != nil {
				t.Fatal(err)
			}

			err = providers.kubeValidator.ValidateBaseline(t.Context(), domain.Baseline{ID: "staging", Project: "shop", Routing: domain.BaselineRouting{Namespace: "baseline"}, Components: map[string]domain.BaselineBinding{"pricing": {ServiceHost: "pricing.baseline.svc.cluster.local"}}}, map[string]domain.Component{"pricing": {ID: "pricing", Profile: "deployment-composite"}})
			// Reaching Service lookup proves that the scoped policy was present and valid.
			if err == nil || !strings.Contains(err.Error(), "Service pricing") {
				t.Fatalf("catalog policy was not propagated: %v", err)
			}
		})
	}
}

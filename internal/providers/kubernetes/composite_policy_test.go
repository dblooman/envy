package kubernetes

import (
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestCompositeInstallationPolicyValidation(t *testing.T) {
	valid := func() domain.CompositePreviewPolicy {
		return domain.CompositePreviewPolicy{Revision: 1, ApplicationContainer: "app", Sidecars: []string{"proxy"}, InitContainers: []string{"bootstrap"}, NativeSidecars: []string{"database"}, SourceServiceAccount: "baseline-app", ServiceAccount: "preview-app", MaxPodCPU: "8", MaxPodMemory: "8Gi", SharedDependencies: []string{"Shared test database"}, ServiceAccountAnnotations: map[string]string{"identity.example.com/account": "test-only"}}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*domain.CompositePreviewPolicy)
	}{
		{"revision", func(p *domain.CompositePreviewPolicy) { p.Revision = 0 }},
		{"duplicate role", func(p *domain.CompositePreviewPolicy) { p.NativeSidecars = []string{"proxy"} }},
		{"mesh container", func(p *domain.CompositePreviewPolicy) { p.Sidecars = []string{"istio-proxy"} }},
		{"source identity", func(p *domain.CompositePreviewPolicy) { p.SourceServiceAccount = "" }},
		{"default identity", func(p *domain.CompositePreviewPolicy) { p.ServiceAccount = "default" }},
		{"shared identity", func(p *domain.CompositePreviewPolicy) { p.ServiceAccount = "envy-workload" }},
		{"CPU bound", func(p *domain.CompositePreviewPolicy) { p.MaxPodCPU = "0" }},
		{"memory bound", func(p *domain.CompositePreviewPolicy) { p.MaxPodMemory = "unlimited" }},
		{"dependencies", func(p *domain.CompositePreviewPolicy) { p.SharedDependencies = nil }},
		{"metadata", func(p *domain.CompositePreviewPolicy) {
			p.ServiceAccountAnnotations["envy.dev/ownership-token"] = "foreign"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			if err := (PreviewPolicy{Composite: map[string]domain.CompositePreviewPolicy{"shop/staging/pricing": p}}).Validate(); err == nil {
				t.Fatal("unsafe policy accepted")
			}
		})
	}

	for _, key := range []string{"pricing", "shop//pricing", "shop/staging/pricing/other"} {
		if err := (PreviewPolicy{Composite: map[string]domain.CompositePreviewPolicy{key: valid()}}).Validate(); err == nil {
			t.Fatalf("invalid scope %q accepted", key)
		}
	}

	if err := (PreviewPolicy{Composite: map[string]domain.CompositePreviewPolicy{"shop/staging/pricing": valid()}}).Validate(); err != nil {
		t.Fatal(err)
	}

	if err := (PreviewPolicy{}).Validate(); err != nil {
		t.Fatalf("legacy defaults failed: %v", err)
	}
}

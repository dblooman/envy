// Package cilium uses Cilium's Gateway API and GAMMA controllers, without sidecars.
package cilium

import (
	"context"
	"fmt"
	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/mesh"
	"github.com/dblooman/envy/internal/providers/gatewayapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	kube "k8s.io/client-go/kubernetes"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	"strings"
)

type Provider struct {
	*gatewayapi.Provider
	kube      kube.Interface
	endpoints dynamic.Interface
}

func (p *Provider) WithEndpointClient(client dynamic.Interface) *Provider {
	p.endpoints = client
	return p
}

func New(client gatewayclient.Interface, k kube.Interface, installation string, guard func(context.Context) error, class string) *Provider {
	profile, _ := mesh.Resolve("cilium")
	return &Provider{Provider: gatewayapi.NewProfile(client, installation, guard, class, profile).WithKubernetes(k), kube: k}
}
func (p *Provider) ValidateBaseline(ctx context.Context, b domain.Baseline, c map[string]domain.Component) error {
	if err := p.CheckPrerequisites(ctx); err != nil {
		return err
	}

	if p.endpoints == nil {
		return fmt.Errorf("Cilium endpoint client is required to validate baseline participation")
	}

	for _, binding := range b.Components {
		parts := strings.Split(binding.ServiceHost, ".")
		namespace := b.Routing.Namespace
		if len(parts) > 1 {
			namespace = parts[1]
		}

		svc, err := p.kube.CoreV1().Services(namespace).Get(ctx, parts[0], metav1.GetOptions{})
		if err != nil {
			return err
		}

		if len(svc.Spec.Selector) == 0 {
			return domain.Validation("Cilium baseline Service requires a workload selector")
		}

		pods, err := p.kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String()})
		if err != nil {
			return err
		}

		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}

			ep, err := p.endpoints.Resource(schema.GroupVersionResource{Group: "cilium.io", Version: "v2", Resource: "ciliumendpoints"}).Namespace(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if err != nil {
				return domain.Validation("baseline pod " + namespace + "/" + pod.Name + " requires a Cilium-managed endpoint: " + err.Error())
			}

			state, _, _ := unstructured.NestedString(ep.Object, "status", "state")
			if state != "ready" {
				return domain.Validation("Cilium endpoint " + namespace + "/" + pod.Name + " is not ready")
			}
		}
	}

	return p.Provider.ValidateBaseline(ctx, b, c)
}
func (p *Provider) CheckPrerequisites(ctx context.Context) error {
	cfg, err := p.kube.CoreV1().ConfigMaps("kube-system").Get(ctx, "cilium-config", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("inspect Cilium configuration: %w", err)
	}

	if cfg.Data["enable-gateway-api-hostnetwork"] == "true" {
		return domain.Validation("Cilium host-network Gateway exposure is unsupported for multi-service GAMMA in the pinned profile; use Service/LoadBalancer exposure")
	}

	for _, key := range []string{"kube-proxy-replacement", "enable-l7-proxy", "enable-gateway-api"} {
		if cfg.Data[key] != "true" {
			return domain.Validation("Cilium requires " + key + "=true")
		}
	}

	ds, err := p.kube.AppsV1().DaemonSets("kube-system").Get(ctx, "cilium", metav1.GetOptions{})
	if err != nil {
		return err
	}

	if ds.Status.DesiredNumberScheduled == 0 || ds.Status.NumberReady != ds.Status.DesiredNumberScheduled {
		return domain.Validation("Cilium agents are not ready on all nodes")
	}

	return nil
}

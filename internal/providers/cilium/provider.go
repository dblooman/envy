// Package cilium reconciles routing snapshots into Cilium Service Mesh resources,
// supporting both Gateway API mode and native CiliumEnvoyConfig (CEC) mode.
package cilium

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/providers/gatewayapi"
	"github.com/dblooman/envy/internal/routing"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

const installationLabel = "envy.dev/installation"
const compositionLabel = "envy.dev/composition"
const ownershipAnnotation = "envy.dev/ownership-token"
const roleLabel = "envy.dev/route-role"

var ciliumEnvoyConfigGVR = schema.GroupVersionResource{
	Group:    "cilium.io",
	Version:  "v2",
	Resource: "ciliumenvoyconfigs",
}

type Config struct {
	Installation string
	NativeCEC    bool
	GatewayClass string
}

type Provider struct {
	cfg          Config
	gwProvider   *gatewayapi.Provider
	dynamic      dynamic.Interface
	installation string
	guard        func(context.Context) error
}

func NewGatewayAPI(client gatewayclient.Interface, installation string, guard func(context.Context) error) *Provider {
	return New(Config{Installation: installation, NativeCEC: false, GatewayClass: "cilium"}, client, nil, guard)
}

func NewNative(dynamic dynamic.Interface, installation string, guard func(context.Context) error) *Provider {
	return New(Config{Installation: installation, NativeCEC: true}, nil, dynamic, guard)
}

func New(cfg Config, gwClient gatewayclient.Interface, dyn dynamic.Interface, guard func(context.Context) error) *Provider {
	if cfg.GatewayClass == "" {
		cfg.GatewayClass = "cilium"
	}
	var gw *gatewayapi.Provider
	if gwClient != nil {
		gw = gatewayapi.NewWithGatewayClass(gwClient, cfg.Installation, guard, cfg.GatewayClass)
	}
	return &Provider{
		cfg:          cfg,
		gwProvider:   gw,
		dynamic:      dyn,
		installation: cfg.Installation,
		guard:        guard,
	}
}

func (p *Provider) writable(ctx context.Context) error {
	if p.guard == nil {
		return fmt.Errorf("provider mutation requires leadership guard")
	}
	return p.guard(ctx)
}

func (p *Provider) owned(u *unstructured.Unstructured, token string) bool {
	labels := u.GetLabels()
	annotations := u.GetAnnotations()
	return token != "" && labels[installationLabel] == p.installation && annotations[ownershipAnnotation] == token
}

func aggregateToken(installation string) string {
	return "aggregate:" + installation
}

func domains(snapshot domain.RouteSnapshot) []domain.RouteDomain {
	byHost := map[string]domain.RouteDomain{}
	for _, d := range snapshot.Domains {
		byHost[d.ServiceHost] = d
	}
	for _, e := range snapshot.MeshEntries {
		byHost[e.Domain.ServiceHost] = e.Domain
	}
	out := make([]domain.RouteDomain, 0, len(byHost))
	for _, d := range byHost {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceHost < out[j].ServiceHost })
	return out
}

func parseServiceHost(host, fallbackNamespace string) (name, namespace string) {
	parts := strings.Split(host, ".")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return parts[0], fallbackNamespace
}

func (p *Provider) ValidateBaseline(ctx context.Context, b domain.Baseline, components map[string]domain.Component) error {
	if !p.cfg.NativeCEC {
		if p.gwProvider == nil {
			return fmt.Errorf("gateway-api client not configured for Cilium Gateway API mode")
		}
		return p.gwProvider.ValidateBaseline(ctx, b, components)
	}

	// In Native CEC mode, baseline namespace must be non-empty and entry component valid
	if b.Routing.Namespace == "" {
		return domain.Validation("Cilium native mode requires routing namespace")
	}
	if b.Routing.EntryComponent == "" {
		return domain.Validation("Cilium native mode requires entry component")
	}
	return nil
}

func (p *Provider) Reconcile(ctx context.Context, snapshot domain.RouteSnapshot) (domain.RouteObservation, error) {
	if !p.cfg.NativeCEC {
		if p.gwProvider == nil {
			return domain.RouteObservation{}, fmt.Errorf("gateway-api client not configured for Cilium Gateway API mode")
		}
		return p.gwProvider.Reconcile(ctx, snapshot)
	}

	// Native CiliumEnvoyConfig (CEC) mode
	if p.dynamic == nil {
		return domain.RouteObservation{}, fmt.Errorf("dynamic kubernetes client required for Cilium native mode")
	}
	if err := p.writable(ctx); err != nil {
		return domain.RouteObservation{}, err
	}

	cecClient := p.dynamic.Resource(ciliumEnvoyConfigGVR)

	// Reconcile CiliumEnvoyConfig for each aggregate domain
	for _, d := range domains(snapshot) {
		svcName, svcNamespace := parseServiceHost(d.ServiceHost, d.Namespace)
		cecName := d.AggregateName

		routes := []any{}
		for _, e := range snapshot.MeshEntries {
			if e.Domain.ServiceHost != d.ServiceHost {
				continue
			}
			destName, destNamespace := parseServiceHost(e.DestinationHost, domain.NamespaceForID(e.CompositionID))
			routes = append(routes, map[string]any{
				"match": map[string]any{
					"safe_regex": map[string]any{
						"google_re2": map[string]any{},
						"regex":      routing.BaggagePattern(e.CompositionID),
					},
					"name": "baggage",
				},
				"route": map[string]any{
					"cluster": fmt.Sprintf("k8s/%s/%s:%d", destNamespace, destName, e.Port),
				},
			})
		}

		// Fallback baseline route
		routes = append(routes, map[string]any{
			"match": map[string]any{
				"prefix": "/",
			},
			"route": map[string]any{
				"cluster": fmt.Sprintf("k8s/%s/%s:%d", svcNamespace, svcName, d.Port),
			},
		})

		cec := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "cilium.io/v2",
				"kind":       "CiliumEnvoyConfig",
				"metadata": map[string]any{
					"name":      cecName,
					"namespace": d.Namespace,
					"labels": map[string]any{
						installationLabel: p.installation,
						roleLabel:         "aggregate",
					},
					"annotations": map[string]any{
						ownershipAnnotation: aggregateToken(p.installation),
					},
				},
				"spec": map[string]any{
					"services": []any{
						map[string]any{
							"name":      svcName,
							"namespace": svcNamespace,
							"ports":     []any{int64(d.Port)},
						},
					},
					"resources": []any{
						map[string]any{
							"@type": "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
							"name":  cecName,
							"virtual_hosts": []any{
								map[string]any{
									"name":    svcName,
									"domains": []any{d.ServiceHost, svcName, fmt.Sprintf("%s:%d", svcName, d.Port)},
									"routes":  routes,
								},
							},
						},
					},
				},
			},
		}

		existing, err := cecClient.Namespace(d.Namespace).Get(ctx, cecName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if err := p.writable(ctx); err != nil {
				return domain.RouteObservation{}, err
			}
			_, err = cecClient.Namespace(d.Namespace).Create(ctx, cec, metav1.CreateOptions{})
			if err != nil {
				return domain.RouteObservation{}, err
			}
		} else if err != nil {
			return domain.RouteObservation{}, err
		} else {
			if !p.owned(existing, aggregateToken(p.installation)) {
				return domain.RouteObservation{}, fmt.Errorf("cilium aggregate ownership conflict: %s/%s", d.Namespace, cecName)
			}
			cec.SetResourceVersion(existing.GetResourceVersion())
			if err := p.writable(ctx); err != nil {
				return domain.RouteObservation{}, err
			}
			_, err = cecClient.Namespace(d.Namespace).Update(ctx, cec, metav1.UpdateOptions{})
			if err != nil {
				return domain.RouteObservation{}, err
			}
		}
	}

	// Clean up stale preview ingress CECs if any
	wantCompositions := map[string]bool{}
	for _, e := range snapshot.IngressEntries {
		wantCompositions[e.CompositionID] = true
	}
	existingList, err := cecClient.List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=ingress", installationLabel, p.installation, roleLabel),
	})
	if err == nil {
		for _, item := range existingList.Items {
			compID := item.GetLabels()[compositionLabel]
			if !wantCompositions[compID] {
				token := snapshot.OwnedCompositions[compID]
				if token == "" || !p.owned(&item, token) {
					continue
				}
				if err := p.writable(ctx); err != nil {
					return domain.RouteObservation{}, err
				}
				uid := types.UID(item.GetUID())
				_ = cecClient.Namespace(item.GetNamespace()).Delete(ctx, item.GetName(), metav1.DeleteOptions{
					Preconditions: &metav1.Preconditions{UID: &uid},
				})
			}
		}
	}

	return domain.RouteObservation{Ready: true, Message: "cilium routing resources accepted; eBPF datapath convergence active"}, nil
}

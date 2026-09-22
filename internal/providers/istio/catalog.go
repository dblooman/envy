package istio

import (
	"context"
	"maps"
	"net/url"
	"strconv"
	"strings"

	networking "istio.io/api/networking/v1alpha3"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/dblooman/envy/internal/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (p *Provider) ValidateBaseline(ctx context.Context, b domain.Baseline, _ map[string]domain.Component) error {
	if b.Routing.GatewaySectionName != "" {
		return domain.Validation("gateway_section_name applies to Gateway API listeners; omit it for Istio VirtualServices")
	}

	gateway, err := p.client.NetworkingV1().Gateways(b.Routing.GatewayNS()).Get(ctx, b.Routing.Gateway, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return domain.Validation("Istio Gateway " + b.Routing.Namespace + "/" + b.Routing.Gateway + " does not exist")
		}

		return &domain.Error{Code: "unavailable", Message: "cannot inspect Istio Gateway; check controller Kubernetes access", Retryable: true}
	}

	// This installation's verifier and exposure use this ingress deployment.
	if !maps.Equal(gateway.Spec.Selector, p.ingressSelector) {
		return domain.Validation("Gateway must select the installation's istio ingressgateway")
	}

	endpoint, _ := url.Parse(b.Endpoint)
	// Every generated composition name is a sibling under the configured domain.
	suffix := strings.SplitN(endpoint.Hostname(), ".", 2)
	if len(suffix) != 2 {
		return domain.Validation("invalid baseline hostname")
	}

	previewHost := "cmp-catalog-validation." + suffix[1]
	port := 80
	protocol := "HTTP"
	if endpoint.Scheme == "https" {
		port, protocol = 443, "HTTPS"
	}

	if endpoint.Port() != "" && endpoint.Scheme == "https" {
		port, _ = strconv.Atoi(endpoint.Port())
	}

	covered := func(host string) bool {
		for _, server := range gateway.Spec.Servers {
			if server.Port != nil && server.Port.Number == uint32(port) && server.Port.Protocol == protocol {
				if protocol == "HTTPS" && (server.Tls == nil || server.Tls.Mode != networking.ServerTLSSettings_SIMPLE || server.Tls.CredentialName == "") {
					continue
				}

				for _, pattern := range server.Hosts {
					if hostOverlap(pattern, host) && (host != previewHost || pattern == "*" || strings.HasPrefix(pattern, "*.")) {
						return true
					}
				}
			}
		}

		return false
	}
	if !covered(endpoint.Hostname()) || !covered(previewHost) {
		return domain.Validation("Gateway HTTP hosts must cover the baseline and composition domain")
	}

	snapshot := domain.RouteSnapshot{}
	for id := range b.Components {
		snapshot.Domains = append(snapshot.Domains, b.RouteDomain(id))
	}

	// A wildcard on any gateway can steal public composition hostnames. Check all
	// ingress rules selecting this gateway before accepting its routing domain.
	d := b.RouteDomain(b.Routing.EntryComponent)
	snapshot.IngressEntries = []domain.RouteEntry{{Domain: d, Host: previewHost}}
	if err = p.Validate(ctx, snapshot); err != nil {
		return &domain.Error{Code: "conflict", Message: err.Error()}
	}

	list, err := p.client.NetworkingV1().VirtualServices("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	matches := 0
	entry := b.Components[b.Routing.EntryComponent]
	for _, v := range list.Items {
		if !preview(v, d) {
			continue
		}

		for _, host := range v.Spec.Hosts {
			if !hostOverlap(host, endpoint.Hostname()) {
				continue
			}

			matches++
			if host != endpoint.Hostname() {
				return domain.Validation("baseline ingress must have an exact-host route")
			}

			var rule *networking.HTTPRoute
			if selector := b.Routing.PreviewSelector; selector != nil {
				selectorEntry := domain.RouteEntry{Domain: b.RouteDomain(b.Routing.EntryComponent), Host: endpoint.Hostname(), SelectorHeader: selector.Header}
				if len(v.Spec.Http) != 2 || len(v.Spec.Http[0].Match) != 0 || v.Spec.Http[0].Delegate == nil ||
					v.Spec.Http[0].Delegate.Name != selectorName(selectorEntry) || v.Spec.Http[0].Delegate.Namespace != b.Routing.Namespace ||
					len(v.Spec.Http[0].Route) != 0 || v.Spec.Http[0].Headers != nil {
					return domain.Validation("baseline ingress must delegate selector-header requests to Envy")
				}

				rule = v.Spec.Http[1]
			} else {
				if len(v.Spec.Http) != 1 || len(v.Spec.Http[0].Match) != 0 {
					return domain.Validation("baseline ingress must have one exact-host unconditional HTTP route")
				}

				rule = v.Spec.Http[0]
			}

			if rule.Headers == nil || rule.Headers.Request == nil || len(rule.Route) != 1 || rule.Rewrite != nil || rule.Redirect != nil || rule.Mirror != nil || len(rule.Mirrors) != 0 || rule.Fault != nil {
				return domain.Validation("baseline ingress requires a direct route with baggage removal")
			}

			headers := rule.Headers.Request
			removed := false
			for _, key := range headers.Remove {
				if strings.EqualFold(key, "baggage") {
					removed = true
				}
			}
			if selector := b.Routing.PreviewSelector; selector != nil {
				removedSelector := false
				for _, key := range headers.Remove {
					if strings.EqualFold(key, selector.Header) {
						removedSelector = true
					}
				}

				if !removedSelector {
					return domain.Validation("baseline ingress must remove the preview selector header")
				}
			}

			for key := range headers.Set {
				if strings.EqualFold(key, "baggage") {
					return domain.Validation("baseline ingress must not set baggage")
				}
			}

			for key := range headers.Add {
				if strings.EqualFold(key, "baggage") {
					return domain.Validation("baseline ingress must not add baggage")
				}
			}

			dest := rule.Route[0].Destination
			if !removed || dest == nil || normalizeHost(dest.Host, v.Namespace) != entry.ServiceHost || dest.Port == nil || int32(dest.Port.Number) != entry.Port || dest.Subset != "" || rule.Route[0].Headers != nil {
				return domain.Validation("baseline ingress must remove baggage and route directly to the entry binding")
			}
		}
	}

	if matches != 1 {
		return domain.Validation("baseline requires exactly one existing ingress route")
	}

	return nil
}

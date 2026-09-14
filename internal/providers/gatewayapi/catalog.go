package gatewayapi

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (p *Provider) ValidateBaseline(ctx context.Context, b domain.Baseline, _ map[string]domain.Component) error {
	gw, err := p.client.GatewayV1().Gateways(b.Routing.Namespace).Get(ctx, b.Routing.Gateway, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return domain.Validation("Gateway " + b.Routing.Namespace + "/" + b.Routing.Gateway + " does not exist")
		}
		return &domain.Error{Code: "unavailable", Message: "cannot inspect Gateway; check controller Kubernetes access", Retryable: true}
	}

	if p.gatewayClass != "" && string(gw.Spec.GatewayClassName) != p.gatewayClass {
		return domain.Validation("Gateway must use gatewayClassName " + p.gatewayClass)
	}

	endpoint, _ := url.Parse(b.Endpoint)
	suffix := strings.SplitN(endpoint.Hostname(), ".", 2)
	if len(suffix) != 2 {
		return domain.Validation("invalid baseline hostname")
	}
	previewHost := "cmp-catalog-validation." + suffix[1]

	port := 80
	protocol := gatewayv1.HTTPProtocolType
	if endpoint.Scheme == "https" {
		port, protocol = 443, gatewayv1.HTTPSProtocolType
	}
	if endpoint.Port() != "" && endpoint.Scheme == "https" {
		port, _ = strconv.Atoi(endpoint.Port())
	}

	covered := func(host string) bool {
		for _, listener := range gw.Spec.Listeners {
			if int(listener.Port) == port && listener.Protocol == protocol {
				if protocol == gatewayv1.HTTPSProtocolType && (listener.TLS == nil || listener.TLS.Mode == nil || *listener.TLS.Mode != gatewayv1.TLSModeTerminate) {
					continue
				}
				if listener.Hostname == nil || string(*listener.Hostname) == "*" || string(*listener.Hostname) == "" {
					return true
				}
				pattern := string(*listener.Hostname)
				if hostOverlap(pattern, host) && (host != previewHost || strings.HasPrefix(pattern, "*.")) {
					return true
				}
			}
		}
		return false
	}

	if !covered(endpoint.Hostname()) || !covered(previewHost) {
		return domain.Validation("Gateway listeners must cover the baseline and composition domain on " + string(protocol) + " port " + strconv.Itoa(port))
	}

	if b.Routing.EntryComponent == "" {
		return domain.Validation("baseline requires an entry component")
	}
	entry, ok := b.Components[b.Routing.EntryComponent]
	if !ok {
		return domain.Validation("baseline entry component not found in components")
	}

	snapshot := domain.RouteSnapshot{}
	for id := range b.Components {
		snapshot.Domains = append(snapshot.Domains, b.RouteDomain(id))
	}
	d := b.RouteDomain(b.Routing.EntryComponent)
	snapshot.IngressEntries = []domain.RouteEntry{{Domain: d, Host: previewHost}}
	if err = p.Validate(ctx, snapshot); err != nil {
		return &domain.Error{Code: "conflict", Message: err.Error()}
	}

	list, err := p.client.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	matches := 0

	for _, r := range list.Items {
		if !attachesToGateway(&r, b.Routing.Namespace, b.Routing.Gateway) {
			continue
		}
		hosts := make([]string, 0, len(r.Spec.Hostnames))
		for _, h := range r.Spec.Hostnames {
			hosts = append(hosts, string(h))
		}
		if len(hosts) == 0 {
			hosts = append(hosts, "*")
		}
		for _, host := range hosts {
			if !hostOverlap(host, endpoint.Hostname()) {
				continue
			}
			matches++
			if host != endpoint.Hostname() || len(r.Spec.Rules) != 1 {
				return domain.Validation("baseline ingress must have one exact-host unconditional HTTP route")
			}
			rule := r.Spec.Rules[0]
			if len(rule.Matches) > 0 {
				for _, m := range rule.Matches {
					if len(m.Headers) > 0 || len(m.QueryParams) > 0 || m.Method != nil {
						return domain.Validation("baseline ingress must have one exact-host unconditional HTTP route")
					}
					if m.Path != nil && (m.Path.Type == nil || *m.Path.Type != gatewayv1.PathMatchPathPrefix || m.Path.Value == nil || *m.Path.Value != "/") {
						return domain.Validation("baseline ingress must have one exact-host unconditional HTTP route")
					}
				}
			}
			if len(rule.BackendRefs) != 1 {
				return domain.Validation("baseline ingress requires a direct route with baggage removal")
			}
			bRef := rule.BackendRefs[0]
			bGroup := ""
			if bRef.Group != nil && *bRef.Group != "" {
				bGroup = string(*bRef.Group)
			}
			bKind := "Service"
			if bRef.Kind != nil && *bRef.Kind != "" {
				bKind = string(*bRef.Kind)
			}
			if (bGroup != "" && bGroup != "core") || bKind != "Service" || bRef.Port == nil || len(bRef.Filters) > 0 {
				return domain.Validation("baseline ingress requires a direct route with baggage removal")
			}

			var headerFilter *gatewayv1.HTTPHeaderFilter
			for _, filter := range rule.Filters {
				if filter.Type == gatewayv1.HTTPRouteFilterRequestHeaderModifier {
					headerFilter = filter.RequestHeaderModifier
				} else if filter.Type == gatewayv1.HTTPRouteFilterURLRewrite || filter.Type == gatewayv1.HTTPRouteFilterRequestRedirect {
					return domain.Validation("baseline ingress requires a direct route with baggage removal")
				}
			}
			if headerFilter == nil {
				return domain.Validation("baseline ingress requires a direct route with baggage removal")
			}
			removed := false
			for _, key := range headerFilter.Remove {
				if strings.EqualFold(key, "baggage") {
					removed = true
				}
			}
			for _, h := range headerFilter.Set {
				if strings.EqualFold(string(h.Name), "baggage") {
					return domain.Validation("baseline ingress must not set baggage")
				}
			}
			for _, h := range headerFilter.Add {
				if strings.EqualFold(string(h.Name), "baggage") {
					return domain.Validation("baseline ingress must not add baggage")
				}
			}

			bNs := r.Namespace
			if bRef.Namespace != nil && *bRef.Namespace != "" {
				bNs = string(*bRef.Namespace)
			}
			targetHost := normalizeHost(string(bRef.Name), bNs)
			expectedHost := normalizeHost(entry.ServiceHost, b.Routing.Namespace)
			if !removed || targetHost != expectedHost || int32(*bRef.Port) != entry.Port {
				return domain.Validation("baseline ingress must remove baggage and route directly to the entry binding")
			}
		}
	}

	if matches != 1 {
		return domain.Validation("baseline requires exactly one existing ingress route")
	}

	return nil
}

func attachesToGateway(r *gatewayv1.HTTPRoute, gwNamespace, gwName string) bool {
	for _, pRef := range r.Spec.ParentRefs {
		pGroup := gatewayv1.GroupName
		if pRef.Group != nil && *pRef.Group != "" {
			pGroup = string(*pRef.Group)
		}
		pKind := "Gateway"
		if pRef.Kind != nil && *pRef.Kind != "" {
			pKind = string(*pRef.Kind)
		}
		pNs := r.Namespace
		if pRef.Namespace != nil && *pRef.Namespace != "" {
			pNs = string(*pRef.Namespace)
		}
		if (pGroup == "" || pGroup == gatewayv1.GroupName) && pKind == "Gateway" && string(pRef.Name) == gwName && pNs == gwNamespace {
			return true
		}
	}
	return false
}

func normalizeHost(host, ns string) string {
	if !strings.Contains(host, ".") && host != "*" {
		return host + "." + ns + ".svc.cluster.local"
	}
	return host
}

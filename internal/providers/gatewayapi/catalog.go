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

	return nil
}

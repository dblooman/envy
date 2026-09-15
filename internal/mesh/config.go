// Package mesh defines the supported installation profiles. Gateway API is an
// implementation detail, not a mesh or an independently supported profile.
package mesh

import (
	"fmt"
	"net/url"
)

type Config struct {
	Provider string `json:"provider"`
}

type Profile struct {
	Name              string
	GatewayClass      string
	GatewayController string
	MeshController    string
	ProxyContainer    string
}

func Resolve(provider string) (Profile, error) {
	switch provider {
	case "", "istio":
		return Profile{Name: "istio", ProxyContainer: "istio-proxy"}, nil
	case "cilium":
		return Profile{Name: "cilium", GatewayClass: "cilium", GatewayController: "io.cilium/gateway-controller", MeshController: "io.cilium/gateway-controller"}, nil
	case "linkerd":
		return Profile{Name: "linkerd", GatewayClass: "eg", GatewayController: "gateway.envoyproxy.io/gatewayclass-controller", MeshController: "linkerd.io/policy-controller", ProxyContainer: "linkerd-proxy"}, nil
	case "gateway-api":
		return Profile{}, fmt.Errorf("mesh.provider gateway-api is retired: drain compositions using the previous server, then choose cilium or linkerd in a fresh installation")
	default:
		return Profile{}, fmt.Errorf("unknown mesh.provider %q: choose istio, cilium, or linkerd", provider)
	}
}

func (p Profile) IngressURL(value string) (string, error) {
	if value == "" && p.Name == "istio" {
		return "http://istio-ingressgateway.istio-system.svc.cluster.local", nil
	}

	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%s requires an explicit absolute HTTP(S) runtime.ingressURL", p.Name)
	}

	return value, nil
}

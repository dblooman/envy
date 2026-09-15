package gatewayapi

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kube "k8s.io/client-go/kubernetes"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (p *Provider) WithKubernetes(k kube.Interface) *Provider { p.kube = k; return p }

func (p *Provider) CheckGateway(ctx context.Context, namespace, name, section, routeNamespace string) error {
	gw, err := p.client.GatewayV1().Gateways(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("inspect Gateway %s/%s: %w", namespace, name, err)
	}

	if string(gw.Spec.GatewayClassName) != p.gatewayClass {
		return fmt.Errorf("Gateway must use GatewayClass %s", p.gatewayClass)
	}

	class, err := p.client.GatewayV1().GatewayClasses().Get(ctx, p.gatewayClass, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("inspect GatewayClass: %w", err)
	}

	if string(class.Spec.ControllerName) != p.profile.GatewayController {
		return fmt.Errorf("GatewayClass must use controller %s", p.profile.GatewayController)
	}

	if msg := conditionPending(class.Status.Conditions, class.Generation, "Accepted"); msg != "" {
		return fmt.Errorf("GatewayClass: %s", msg)
	}

	if msg := conditionPending(gw.Status.Conditions, gw.Generation, "Accepted", "Programmed"); msg != "" {
		return fmt.Errorf("Gateway: %s", msg)
	}

	for _, l := range gw.Spec.Listeners {
		if section != "" && string(l.Name) != section {
			continue
		}

		if l.Protocol != v1.HTTPProtocolType && l.Protocol != v1.HTTPSProtocolType {
			continue
		}

		if err = p.listenerAllows(ctx, l, namespace, routeNamespace); err != nil {
			continue
		}

		for _, s := range gw.Status.Listeners {
			if s.Name == l.Name {
				if msg := conditionPending(s.Conditions, gw.Generation, "Accepted", "Programmed", "ResolvedRefs"); msg == "" {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("Gateway has no ready HTTP(S) listener allowing HTTPRoutes from %s (section %q)", routeNamespace, section)
}
func (p *Provider) listenerAllows(ctx context.Context, l v1.Listener, gatewayNS, routeNS string) error {
	allowed := l.AllowedRoutes
	if allowed != nil && len(allowed.Kinds) > 0 {
		ok := false
		for _, k := range allowed.Kinds {
			if k.Kind == "HTTPRoute" && (k.Group == nil || *k.Group == v1.GroupName) {
				ok = true
			}
		}

		if !ok {
			return fmt.Errorf("listener disallows HTTPRoute")
		}
	}

	from := v1.NamespacesFromSame
	if allowed != nil && allowed.Namespaces != nil && allowed.Namespaces.From != nil {
		from = *allowed.Namespaces.From
	}

	switch from {
	case v1.NamespacesFromAll:
		return nil
	case v1.NamespacesFromSame:
		if gatewayNS == routeNS {
			return nil
		}
	case v1.NamespacesFromSelector:
		if p.kube == nil {
			return fmt.Errorf("namespace client required to validate listener selector")
		}

		ns, err := p.kube.CoreV1().Namespaces().Get(ctx, routeNS, metav1.GetOptions{})
		if err != nil {
			return err
		}

		selector, err := metav1.LabelSelectorAsSelector(allowed.Namespaces.Selector)
		if err != nil {
			return err
		}

		if selector.Matches(labels.Set(ns.Labels)) {
			return nil
		}
	}

	return fmt.Errorf("listener does not allow route namespace %s", routeNS)
}

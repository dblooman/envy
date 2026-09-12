package kubernetes

import (
	"context"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// ValidateBaseline only inspects borrowed resources. It never adopts workloads.
func (p *Provider) ValidateBaseline(ctx context.Context, b domain.Baseline, profiles map[string]domain.Component) error {
	ns, err := p.client.CoreV1().Namespaces().Get(ctx, b.Routing.Namespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return domain.Validation("baseline namespace " + b.Routing.Namespace + " does not exist")
		}
		return &domain.Error{Code: "unavailable", Message: "cannot inspect baseline namespace; check controller Kubernetes access", Retryable: true}
	}
	if ns.DeletionTimestamp != nil {
		return domain.Validation("baseline namespace is terminating")
	}
	for key, value := range p.injection {
		if ns.Labels[key] != value {
			return domain.Validation("baseline namespace must have Istio sidecar injection enabled")
		}
	}
	for id, binding := range b.Components {
		name, _, _ := strings.Cut(binding.ServiceHost, ".")
		svc, err := p.client.CoreV1().Services(ns.Name).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return domain.Validation(fmt.Sprintf("baseline Service %s for component %s does not exist", name, id))
			}
			return &domain.Error{Code: "unavailable", Message: "cannot inspect baseline Service for " + id + "; check controller Kubernetes access", Retryable: true}
		}
		if svc.Spec.Type == corev1.ServiceTypeExternalName || len(svc.Spec.Selector) == 0 {
			return domain.Validation("baseline Services must select pods in their namespace")
		}
		http := false
		for _, port := range svc.Spec.Ports {
			if port.Port == binding.Port && port.Protocol == corev1.ProtocolTCP && ((port.AppProtocol != nil && *port.AppProtocol == "http") || port.Name == "http" || strings.HasPrefix(port.Name, "http-")) {
				http = true
			}
		}
		if !http {
			return domain.Validation("baseline component " + id + " must bind an explicitly declared HTTP Service port")
		}
		pods, err := p.client.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(), Limit: 100})
		if err != nil {
			return err
		}
		if pods.Continue != "" {
			return domain.Validation("baseline Service selects too many pods for the supported verification contract")
		}
		ready := false
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}
			app, sidecar, podReady := false, false, false
			for _, container := range pod.Spec.Containers {
				if container.Name == id {
					app = true
				}
			}
			for _, status := range pod.Status.ContainerStatuses {
				if status.Name == "istio-proxy" && status.Ready {
					sidecar = true
				}
			}
			for _, status := range pod.Status.InitContainerStatuses {
				if status.Name == "istio-proxy" && status.Ready {
					sidecar = true
				}
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					podReady = true
				}
			}
			if app && sidecar && podReady {
				ready = true
			}
		}
		if !ready {
			return domain.Validation("baseline component " + id + " requires a ready pod with both its named application container and an Istio sidecar")
		}
	}
	return nil
}

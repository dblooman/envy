package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// ObserveBaseline reads the registered namespace only. Secret and ConfigMap
// contents are never fetched, and ambiguous execution identity stays unknown.
func (p *Provider) ObserveBaseline(ctx context.Context, baseline domain.Baseline, overrides map[string]domain.ComponentOverride) (domain.BaselineObservation, error) {
	out := domain.BaselineObservation{Installation: p.installation, Project: baseline.Project, Baseline: baseline.ID, State: "current", ObservedAt: time.Now().UTC(), Components: map[string]domain.BaselineComponentObservation{}}
	namespace := baseline.Routing.Namespace
	if namespace == "" {
		return out, fmt.Errorf("registered baseline has no namespace")
	}

	// Verification freshness cannot trust an informer cache after its watch
	// disconnects. These named/namespace-scoped reads go to the API server.
	deployments, err := p.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, fmt.Errorf("observe baseline deployments in %s: %w", namespace, err)
	}

	for name, binding := range baseline.Components {
		if binding.ServiceHost == "" {
			continue
		}

		serviceName, _, _ := strings.Cut(binding.ServiceHost, ".")
		service, err := p.client.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			return out, fmt.Errorf("observe baseline Service %s/%s: %w", namespace, serviceName, err)
		}

		component := domain.BaselineComponentObservation{ServiceUID: string(service.UID), ServiceSelector: maps.Clone(service.Spec.Selector), ServicePorts: []domain.BaselineServicePort{}, ExecutionState: "unknown", ImageIdentity: "unknown"}
		for _, port := range service.Spec.Ports {
			component.ServicePorts = append(component.ServicePorts, domain.BaselineServicePort{Name: port.Name, Protocol: string(port.Protocol), Port: port.Port, TargetPort: port.TargetPort.String()})
		}

		if _, selected := overrides[name]; !selected {
			if err := p.observeInheritedExecution(ctx, namespace, service, deployments.Items, &component); err != nil {
				return out, fmt.Errorf("observe inherited execution for %s: %w", name, err)
			}
		} else {
			component.ExecutionState = "overridden"
		}

		out.Components[name] = component
	}

	out.CalculateFingerprint()
	return out, nil
}

func (p *Provider) observeInheritedExecution(ctx context.Context, namespace string, service *corev1.Service, deployments []appsv1.Deployment, out *domain.BaselineComponentObservation) error {
	if len(service.Spec.Selector) == 0 {
		return nil
	}

	selected := matchingBaselineDeployment(service.Spec.Selector, deployments)
	if selected == nil {
		return nil
	}

	out.ExecutionState = "observed"
	out.DeploymentUID = string(selected.UID)
	out.DeploymentGeneration = selected.Generation
	template, _ := json.Marshal(selected.Spec.Template)
	sum := sha256.Sum256(template)
	out.TemplateFingerprint = hex.EncodeToString(sum[:])
	out.DeclaredImages = map[string]string{}
	for _, container := range append(slices.Clone(selected.Spec.Template.Spec.Containers), selected.Spec.Template.Spec.InitContainers...) {
		out.DeclaredImages[container.Name] = container.Image
	}

	pods, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(service.Spec.Selector).String()})
	if err != nil {
		return err
	}

	actual := map[string]map[string]bool{}
	for _, pod := range pods.Items {
		for _, status := range append(slices.Clone(pod.Status.ContainerStatuses), pod.Status.InitContainerStatuses...) {
			if _, expected := out.DeclaredImages[status.Name]; !expected || status.ImageID == "" {
				continue
			}

			if actual[status.Name] == nil {
				actual[status.Name] = map[string]bool{}
			}

			actual[status.Name][status.ImageID] = true
		}
	}

	out.ActualImageIDs = map[string][]string{}
	out.ImageIdentity = "known"
	for name := range out.DeclaredImages {
		ids := slices.Sorted(maps.Keys(actual[name]))
		if len(ids) == 0 {
			out.ImageIdentity = "unknown"
		} else {
			out.ActualImageIDs[name] = ids
		}
	}

	return nil
}

func matchingBaselineDeployment(serviceSelector map[string]string, deployments []appsv1.Deployment) *appsv1.Deployment {
	selector := labels.SelectorFromSet(serviceSelector)
	var selected *appsv1.Deployment
	for i := range deployments {
		if !selector.Matches(labels.Set(deployments[i].Spec.Template.Labels)) || deployments[i].DeletionTimestamp != nil {
			continue
		}

		if selected != nil {
			return nil // More than one execution matches; do not guess.
		}

		selected = &deployments[i]
	}

	return selected
}

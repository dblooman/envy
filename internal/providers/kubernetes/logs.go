package kubernetes

import (
	"context"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	kube "k8s.io/client-go/kubernetes"
)

type LogProvider struct {
	client        kube.Interface
	installation  string
	previewPolicy PreviewPolicy
	stream        func(context.Context, string, string, *corev1.PodLogOptions) (io.ReadCloser, error)
}

func NewLogReader(client kube.Interface, installation string) *LogProvider {
	return &LogProvider{client: client, installation: installation, stream: func(ctx context.Context, namespace, pod string, options *corev1.PodLogOptions) (io.ReadCloser, error) {
		return client.CoreV1().Pods(namespace).GetLogs(pod, options).Stream(ctx)
	}}
}

// WithPreviewPolicy selects inherited composite applications using the current
// operator contract; source container names are never accepted from callers.
func (p *LogProvider) WithPreviewPolicy(policy PreviewPolicy) *LogProvider {
	p.previewPolicy = policy
	return p
}

func (p *LogProvider) logContainer(target domain.LogTarget, options domain.LogOptions) (string, error) {
	if target.Source == "shared-baseline" && target.BaselineComposite {
		if options.Container != "" && options.Container != target.Component {
			return "", domain.Validation("supporting container selection requires a captured override contract")
		}

		key := target.Project + "/" + target.Baseline + "/" + target.Component
		policy, ok := p.previewPolicy.Composite[key]
		if !ok {
			return "", domain.Validation("composite baseline logs require an installed operator policy for " + key)
		}

		if err := validateCompositePolicy(policy); err != nil {
			return "", domain.Validation("composite baseline log policy is invalid")
		}

		return policy.ApplicationContainer, nil
	}

	selected := target.Component
	if options.Container != "" {
		selected = options.Container
		if selected != target.Component && (target.Source != "override" || !slices.Contains(target.AllowedContainers, selected)) {
			return "", domain.Validation("container is not in the captured preview execution contract")
		}
	}

	return selected, nil
}

func logUnavailable() error {
	return &domain.Error{Code: "unavailable", Message: "Kubernetes component logs are unavailable", Retryable: true}
}

func logConflict() error {
	return &domain.Error{Code: "conflict", Message: "log workload ownership or identity changed"}
}

func (p *LogProvider) ReadLogs(ctx context.Context, target domain.LogTarget, options domain.LogOptions) (domain.ComponentLogs, error) {
	result := domain.ComponentLogs{Streams: []domain.LogStream{}}
	options, err := domain.NormalizeLogOptions(options)
	if err != nil {
		return result, err
	}
	selected, err := p.logContainer(target, options)
	if err != nil {
		return result, err
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	namespace, service := "", ""
	if target.Source == "override" {
		ref := target.Workload
		if ref.Namespace == "" {
			return result, nil
		} // durable intent can predate provisioning

		if ref.Namespace != domain.NamespaceForID(target.Composition) || ref.Service != target.Component || ref.Deployment != target.Component || ref.OwnershipToken == "" {
			return result, logConflict()
		}

		namespace, service = ref.Namespace, ref.Service
		ns, err := p.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return result, nil
		}

		if err != nil {
			return result, logUnavailable()
		}

		owner := Provider{installation: p.installation}
		if owner.owned(ns, ref.OwnershipToken) != nil || (ref.NamespaceUID != "" && string(ns.UID) != ref.NamespaceUID) {
			return result, logConflict()
		}

		d, err := p.client.AppsV1().Deployments(namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return result, nil
		}

		if err != nil {
			return result, logUnavailable()
		}

		if owner.owned(d, ref.OwnershipToken) != nil || (ref.DeploymentUID != "" && string(d.UID) != ref.DeploymentUID) {
			return result, logConflict()
		}
	} else if target.Source == "shared-baseline" {
		parts := strings.Split(target.BaselineServiceHost, ".")
		if len(parts) != 5 || parts[2] != "svc" || parts[3] != "cluster" || parts[4] != "local" || len(validation.IsDNS1035Label(parts[0])) != 0 || len(validation.IsDNS1123Label(parts[1])) != 0 {
			return result, domain.Validation("baseline logs require a registered Kubernetes Service FQDN")
		}

		service, namespace = parts[0], parts[1]
	} else {
		return result, domain.Validation("unsupported log source")
	}

	svc, err := p.client.CoreV1().Services(namespace).Get(ctx, service, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return result, nil
	}

	if err != nil {
		return result, logUnavailable()
	}

	if len(svc.Spec.Selector) == 0 {
		return result, domain.Validation("logs require a registered Service with a pod selector")
	}

	if target.Source == "override" {
		owner := Provider{installation: p.installation}
		if owner.owned(svc, target.Workload.OwnershipToken) != nil || (target.Workload.ServiceUID != "" && string(svc.UID) != target.Workload.ServiceUID) {
			return result, logConflict()
		}

		if svc.Spec.Selector[CompositionLabel] != target.Composition || svc.Spec.Selector[ComponentLabel] != target.Component || svc.Spec.Selector[InstallationLabel] != p.installation {
			return result, logConflict()
		}
	}

	pods, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(), Limit: 100})
	if err != nil {
		return result, logUnavailable()
	}

	sort.Slice(pods.Items, func(i, j int) bool {
		a, b := pods.Items[i], pods.Items[j]
		if a.CreationTimestamp.Equal(&b.CreationTimestamp) {
			return a.Name < b.Name
		}

		return a.CreationTimestamp.After(b.CreationTimestamp.Time)
	})
	candidates := make([]corev1.Pod, 0, 3)
	for _, pod := range pods.Items {
		containers := pod.Spec.Containers
		if target.Source == "override" {
			containers = append(append([]corev1.Container{}, containers...), pod.Spec.InitContainers...)
		}

		for _, container := range containers {
			if container.Name == selected || (options.Container == "" && target.Source == "shared-baseline" && !target.BaselineComposite && container.Name != "istio-proxy" && countApplicationContainers(pod) == 1) {
				candidates = append(candidates, pod)
				break
			}
		}
	}

	result.Truncated = pods.Continue != "" || len(candidates) > 3
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}

	remaining := options.MaxBytes
	for _, pod := range candidates {
		if remaining <= 0 {
			result.Truncated = true
			break
		}

		containerName := selected
		if options.Container == "" && target.Source == "shared-baseline" && !target.BaselineComposite && countApplicationContainers(pod) == 1 {
			for _, c := range pod.Spec.Containers {
				if c.Name != "istio-proxy" {
					containerName = c.Name
				}
			}
		}

		item := domain.LogStream{Pod: pod.Name, WorkloadID: string(pod.UID), Container: containerName}
		max := remaining + 1
		opts := &corev1.PodLogOptions{Container: containerName, TailLines: &options.TailLines, LimitBytes: &max, Previous: options.Previous, Timestamps: true, Follow: false}
		if options.SinceSeconds > 0 {
			opts.SinceSeconds = &options.SinceSeconds
		}

		reader, err := p.stream(ctx, namespace, pod.Name, opts)
		if err == nil {
			var data []byte
			data, err = io.ReadAll(io.LimitReader(reader, max))
			reader.Close()
			if err == nil {
				// Pod names can be reused. Discard data if the identity changed during read.
				current, getErr := p.client.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
				if getErr != nil || current.UID != pod.UID {
					err = logConflict()
				} else {
					if int64(len(data)) >= remaining {
						item.Truncated = true
						result.Truncated = true
					}

					if int64(len(data)) > remaining {
						data = data[:remaining]
					}

					remaining -= int64(len(data))
					item.Text = strings.ToValidUTF8(string(data), "")
				}
			}
		}

		if err != nil {
			item.Error = &domain.Error{Code: "logs_unavailable", Message: "selected container logs unavailable for this pod or container instance", Retryable: true}
			result.Partial = true
		}

		result.Streams = append(result.Streams, item)
	}

	return result, nil
}

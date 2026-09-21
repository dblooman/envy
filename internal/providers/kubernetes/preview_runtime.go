package kubernetes

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	dependencyVersion            = "envy.dev/source-version"
	compositeExecutionAnnotation = "envy.dev/composite-execution"
)

func compositeExecutionFingerprint(template corev1.PodTemplateSpec) string {
	copy := template.DeepCopy()
	// The API maintains the deprecated alias from serviceAccountName. It is not
	// an independent execution setting, and discovery deliberately clears it.
	copy.Spec.DeprecatedServiceAccount = ""
	copy.ObjectMeta = metav1.ObjectMeta{Labels: copy.Labels, Annotations: copy.Annotations}
	return previewHash(copy)
}

func checkedCompositeDeployment(want, got *appsv1.Deployment, err error) (*appsv1.Deployment, error) {
	if err != nil || got == nil {
		return got, err
	}

	if expected := want.Annotations[compositeExecutionAnnotation]; expected != "" && (got.Annotations[compositeExecutionAnnotation] != expected || compositeExecutionFingerprint(got.Spec.Template) != expected) {
		return got, fmt.Errorf("composite execution changed during deployment admission; inspect the deployment and recreate the composition")
	}

	return got, nil
}

// Captured execution policy is revocable: a restart must not silently continue
// using an identity or supporting container that the operator no longer allows.
func (p *Provider) validateCompositeRuntime(s domain.WorkloadSpec) error {
	accounts := map[string]string{}
	for component, snapshot := range s.Previews {
		// Retained snapshots reserve quota and allow an override to be readded.
		// Revoking a removed component must not prevent remaining workloads or cleanup.
		if s.DesiredComponents != nil && !slices.Contains(s.DesiredComponents, component) {
			continue
		}

		if err := p.validateCompositeSnapshot(s, component, &snapshot); err != nil {
			return err
		}

		if err := claimCompositeAccount(accounts, component, &snapshot); err != nil {
			return err
		}
	}

	if captured, ok := s.Previews[s.ComponentID]; ok && s.Preview != nil && (previewHash(captured.CompositePolicy) != previewHash(s.Preview.CompositePolicy) || captured.CompositePolicyKey != s.Preview.CompositePolicyKey) {
		return fmt.Errorf("inconsistent captured composite preview policy")
	}

	if err := p.validateCompositeSnapshot(s, s.ComponentID, s.Preview); err != nil {
		return err
	}

	return claimCompositeAccount(accounts, s.ComponentID, s.Preview)
}

func claimCompositeAccount(accounts map[string]string, component string, snapshot *domain.PreviewSnapshot) error {
	if snapshot == nil || snapshot.CompositePolicy == nil {
		return nil
	}

	name := snapshot.CompositePolicy.ServiceAccount
	if prior, ok := accounts[name]; ok && prior != component {
		return fmt.Errorf("composite service account %s is reused by components %s and %s", name, prior, component)
	}

	accounts[name] = component
	return nil
}

func (p *Provider) validateCompositeSnapshot(s domain.WorkloadSpec, component string, snapshot *domain.PreviewSnapshot) error {
	if snapshot == nil {
		return nil
	}

	if snapshot.CompositePolicy == nil {
		if snapshot.CompositePolicyKey != "" {
			return fmt.Errorf("invalid captured composite preview policy")
		}

		return nil
	}

	policy := snapshot.CompositePolicy
	if !compositeSnapshotScopeMatches(s, component, snapshot) {
		return fmt.Errorf("captured composite preview policy scope does not match the workload")
	}

	installed, ok := p.previewPolicy.Composite[snapshot.CompositePolicyKey]
	// Compare the serialized contract so omitted empty maps/slices remain
	// equivalent after the snapshot has been persisted and loaded again.
	if !ok || previewHash(installed) != previewHash(*policy) {
		return fmt.Errorf("composite preview policy changed or was revoked; inspect and approve a new preview")
	}

	if policy.ServiceAccount == "" || policy.ServiceAccount == "envy-workload" {
		return fmt.Errorf("composite preview requires a dedicated approved service account")
	}

	template, err := decodePreview(snapshot)
	if err != nil {
		return err
	}

	app, err := previewApplication(&template, snapshot)
	if err != nil || app.Name != component || template.Spec.ServiceAccountName != policy.ServiceAccount || template.Spec.AutomountServiceAccountToken == nil || *template.Spec.AutomountServiceAccountToken {
		return fmt.Errorf("captured composite application or service account does not match its policy")
	}

	return nil
}

func compositeSnapshotScopeMatches(s domain.WorkloadSpec, component string, snapshot *domain.PreviewSnapshot) bool {
	parts := strings.Split(snapshot.CompositePolicyKey, "/")
	return len(parts) == 3 && parts[2] == component &&
		(s.ProjectID == "" || parts[0] == s.ProjectID) &&
		(s.BaselineNamespace == "" || snapshot.Source.Namespace == s.BaselineNamespace)
}

func previewInjectedMeshContainer(name string) bool {
	switch name {
	case "istio-proxy", "istio-init", "linkerd-proxy", "linkerd-init":
		return true
	default:
		return false
	}
}

// previewPodResources implements the Kubernetes effective Pod resource model:
// regular containers and native sidecars run together; each sequential init
// runs alongside only the native sidecars that have already started. The mesh
// budget is captured separately and is added by previewQuota.
func previewPodResources(spec corev1.PodSpec) corev1.ResourceRequirements {
	add := func(dst, src corev1.ResourceList) {
		for key, value := range src {
			q := dst[key].DeepCopy()
			q.Add(value)
			dst[key] = q
		}
	}
	maximum := func(dst, src corev1.ResourceList) {
		for key, value := range src {
			if q := dst[key]; q.Cmp(value) < 0 {
				dst[key] = value.DeepCopy()
			}
		}
	}
	result := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	for _, container := range spec.Containers {
		if !previewInjectedMeshContainer(container.Name) {
			add(result.Requests, container.Resources.Requests)
			add(result.Limits, container.Resources.Limits)
		}
	}

	persistent := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	peak := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	for _, container := range spec.InitContainers {
		if previewInjectedMeshContainer(container.Name) {
			continue
		}

		current := corev1.ResourceRequirements{Requests: persistent.Requests.DeepCopy(), Limits: persistent.Limits.DeepCopy()}
		add(current.Requests, container.Resources.Requests)
		add(current.Limits, container.Resources.Limits)
		maximum(peak.Requests, current.Requests)
		maximum(peak.Limits, current.Limits)
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			persistent = current
		}
	}

	add(result.Requests, persistent.Requests)
	add(result.Limits, persistent.Limits)
	maximum(result.Requests, peak.Requests)
	maximum(result.Limits, peak.Limits)
	add(result.Requests, spec.Overhead)
	add(result.Limits, spec.Overhead)
	return result
}

// A deployment can fail before its application starts, or because a supporting
// process fails. Inspect both status collections so those causes stay visible.
func previewPodFailure(pod corev1.Pod) (string, bool) {
	waiting := ""
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, status := range statuses {
			if state := status.State.Terminated; state != nil && state.ExitCode != 0 {
				return fmt.Sprintf("container %s terminated: %s (exit %d)", status.Name, state.Reason, state.ExitCode), true
			}

			if state := status.State.Waiting; state != nil {
				message := "container " + status.Name + ": " + state.Reason
				if state.Message != "" {
					message += ": " + state.Message
				}

				switch state.Reason {
				case "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "CreateContainerConfigError", "CreateContainerError", "RunContainerError", "InvalidImageName":
					return message, true
				}

				if waiting == "" {
					waiting = message
				}
			}
		}
	}

	return waiting, false
}

func previewSupportReadiness(spec corev1.PodSpec, status corev1.PodStatus, application string) string {
	regular := map[string]corev1.ContainerStatus{}
	for _, container := range status.ContainerStatuses {
		regular[container.Name] = container
	}

	for _, container := range spec.Containers {
		if container.Name != application && !previewInjectedMeshContainer(container.Name) && (container.ReadinessProbe != nil || container.StartupProbe != nil) && !regular[container.Name].Ready {
			return "waiting for supporting container " + container.Name + " readiness"
		}
	}

	return previewInitReadiness(spec.InitContainers, status.InitContainerStatuses)
}

func previewInitReadiness(containers []corev1.Container, statuses []corev1.ContainerStatus) string {
	init := map[string]corev1.ContainerStatus{}
	for _, container := range statuses {
		init[container.Name] = container
	}

	for _, container := range containers {
		if previewInjectedMeshContainer(container.Name) {
			continue
		}

		state := init[container.Name]
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			if !state.Ready {
				return "waiting for native sidecar " + container.Name + " readiness"
			}
		} else if state.State.Terminated == nil || state.State.Terminated.ExitCode != 0 {
			return "waiting for init container " + container.Name + " completion"
		}
	}

	return ""
}

func (p *Provider) ensurePreviewDependencies(ctx context.Context, s domain.WorkloadSpec, ns string) error {
	var secrets []*corev1.Secret
	var configs []*corev1.ConfigMap
	for _, dep := range s.Preview.Dependencies {
		name := depName(s.ComponentID, dep.Kind, dep.Name)
		stamp := previewHash(dep)
		meta := p.metadata(s, name, ns)
		meta.Annotations[dependencyVersion] = stamp
		if dep.Kind == "Secret" {
			got, e := p.client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
			if e == nil {
				if err := p.owned(got, s.OwnershipToken); err != nil {
					return err
				}

				if got.Immutable == nil || !*got.Immutable || got.Annotations[dependencyVersion] != stamp {
					return fmt.Errorf("preview dependency identity changed; recreate composition")
				}

				continue
			}

			if !apierrors.IsNotFound(e) {
				return fmt.Errorf("cannot inspect destination Secret; check preview namespace RBAC")
			}

			source, e := p.client.CoreV1().Secrets(s.Preview.Source.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
			if e != nil || !sameDependency(source, dep) {
				return fmt.Errorf("Secret %s changed or is unavailable; recreate composition", dep.Name)
			}

			if source.Type == corev1.SecretTypeServiceAccountToken || source.Annotations[corev1.ServiceAccountNameKey] != "" {
				return fmt.Errorf("service-account credentials cannot be copied")
			}

			secrets = append(secrets, &corev1.Secret{ObjectMeta: meta, Type: source.Type, Data: source.Data, Immutable: new(true)})
		} else if dep.Kind == "ConfigMap" {
			got, e := p.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
			if e == nil {
				if err := p.owned(got, s.OwnershipToken); err != nil {
					return err
				}

				if got.Immutable == nil || !*got.Immutable || got.Annotations[dependencyVersion] != stamp {
					return fmt.Errorf("preview dependency identity changed; recreate composition")
				}

				continue
			}

			if !apierrors.IsNotFound(e) {
				return fmt.Errorf("cannot inspect destination ConfigMap; check preview namespace RBAC")
			}

			source, e := p.client.CoreV1().ConfigMaps(s.Preview.Source.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
			if e != nil || !sameDependency(source, dep) {
				return fmt.Errorf("ConfigMap %s changed or is unavailable; recreate composition", dep.Name)
			}

			for key, value := range s.Preview.Selection.ConfigMapKeys[dep.Name] {
				if source.Data == nil {
					source.Data = map[string]string{}
				}

				source.Data[key] = value
			}

			configs = append(configs, &corev1.ConfigMap{ObjectMeta: meta, Data: source.Data, BinaryData: source.BinaryData, Immutable: new(true)})
		} else {
			return fmt.Errorf("unsupported persisted dependency kind")
		}
	}

	for _, secret := range secrets {
		if err := p.writable(ctx); err != nil {
			return err
		}

		if _, err := p.client.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("cannot create approved preview Secret; check namespace RBAC and retry")
		}
	}

	for _, config := range configs {
		if err := p.writable(ctx); err != nil {
			return err
		}

		if _, err := p.client.CoreV1().ConfigMaps(ns).Create(ctx, config, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("cannot create approved preview ConfigMap; check namespace RBAC and retry")
		}
	}

	return nil
}

func previewTarget(s domain.WorkloadSpec) (int32, error) {
	t, err := decodePreview(s.Preview)
	if err != nil {
		return 0, err
	}

	port, err := strconv.ParseInt(t.Annotations["envy.dev/target-port"], 10, 32)
	if err != nil || port < 1024 || port > 65535 {
		return 0, fmt.Errorf("invalid preview target port")
	}

	return int32(port), nil
}

// Two complete Pod budgets account for a rolling update, plus two mesh sidecars.
// Legacy entries retain their old per-component budget during mixed operation.
func previewQuota(s domain.WorkloadSpec) (corev1.ResourceList, error) {
	hard := corev1.ResourceList{}
	add := func(key corev1.ResourceName, q resource.Quantity) { v := hard[key]; v.Add(q); hard[key] = v }
	legacy := max(s.WorkloadCount-len(s.Previews), 0)
	hard[corev1.ResourcePods] = resource.MustParse(fmt.Sprint(2*s.WorkloadCount + 2))
	for _, entry := range []struct {
		key corev1.ResourceName
		per string
	}{{corev1.ResourceRequestsCPU, "1"}, {corev1.ResourceRequestsMemory, "512Mi"}, {corev1.ResourceLimitsCPU, "3"}, {corev1.ResourceLimitsMemory, "2Gi"}} {
		q := resource.MustParse(entry.per)
		q.Mul(int64(legacy))
		hard[entry.key] = q
	}

	for _, snapshot := range s.Previews {
		t, err := decodePreview(&snapshot)
		if err != nil {
			return nil, err
		}

		r := previewPodResources(t.Spec)
		for _, entry := range []struct {
			key   corev1.ResourceName
			value resource.Quantity
			mesh  string
		}{{corev1.ResourceRequestsCPU, *r.Requests.Cpu(), "100m"}, {corev1.ResourceRequestsMemory, *r.Requests.Memory(), "128Mi"}, {corev1.ResourceLimitsCPU, *r.Limits.Cpu(), "2"}, {corev1.ResourceLimitsMemory, *r.Limits.Memory(), "1Gi"}} {
			q := entry.value.DeepCopy()
			mesh := snapshot.MeshBudget[string(entry.key)]
			if mesh == "" {
				mesh = entry.mesh
			}

			meshQuantity, err := resource.ParseQuantity(mesh)
			if err != nil || meshQuantity.Sign() <= 0 {
				return nil, fmt.Errorf("invalid captured mesh quota")
			}

			q.Add(meshQuantity)
			q.Mul(2)
			add(entry.key, q)
		}
	}

	return hard, nil
}

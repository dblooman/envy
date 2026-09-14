package kubernetes

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const dependencyVersion = "envy.dev/source-version"

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

// Two application replicas account for a rolling update, plus two mesh sidecars.
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
		r := t.Spec.Containers[0].Resources
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

package kubernetes

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/util/validation"
)

type PreviewPolicy struct {
	Composite                map[string]domain.CompositePreviewPolicy `json:"composite,omitempty"`
	MaxCPU                   string                                   `json:"max_cpu"`
	MaxMemory                string                                   `json:"max_memory"`
	MeshRequestCPU           string                                   `json:"mesh_request_cpu"`
	MeshRequestMemory        string                                   `json:"mesh_request_memory"`
	MeshLimitCPU             string                                   `json:"mesh_limit_cpu"`
	MeshLimitMemory          string                                   `json:"mesh_limit_memory"`
	ControllerNamespace      string                                   `json:"controller_namespace"`
	ControllerServiceAccount string                                   `json:"controller_service_account"`
	DependencyClusterRole    string                                   `json:"dependency_cluster_role"`
}

func (policy PreviewPolicy) Defaults() PreviewPolicy {
	for _, entry := range []struct {
		p     *string
		value string
	}{{&policy.MaxCPU, "2"}, {&policy.MaxMemory, "2Gi"}, {&policy.MeshRequestCPU, "100m"}, {&policy.MeshRequestMemory, "128Mi"}, {&policy.MeshLimitCPU, "2"}, {&policy.MeshLimitMemory, "1Gi"}} {
		if *entry.p == "" {
			*entry.p = entry.value
		}
	}

	return policy
}

func (policy PreviewPolicy) Validate() error {
	policy = policy.Defaults()
	for _, value := range []string{policy.MaxCPU, policy.MaxMemory, policy.MeshRequestCPU, policy.MeshRequestMemory, policy.MeshLimitCPU, policy.MeshLimitMemory} {
		q, e := resource.ParseQuantity(value)
		if e != nil || q.Sign() <= 0 {
			return fmt.Errorf("preview resource policy requires positive Kubernetes quantities")
		}
	}

	for _, pair := range [][2]string{{policy.MeshRequestCPU, policy.MeshLimitCPU}, {policy.MeshRequestMemory, policy.MeshLimitMemory}} {
		a := resource.MustParse(pair[0])
		if a.Cmp(resource.MustParse(pair[1])) > 0 {
			return fmt.Errorf("preview mesh requests exceed limits")
		}
	}

	if policy.ControllerNamespace != "" || policy.ControllerServiceAccount != "" || policy.DependencyClusterRole != "" {
		for _, id := range []string{policy.ControllerNamespace, policy.ControllerServiceAccount, policy.DependencyClusterRole} {
			if !domain.ValidCatalogID(id) {
				return fmt.Errorf("preview namespace, service account and dependency role must all be specified as DNS labels")
			}
		}
	}
	if len(policy.Composite) > 64 {
		return fmt.Errorf("at most 64 composite preview policies are supported")
	}

	for key, composite := range policy.Composite {
		parts := strings.Split(key, "/")
		if len(parts) != 3 || !domain.ValidCatalogID(parts[0]) || !domain.ValidCatalogID(parts[1]) || !domain.ValidCatalogID(parts[2]) {
			return fmt.Errorf("composite policy key must be project/baseline/component")
		}

		if err := validateCompositePolicy(composite); err != nil {
			return fmt.Errorf("composite policy %s: %w", key, err)
		}
	}

	return nil
}

func validateCompositePolicy(policy domain.CompositePreviewPolicy) error {
	if policy.Revision < 1 {
		return fmt.Errorf("revision must be positive")
	}

	if !domain.ValidCatalogID(policy.SourceServiceAccount) || !domain.ValidCatalogID(policy.ServiceAccount) || policy.ServiceAccount == "default" || strings.HasPrefix(policy.ServiceAccount, "envy-") {
		return fmt.Errorf("source and destination service accounts must be explicit DNS labels; destination must not be default or reserved envy- names")
	}

	if err := validateCompositeContainerNames(policy); err != nil {
		return err
	}

	for _, bound := range []string{policy.MaxPodCPU, policy.MaxPodMemory} {
		q, err := resource.ParseQuantity(bound)
		if err != nil || q.Sign() <= 0 {
			return fmt.Errorf("max_pod_cpu and max_pod_memory must be explicit positive quantities")
		}
	}

	if err := validateCompositeDependencies(policy.SharedDependencies); err != nil {
		return err
	}

	return validateCompositeAnnotations(policy.ServiceAccountAnnotations)
}

func validateCompositeContainerNames(policy domain.CompositePreviewPolicy) error {
	names := append([]string{policy.ApplicationContainer}, policy.Sidecars...)
	names = append(names, policy.InitContainers...)
	names = append(names, policy.NativeSidecars...)
	if len(names) > 16 {
		return fmt.Errorf("at most sixteen declared containers are supported")
	}

	seen := map[string]bool{}
	for _, name := range names {
		if !domain.ValidCatalogID(name) || seen[name] || name == "istio-proxy" || strings.HasPrefix(name, "linkerd-") || name == "istio-init" {
			return fmt.Errorf("container names must be unique non-mesh DNS labels")
		}

		seen[name] = true
	}

	return nil
}

func validateCompositeDependencies(dependencies []string) error {
	if len(dependencies) == 0 || len(dependencies) > 32 {
		return fmt.Errorf("shared_dependencies must describe one to 32 approved dependencies or the absence of external dependencies")
	}

	for _, dependency := range dependencies {
		if strings.TrimSpace(dependency) == "" || len(dependency) > 512 || strings.ContainsAny(dependency, "\x00\r\n") {
			return fmt.Errorf("shared dependency descriptions must be nonempty single lines of at most 512 bytes")
		}
	}

	return nil
}

func validateCompositeAnnotations(annotations map[string]string) error {
	total := 0
	if len(annotations) > 16 {
		return fmt.Errorf("at most sixteen service account annotations are supported")
	}

	for key, value := range annotations {
		total += len(key) + len(value)
		if len(validation.IsQualifiedName(key)) != 0 || strings.HasPrefix(key, "envy.dev/") || strings.HasPrefix(key, "kubernetes.io/") || strings.HasPrefix(key, "kubectl.kubernetes.io/") || strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("service account annotations contain invalid or reserved metadata")
		}
	}

	if total > 8192 {
		return fmt.Errorf("service account annotations exceed 8 KiB")
	}

	return nil
}

func (p *Provider) WithPreviewPolicy(policy PreviewPolicy) *Provider {
	p.previewPolicy = policy.Defaults()
	return p
}

func (p *Provider) ensureDependencyAccess(ctx context.Context, s domain.WorkloadSpec, ns string) error {
	policy := p.previewPolicy.Defaults()
	// Local kubeconfig callers can provision destination permissions themselves.
	if policy.DependencyClusterRole == "" {
		return nil
	}

	want := &rbacv1.RoleBinding{ObjectMeta: p.metadata(s, "envy-dependencies", ns), RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: policy.DependencyClusterRole}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: policy.ControllerServiceAccount, Namespace: policy.ControllerNamespace}}}
	api := p.client.RbacV1().RoleBindings(ns)
	got, err := api.Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return err
		}

		_, err = api.Create(ctx, want, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("cannot bind preview dependency permissions")
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("cannot inspect preview dependency RoleBinding")
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return err
	}

	if !reflect.DeepEqual(got.RoleRef, want.RoleRef) || !reflect.DeepEqual(got.Subjects, want.Subjects) {
		return fmt.Errorf("preview dependency RoleBinding conflict")
	}

	return nil
}

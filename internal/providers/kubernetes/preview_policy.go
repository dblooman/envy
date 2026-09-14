package kubernetes

import (
	"context"
	"fmt"
	"github.com/dblooman/envy/internal/domain"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
)

type PreviewPolicy struct {
	MaxCPU                   string `json:"max_cpu"`
	MaxMemory                string `json:"max_memory"`
	MeshRequestCPU           string `json:"mesh_request_cpu"`
	MeshRequestMemory        string `json:"mesh_request_memory"`
	MeshLimitCPU             string `json:"mesh_limit_cpu"`
	MeshLimitMemory          string `json:"mesh_limit_memory"`
	ControllerNamespace      string `json:"controller_namespace"`
	ControllerServiceAccount string `json:"controller_service_account"`
	DependencyClusterRole    string `json:"dependency_cluster_role"`
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

// Package kubernetes manages only composition-owned workload resources.
package kubernetes

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/mesh"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	kube "k8s.io/client-go/kubernetes"
)

const (
	InstallationLabel   = "envy.dev/installation"
	CompositionLabel    = "envy.dev/composition"
	ComponentLabel      = "envy.dev/component"
	OwnershipAnnotation = "envy.dev/ownership-token"
)

type Provider struct {
	previewPolicy       PreviewPolicy
	approvedPullSecrets []string
	client              kube.Interface
	installation        string
	guard               func(context.Context) error
	injection           map[string]string
	podAnnotations      map[string]string
	mesh                string
}

func New(client kube.Interface, installation string, guard func(context.Context) error) *Provider {
	return NewWithInjection(client, installation, guard, nil)
}

func NewWithInjection(client kube.Interface, installation string, guard func(context.Context) error, labels map[string]string) *Provider {
	if labels == nil {
		labels = map[string]string{"istio-injection": "enabled"}
	}

	copy := map[string]string{}
	maps.Copy(copy, labels)
	return &Provider{mesh: "istio", client: client, installation: installation, guard: guard, injection: copy}
}

// WithMesh separates mesh participation from Kubernetes application readiness.
func (p *Provider) WithMesh(name string) *Provider {
	p.mesh = name
	if name == "linkerd" {
		// Linkerd's default injector leaves resources unset, which cannot be
		// admitted into an Envy namespace with CPU and memory quotas. The init
		// container inherits the proxy resources in the supported profile.
		p.WithPodAnnotations(map[string]string{
			"linkerd.io/inject":                      "enabled",
			"config.linkerd.io/proxy-cpu-request":    "100m",
			"config.linkerd.io/proxy-cpu-limit":      "1",
			"config.linkerd.io/proxy-memory-request": "64Mi",
			"config.linkerd.io/proxy-memory-limit":   "256Mi",
		})
	}

	return p
}

// WithPodAnnotations configures annotations added to workload Pods (e.g. linkerd.io/inject: enabled).
func (p *Provider) WithPodAnnotations(annotations map[string]string) *Provider {
	if annotations == nil {
		p.podAnnotations = nil
		return p
	}

	copy := map[string]string{}
	maps.Copy(copy, annotations)
	p.podAnnotations = copy
	return p
}

// WithApprovedPullSecrets configures the operator policy before serving requests.
func (p *Provider) WithApprovedPullSecrets(names []string) *Provider {
	p.approvedPullSecrets = slices.Clone(names)
	return p
}
func Namespace(id string) string { return "envy-" + strings.ReplaceAll(id, "_", "-") }
func (p *Provider) writable(ctx context.Context) error {
	if p.guard == nil {
		return fmt.Errorf("provider mutation requires leadership guard")
	}

	return p.guard(ctx)
}

func (p *Provider) owned(m metav1.Object, token string) error {
	if token == "" || m.GetLabels()[InstallationLabel] != p.installation || m.GetAnnotations()[OwnershipAnnotation] != token {
		return fmt.Errorf("ownership conflict for %s/%s", m.GetNamespace(), m.GetName())
	}

	return nil
}

func (p *Provider) metadata(s domain.WorkloadSpec, name, ns string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{InstallationLabel: p.installation, CompositionLabel: s.CompositionID, ComponentLabel: s.ComponentID}, Annotations: map[string]string{OwnershipAnnotation: s.OwnershipToken}}
}

func (p *Provider) Ensure(ctx context.Context, s domain.WorkloadSpec) (domain.WorkloadRef, error) {
	if s.CompositionID == "" || !domain.ValidCatalogID(s.ComponentID) || s.OwnershipToken == "" || s.Image == "" {
		return domain.WorkloadRef{}, fmt.Errorf("invalid or unsupported workload specification")
	}

	if s.Profile.Port < 1 || s.Profile.Port > 65535 || (s.Preview == nil && (s.Profile.Port < 1024 || s.Profile.Profile != "http-small" || s.Profile.HealthPath == "" || s.Profile.ReadinessPath == "")) {
		return domain.WorkloadRef{}, fmt.Errorf("missing or unsupported approved workload profile")
	}

	if s.WorkloadCount == 0 {
		s.WorkloadCount = 1
	}

	if s.WorkloadCount < 1 || s.WorkloadCount > domain.MaxOverrides {
		return domain.WorkloadRef{}, fmt.Errorf("invalid workload count")
	}

	for _, name := range s.Profile.ImagePullSecrets {
		if !slices.Contains(p.approvedPullSecrets, name) {
			return domain.WorkloadRef{}, fmt.Errorf("image pull Secret is not operator-approved: %s", name)
		}
	}

	ns := Namespace(s.CompositionID)
	meta := p.metadata(s, ns, "")
	maps.Copy(meta.Labels, p.injection)
	wantNS := &corev1.Namespace{ObjectMeta: meta}
	currentNS, err := p.client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return domain.WorkloadRef{}, err
		}

		currentNS, err = p.client.CoreV1().Namespaces().Create(ctx, wantNS, metav1.CreateOptions{})
	} else if err == nil {
		err = p.owned(currentNS, s.OwnershipToken)
	}

	if err != nil {
		return domain.WorkloadRef{}, fmt.Errorf("ensure namespace: %w", err)
	}

	if currentNS.DeletionTimestamp != nil {
		return domain.WorkloadRef{}, fmt.Errorf("namespace is terminating")
	}

	injectionChanged := false
	for key, value := range p.injection {
		if currentNS.Labels[key] != value {
			currentNS.Labels[key] = value
			injectionChanged = true
		}
	}

	if injectionChanged {
		if err = p.writable(ctx); err != nil {
			return domain.WorkloadRef{}, err
		}

		if _, err = p.client.CoreV1().Namespaces().Update(ctx, currentNS, metav1.UpdateOptions{}); err != nil {
			return domain.WorkloadRef{}, err
		}
	}

	ref := domain.WorkloadRef{Namespace: ns, NamespaceUID: string(currentNS.UID), Deployment: s.ComponentID, Service: s.ComponentID, OwnershipToken: s.OwnershipToken}
	if err = p.ensureQuota(ctx, s, ns); err != nil {
		return ref, err
	}

	if err = p.ensureAccount(ctx, s, ns); err != nil {
		return ref, err
	}

	if s.Preview != nil {
		if err := p.ensureDependencyAccess(ctx, s, ns); err != nil {
			return ref, err
		}

		if err := p.ensurePreviewDependencies(ctx, s, ns); err != nil {
			return ref, err
		}
	}

	service, err := p.ensureService(ctx, s, ns)
	if err != nil {
		return ref, err
	}

	ref.ServiceUID = string(service.UID)
	deployment, err := p.ensureDeployment(ctx, s, ns)
	if err != nil {
		return ref, err
	}

	ref.DeploymentUID = string(deployment.UID)
	return ref, nil
}

func (p *Provider) ensureQuota(ctx context.Context, s domain.WorkloadSpec, ns string) error {
	// Leave room for a mesh sidecar alongside each application container.
	want := &corev1.ResourceQuota{ObjectMeta: p.metadata(s, "envy-quota", ns), Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourcePods: resource.MustParse(fmt.Sprint(2*s.WorkloadCount + 2)), corev1.ResourceRequestsCPU: resource.MustParse(fmt.Sprint(s.WorkloadCount)), corev1.ResourceRequestsMemory: resource.MustParse(fmt.Sprintf("%dMi", 512*s.WorkloadCount)), corev1.ResourceLimitsCPU: resource.MustParse(fmt.Sprint(3 * s.WorkloadCount)), corev1.ResourceLimitsMemory: resource.MustParse(fmt.Sprintf("%dGi", 2*s.WorkloadCount))}}}
	if len(s.Previews) > 0 {
		hard, err := previewQuota(s)
		if err != nil {
			return err
		}

		want.Spec.Hard = hard
	}

	api := p.client.CoreV1().ResourceQuotas(ns)
	got, err := api.Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return err
		}

		_, err = api.Create(ctx, want, metav1.CreateOptions{})
		return err
	}

	if err != nil {
		return err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(got.Spec, want.Spec) {
		got.Spec = want.Spec
		if err = p.writable(ctx); err != nil {
			return err
		}

		_, err = api.Update(ctx, got, metav1.UpdateOptions{})
	}

	return err
}

func (p *Provider) ensureAccount(ctx context.Context, s domain.WorkloadSpec, ns string) error {
	value := false
	want := &corev1.ServiceAccount{ObjectMeta: p.metadata(s, "envy-workload", ns), AutomountServiceAccountToken: &value}
	api := p.client.CoreV1().ServiceAccounts(ns)
	got, err := api.Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return err
		}

		_, err = api.Create(ctx, want, metav1.CreateOptions{})
		return err
	}

	if err != nil {
		return err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return err
	}

	if got.AutomountServiceAccountToken == nil || *got.AutomountServiceAccountToken {
		got.AutomountServiceAccountToken = &value
		if err = p.writable(ctx); err != nil {
			return err
		}

		_, err = api.Update(ctx, got, metav1.UpdateOptions{})
	}

	return err
}

func (p *Provider) ensureService(ctx context.Context, s domain.WorkloadSpec, ns string) (*corev1.Service, error) {
	want := &corev1.Service{ObjectMeta: p.metadata(s, s.ComponentID, ns), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: map[string]string{InstallationLabel: p.installation, CompositionLabel: s.CompositionID, ComponentLabel: s.ComponentID}, Ports: []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: s.Profile.Port, TargetPort: intstr.FromInt32(s.Profile.Port), AppProtocol: new("http")}}}}
	if s.Preview != nil {
		port, err := previewTarget(s)
		if err != nil {
			return nil, err
		}

		want.Spec.Ports[0].TargetPort = intstr.FromInt32(port)
	}

	api := p.client.CoreV1().Services(ns)
	got, err := api.Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return nil, err
		}

		return api.Create(ctx, want, metav1.CreateOptions{})
	}

	if err != nil {
		return nil, err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return nil, err
	}

	if !equality.Semantic.DeepEqual(got.Spec.Selector, want.Spec.Selector) || !equality.Semantic.DeepEqual(got.Spec.Ports, want.Spec.Ports) || got.Spec.Type != want.Spec.Type {
		got.Spec.Selector = want.Spec.Selector
		got.Spec.Ports = want.Spec.Ports
		got.Spec.Type = want.Spec.Type
		if err = p.writable(ctx); err != nil {
			return nil, err
		}

		return api.Update(ctx, got, metav1.UpdateOptions{})
	}

	return got, nil
}

func (p *Provider) ensureDeployment(ctx context.Context, s domain.WorkloadSpec, ns string) (*appsv1.Deployment, error) {
	meta := p.metadata(s, s.ComponentID, ns)
	var templateAnnotations map[string]string
	if len(p.podAnnotations) > 0 {
		templateAnnotations = map[string]string{}
		maps.Copy(templateAnnotations, p.podAnnotations)
	}

	want := &appsv1.Deployment{ObjectMeta: meta, Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: meta.Labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: meta.Labels, Annotations: templateAnnotations}, Spec: corev1.PodSpec{ServiceAccountName: "envy-workload", AutomountServiceAccountToken: new(false), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: new(true), RunAsUser: new(int64(65532)), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{{Name: s.ComponentID, Image: s.Image, ImagePullPolicy: corev1.PullIfNotPresent, Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: s.Profile.Port, Protocol: corev1.ProtocolTCP}}, Env: []corev1.EnvVar{{Name: "ENVY_COMPOSITION_ID", Value: s.CompositionID}, {Name: "POD_UID", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"}}}}, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: new(false), ReadOnlyRootFilesystem: new(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("64Mi")}}, ReadinessProbe: &corev1.Probe{HTTPGet: &corev1.HTTPGetAction{Path: s.Profile.ReadinessPath, Port: intstr.FromString("http")}, PeriodSeconds: 2}, LivenessProbe: &corev1.Probe{HTTPGet: &corev1.HTTPGetAction{Path: s.Profile.HealthPath, Port: intstr.FromString("http")}, PeriodSeconds: 10}}}}}}}
	for _, name := range s.Profile.ImagePullSecrets {
		want.Spec.Template.Spec.ImagePullSecrets = append(want.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
	}

	keys := make([]string, 0, len(s.Profile.Env))
	for key := range s.Profile.Env {
		keys = append(keys, key)
	}

	sort.Strings(keys)
	for _, key := range keys {
		want.Spec.Template.Spec.Containers[0].Env = append(want.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: key, Value: s.Profile.Env[key]})
	}

	if s.Preview != nil {
		template, err := decodePreview(s.Preview)
		if err != nil {
			return nil, err
		}

		rewritePreview(&template, s.ComponentID)
		if template.Labels == nil {
			template.Labels = map[string]string{}
		}

		maps.Copy(template.Labels, meta.Labels)
		template.Annotations = nil
		template.Spec.Containers[0].Image = s.Image
		template.Spec.Containers[0].Env = append(template.Spec.Containers[0].Env, corev1.EnvVar{Name: "ENVY_COMPOSITION_ID", Value: s.CompositionID}, corev1.EnvVar{Name: "POD_UID", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"}}})
		want.Spec.Template = template
		if len(p.podAnnotations) > 0 {
			if want.Spec.Template.Annotations == nil {
				want.Spec.Template.Annotations = map[string]string{}
			}

			maps.Copy(want.Spec.Template.Annotations, p.podAnnotations)
		}
	}

	// Explicit env wins over EnvFrom and replaces any copied baseline binding/ValueFrom.
	envKeys := make([]string, 0, len(s.MessagingEnv))
	for key := range s.MessagingEnv {
		envKeys = append(envKeys, key)
	}

	sort.Strings(envKeys)
	container := &want.Spec.Template.Spec.Containers[0]
	for _, key := range envKeys {
		filtered := container.Env[:0]
		for _, entry := range container.Env {
			if entry.Name != key {
				filtered = append(filtered, entry)
			}
		}

		filtered = append(filtered, corev1.EnvVar{Name: key, Value: s.MessagingEnv[key]})
		container.Env = filtered
	}

	api := p.client.AppsV1().Deployments(ns)
	got, err := api.Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return nil, err
		}

		return api.Create(ctx, want, metav1.CreateOptions{})
	}

	if err != nil {
		return nil, err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return nil, err
	}

	if !equality.Semantic.DeepDerivative(want.Spec, got.Spec) {
		got.Spec.Replicas = want.Spec.Replicas
		got.Spec.Template = want.Spec.Template
		if err = p.writable(ctx); err != nil {
			return nil, err
		}

		return api.Update(ctx, got, metav1.UpdateOptions{})
	}

	return got, nil
}

func (p *Provider) Observe(ctx context.Context, ref domain.WorkloadRef) (domain.WorkloadObservation, error) {
	ns, err := p.client.CoreV1().Namespaces().Get(ctx, ref.Namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return domain.WorkloadObservation{Message: "namespace absent"}, nil
	}

	if err != nil {
		return domain.WorkloadObservation{}, err
	}

	if err = p.owned(ns, ref.OwnershipToken); err != nil {
		return domain.WorkloadObservation{}, err
	}

	if string(ns.UID) != ref.NamespaceUID {
		return domain.WorkloadObservation{}, fmt.Errorf("namespace identity changed")
	}

	d, err := p.client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return domain.WorkloadObservation{Message: "deployment absent"}, nil
	}

	if err != nil {
		return domain.WorkloadObservation{}, err
	}

	if err = p.owned(d, ref.OwnershipToken); err != nil {
		return domain.WorkloadObservation{}, err
	}

	if string(d.UID) != ref.DeploymentUID {
		return domain.WorkloadObservation{}, fmt.Errorf("deployment identity changed")
	}

	s, err := p.client.CoreV1().Services(ref.Namespace).Get(ctx, ref.Service, metav1.GetOptions{})
	if err != nil {
		return domain.WorkloadObservation{}, err
	}

	if err = p.owned(s, ref.OwnershipToken); err != nil {
		return domain.WorkloadObservation{}, err
	}

	if string(s.UID) != ref.ServiceUID {
		return domain.WorkloadObservation{}, fmt.Errorf("service identity changed")
	}

	obs := domain.WorkloadObservation{Image: d.Spec.Template.Spec.Containers[0].Image, Message: "waiting for deployment and endpoints"}
	pods, err := p.client.CoreV1().Pods(ref.Namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(d.Spec.Selector)})
	if err != nil {
		return obs, err
	}

	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil || !podImageMatches(pod, ref.Deployment, obs.Image) {
			continue
		}

		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != ref.Deployment {
				continue
			}

			if status.State.Waiting != nil {
				obs.Message = status.State.Waiting.Reason + ": " + status.State.Waiting.Message
				switch status.State.Waiting.Reason {
				case "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "CreateContainerConfigError":
					obs.Failed = true
				}
			}
		}
	}

	if d.Status.ObservedGeneration < d.Generation || d.Status.ReadyReplicas != 1 || d.Status.UpdatedReplicas != 1 || d.Status.Replicas != 1 {
		return obs, nil
	}

	slices, err := p.client.DiscoveryV1().EndpointSlices(ref.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "kubernetes.io/service-name=" + ref.Service})
	if err != nil {
		return obs, err
	}

	for _, slice := range slices.Items {
		for _, ep := range slice.Endpoints {
			if ep.Conditions.Ready == nil || !*ep.Conditions.Ready || ep.TargetRef == nil {
				continue
			}

			for _, pod := range pods.Items {
				if pod.DeletionTimestamp == nil && pod.UID == ep.TargetRef.UID && podImageMatches(pod, ref.Deployment, obs.Image) {
					profile, err := mesh.Resolve(p.mesh)
					if err != nil {
						return obs, err
					}

					proxyReady := profile.ProxyContainer == ""
					for _, status := range append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...) {
						if status.Name == profile.ProxyContainer && status.Ready {
							proxyReady = true
						}
					}

					if !proxyReady {
						obs.Message = "application endpoints are ready; waiting for " + profile.ProxyContainer + " mesh participation"
						continue
					}

					obs.Ready = true
					obs.Failed = false
					obs.Message = "deployment and endpoints ready"
					obs.WorkloadID = string(pod.UID)
					return obs, nil
				}
			}
		}
	}

	return obs, nil
}

func (p *Provider) Delete(ctx context.Context, ref domain.WorkloadRef) error {
	if ref.Namespace == "" || ref.OwnershipToken == "" {
		return fmt.Errorf("cannot delete without namespace and persisted ownership token")
	}

	ns, err := p.client.CoreV1().Namespaces().Get(ctx, ref.Namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	if err = p.owned(ns, ref.OwnershipToken); err != nil {
		return err
	}

	if ref.NamespaceUID != "" && string(ns.UID) != ref.NamespaceUID {
		return fmt.Errorf("refuse namespace deletion: identity changed")
	}

	if ns.DeletionTimestamp != nil {
		return nil
	}

	if err = p.writable(ctx); err != nil {
		return err
	}

	uid := types.UID(ns.UID)
	err = p.client.CoreV1().Namespaces().Delete(ctx, ref.Namespace, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if apierrors.IsNotFound(err) {
		return nil
	}

	return err
}

// DeleteWorkload retires one composition-owned Deployment and Service while
// keeping its namespace, account, and quota available for other overrides.
func (p *Provider) DeleteWorkload(ctx context.Context, ref domain.WorkloadRef) error {
	if ref.Namespace == "" || ref.Deployment == "" || ref.Service == "" || ref.OwnershipToken == "" {
		return fmt.Errorf("cannot delete workload without persisted identity and ownership token")
	}

	deployment, err := p.client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	if err == nil {
		if err = p.owned(deployment, ref.OwnershipToken); err != nil {
			return err
		}

		if ref.DeploymentUID != "" && string(deployment.UID) != ref.DeploymentUID {
			return fmt.Errorf("refuse deployment deletion: identity changed")
		}

		if err = p.writable(ctx); err != nil {
			return err
		}

		uid := types.UID(deployment.UID)
		if err = p.client.AppsV1().Deployments(ref.Namespace).Delete(ctx, ref.Deployment, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	service, err := p.client.CoreV1().Services(ref.Namespace).Get(ctx, ref.Service, metav1.GetOptions{})
	if err == nil {
		if err = p.owned(service, ref.OwnershipToken); err != nil {
			return err
		}

		if ref.ServiceUID != "" && string(service.UID) != ref.ServiceUID {
			return fmt.Errorf("refuse service deletion: identity changed")
		}

		if err = p.writable(ctx); err != nil {
			return err
		}

		uid := types.UID(service.UID)
		if err = p.client.CoreV1().Services(ref.Namespace).Delete(ctx, ref.Service, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (p *Provider) WorkloadAbsent(ctx context.Context, ref domain.WorkloadRef) (bool, error) {
	if ref.Namespace == "" || ref.Deployment == "" || ref.Service == "" {
		return false, fmt.Errorf("invalid workload reference")
	}

	_, deploymentErr := p.client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Deployment, metav1.GetOptions{})
	_, serviceErr := p.client.CoreV1().Services(ref.Namespace).Get(ctx, ref.Service, metav1.GetOptions{})
	if deploymentErr != nil && !apierrors.IsNotFound(deploymentErr) {
		return false, deploymentErr
	}

	if serviceErr != nil && !apierrors.IsNotFound(serviceErr) {
		return false, serviceErr
	}

	return apierrors.IsNotFound(deploymentErr) && apierrors.IsNotFound(serviceErr), nil
}

// Absent confirms cleanup without treating observation failures as absence.
func (p *Provider) Absent(ctx context.Context, ref domain.WorkloadRef) (bool, error) {
	_, err := p.client.CoreV1().Namespaces().Get(ctx, ref.Namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}

	return false, err
}

func podImageMatches(pod corev1.Pod, component, image string) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == component {
			return container.Image == image
		}
	}

	return false
}

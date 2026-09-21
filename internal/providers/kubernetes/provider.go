// Package kubernetes manages only composition-owned workload resources.
//
//nolint:wsl_v5 // Provider methods retain explicit branch boundaries for ownership-sensitive mutations.
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
	"github.com/dblooman/envy/internal/providers/kubeapply"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	kube "k8s.io/client-go/kubernetes"
)

const (
	InstallationLabel   = "envy.dev/installation"
	CompositionLabel    = "envy.dev/composition"
	ComponentLabel      = "envy.dev/component"
	OwnershipAnnotation = "envy.dev/ownership-token"
)

type Provider struct {
	policyClient        dynamic.Interface
	observations        *Observations
	namespacePolicy     NamespacePolicy
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

func (p *Provider) sharedMetadata(s domain.WorkloadSpec, name, ns string) metav1.ObjectMeta {
	meta := p.metadata(s, name, ns)
	delete(meta.Labels, ComponentLabel)
	return meta
}

func (p *Provider) Ensure(ctx context.Context, s domain.WorkloadSpec) (domain.WorkloadRef, error) {
	if s.CompositionID == "" || !domain.ValidCatalogID(s.ComponentID) || s.OwnershipToken == "" || s.Image == "" {
		return domain.WorkloadRef{}, fmt.Errorf("invalid or unsupported workload specification")
	}
	if s.Profile.WorkloadKind() == domain.WorkloadJob {
		return p.ensureJob(ctx, s)
	}
	if s.Profile.WorkloadKind() == domain.WorkloadScheduledJob {
		return p.ensureScheduledJob(ctx, s)
	}
	if s.Profile.WorkloadKind() != domain.WorkloadHTTP {
		return domain.WorkloadRef{}, fmt.Errorf("workload kind %q is not supported by the Kubernetes provider", s.Profile.WorkloadKind())
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
	if err := p.validateCompositeRuntime(s); err != nil {
		return domain.WorkloadRef{}, err
	}

	for _, name := range s.Profile.ImagePullSecrets {
		if !slices.Contains(p.approvedPullSecrets, name) {
			return domain.WorkloadRef{}, fmt.Errorf("image pull Secret is not operator-approved: %s", name)
		}
	}

	if err := p.namespacePolicy.Ready(); err != nil {
		return domain.WorkloadRef{}, err
	}

	ns := Namespace(s.CompositionID)
	meta := p.sharedMetadata(s, ns, "")
	maps.Copy(meta.Labels, p.injection)
	maps.Copy(meta.Labels, p.namespacePolicy.Labels())
	wantNS := &corev1.Namespace{ObjectMeta: meta}
	currentNS, err := unchanged(p, wantNS, func() (*corev1.Namespace, error) {
		return p.client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	})
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return domain.WorkloadRef{}, err
		}

		kubeapply.Stamp(wantNS)
		currentNS, err = p.client.CoreV1().Namespaces().Create(ctx, wantNS, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
	} else if err == nil {
		err = p.owned(currentNS, s.OwnershipToken)
	}

	if err != nil {
		return domain.WorkloadRef{}, fmt.Errorf("ensure namespace: %w", err)
	}

	if currentNS.DeletionTimestamp != nil {
		return domain.WorkloadRef{}, fmt.Errorf("namespace is terminating")
	}

	for key, value := range meta.Labels {
		if strings.HasPrefix(key, "pod-security.kubernetes.io/") && currentNS.Labels[key] != "" && currentNS.Labels[key] != value {
			return domain.WorkloadRef{}, fmt.Errorf("namespace pod security label conflict: %s", key)
		}
	}
	if kubeapply.Changed(wantNS, currentNS) {
		if _, e := kubeapply.Apply(ctx, p.client.CoreV1().Namespaces(), wantNS, currentNS, "v1", "Namespace", kubeapply.RuntimeManager, p.writable); e != nil {
			return domain.WorkloadRef{}, e
		}
	}

	ref := domain.WorkloadRef{Namespace: ns, NamespaceUID: string(currentNS.UID), Deployment: s.ComponentID, Service: s.ComponentID, OwnershipToken: s.OwnershipToken}
	if err = p.ensureNetworkPolicy(ctx, s, ns); err != nil {
		return ref, err
	}

	if err = p.ensureCiliumIngress(ctx, s, ns); err != nil {
		return ref, err
	}

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
	ref.DeploymentGeneration = deployment.Generation
	ref.Image = s.Image
	if s.Preview != nil && s.Preview.CompositePolicy != nil {
		ref.ExecutionFingerprint = deployment.Annotations[compositeExecutionAnnotation]
	}

	return ref, nil
}

func (p *Provider) ensureQuota(ctx context.Context, s domain.WorkloadSpec, ns string) error {
	// Leave room for a mesh sidecar alongside each application container.
	want := &corev1.ResourceQuota{ObjectMeta: p.sharedMetadata(s, "envy-quota", ns), Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourcePods: resource.MustParse(fmt.Sprint(2*s.WorkloadCount + 2)), corev1.ResourceRequestsCPU: resource.MustParse(fmt.Sprint(s.WorkloadCount)), corev1.ResourceRequestsMemory: resource.MustParse(fmt.Sprintf("%dMi", 512*s.WorkloadCount)), corev1.ResourceLimitsCPU: resource.MustParse(fmt.Sprint(3 * s.WorkloadCount)), corev1.ResourceLimitsMemory: resource.MustParse(fmt.Sprintf("%dGi", 2*s.WorkloadCount))}}}
	if len(s.Previews) > 0 {
		hard, err := previewQuota(s)
		if err != nil {
			return err
		}

		want.Spec.Hard = hard
	}

	api := p.client.CoreV1().ResourceQuotas(ns)
	got, err := unchanged(p, want, func() (*corev1.ResourceQuota, error) { return api.Get(ctx, want.Name, metav1.GetOptions{}) })
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return err
		}

		kubeapply.Stamp(want)
		_, err = api.Create(ctx, want, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
		return err
	}

	if err != nil {
		return err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return err
	}

	if kubeapply.Changed(want, got) {
		_, err = kubeapply.Apply(ctx, api, want, got, "v1", "ResourceQuota", kubeapply.RuntimeManager, p.writable)
	}

	return err
}

func (p *Provider) ensureAccount(ctx context.Context, s domain.WorkloadSpec, ns string) error {
	value := false
	want := &corev1.ServiceAccount{ObjectMeta: p.sharedMetadata(s, "envy-workload", ns), AutomountServiceAccountToken: &value}
	composite := s.Preview != nil && s.Preview.CompositePolicy != nil
	if composite {
		policy := s.Preview.CompositePolicy
		want.ObjectMeta = p.metadata(s, policy.ServiceAccount, ns)
		maps.Copy(want.Annotations, policy.ServiceAccountAnnotations)
	}

	api := p.client.CoreV1().ServiceAccounts(ns)
	var got *corev1.ServiceAccount
	var err error
	if composite {
		// Identity annotations require exact comparison. The general informer
		// shortcut deliberately tolerates unrelated metadata and is unsuitable.
		got, err = api.Get(ctx, want.Name, metav1.GetOptions{})
	} else {
		got, err = unchanged(p, want, func() (*corev1.ServiceAccount, error) { return api.Get(ctx, want.Name, metav1.GetOptions{}) })
	}

	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return err
		}

		kubeapply.Stamp(want)
		_, err = api.Create(ctx, want, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
		return err
	}

	if err != nil {
		return err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return err
	}
	if composite {
		if got.Labels[ComponentLabel] != s.ComponentID || got.DeletionTimestamp != nil {
			return fmt.Errorf("composite service account ownership conflict")
		}

		kubeapply.Stamp(want)
		if !maps.Equal(want.Annotations, got.Annotations) || !maps.Equal(want.Labels, got.Labels) || got.AutomountServiceAccountToken == nil || *got.AutomountServiceAccountToken {
			// Remove unapproved identity annotations rather than applying a subset
			// that could retain an independently added cloud identity. ResourceVersion
			// makes this an optimistic update of the object whose ownership was checked.
			got.Annotations = want.Annotations
			got.Labels = want.Labels
			got.AutomountServiceAccountToken = want.AutomountServiceAccountToken
			if err = p.writable(ctx); err != nil {
				return err
			}

			_, err = api.Update(ctx, got, metav1.UpdateOptions{FieldManager: kubeapply.RuntimeManager})
		}

		return err
	}

	if kubeapply.Changed(want, got) {
		_, err = kubeapply.Apply(ctx, api, want, got, "v1", "ServiceAccount", kubeapply.RuntimeManager, p.writable)
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
	got, err := unchanged(p, want, func() (*corev1.Service, error) { return api.Get(ctx, want.Name, metav1.GetOptions{}) })
	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return nil, err
		}

		kubeapply.Stamp(want)
		return api.Create(ctx, want, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
	}

	if err != nil {
		return nil, err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return nil, err
	}

	if kubeapply.Changed(want, got) {
		return kubeapply.Apply(ctx, api, want, got, "v1", "Service", kubeapply.RuntimeManager, p.writable)
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
		app, err := previewApplication(&template, s.Preview)
		if err != nil {
			return nil, err
		}

		app.Image = s.Image
		app.Env = append(app.Env, corev1.EnvVar{Name: "ENVY_COMPOSITION_ID", Value: s.CompositionID}, corev1.EnvVar{Name: "POD_UID", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"}}})
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
	container, err := previewApplication(&want.Spec.Template, s.Preview)
	if err != nil {
		return nil, err
	}

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
	composite := s.Preview != nil && s.Preview.CompositePolicy != nil
	if composite {
		want.Annotations[compositeExecutionAnnotation] = compositeExecutionFingerprint(want.Spec.Template)
	}

	api := p.client.AppsV1().Deployments(ns)
	var got *appsv1.Deployment
	if composite {
		// The generic subset comparison permits additional containers and does
		// not preserve init ordering. Composite execution must be checked fresh.
		got, err = api.Get(ctx, want.Name, metav1.GetOptions{})
	} else {
		got, err = unchanged(p, want, func() (*appsv1.Deployment, error) { return api.Get(ctx, want.Name, metav1.GetOptions{}) })
	}

	if apierrors.IsNotFound(err) {
		if err = p.writable(ctx); err != nil {
			return nil, err
		}

		kubeapply.Stamp(want)
		got, err = api.Create(ctx, want, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
		return checkedCompositeDeployment(want, got, err)
	}

	if err != nil {
		return nil, err
	}

	if err = p.owned(got, s.OwnershipToken); err != nil {
		return nil, err
	}
	if composite && got.Annotations[compositeExecutionAnnotation] != compositeExecutionFingerprint(got.Spec.Template) {
		return nil, fmt.Errorf("composite execution drift detected; inspect the deployment and recreate the composition")
	}

	if kubeapply.Changed(want, got) {
		got, err = kubeapply.Apply(ctx, api, want, got, "apps/v1", "Deployment", kubeapply.RuntimeManager, p.writable)
		return checkedCompositeDeployment(want, got, err)
	}

	return checkedCompositeDeployment(want, got, nil)
}

func (p *Provider) Observe(ctx context.Context, ref domain.WorkloadRef) (domain.WorkloadObservation, error) {
	if ref.Kind == domain.WorkloadJob {
		return p.observeJob(ctx, ref)
	}
	if ref.Kind == domain.WorkloadScheduledJob {
		return p.observeScheduledJob(ctx, ref, ref.MaxRuns)
	}
	ns, err := p.observeNamespace(ctx, ref.Namespace)
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

	d, err := p.observeDeployment(ctx, ref.Namespace, ref.Deployment)
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

	s, err := p.observeService(ctx, ref.Namespace, ref.Service)
	if err != nil {
		return domain.WorkloadObservation{}, err
	}

	if err = p.owned(s, ref.OwnershipToken); err != nil {
		return domain.WorkloadObservation{}, err
	}

	if string(s.UID) != ref.ServiceUID {
		return domain.WorkloadObservation{}, fmt.Errorf("service identity changed")
	}

	app, err := previewApplication(&d.Spec.Template, &domain.PreviewSnapshot{ApplicationContainer: ref.Deployment})
	if err != nil {
		return domain.WorkloadObservation{Message: "waiting for application container"}, nil
	}

	if d.Generation < ref.DeploymentGeneration {
		return domain.WorkloadObservation{Message: "waiting for cache to observe desired workload", Image: ref.Image}, nil
	}

	if ref.ExecutionFingerprint != "" && ref.ExecutionFingerprint != compositeExecutionFingerprint(d.Spec.Template) {
		return domain.WorkloadObservation{Failed: true, Message: "composite execution drift detected; inspect the deployment and recreate the composition", Image: ref.Image}, nil
	}

	if ref.Image != "" && app.Image != ref.Image {
		return domain.WorkloadObservation{Message: "waiting for cache to observe desired workload", Image: ref.Image}, nil
	}

	obs := domain.WorkloadObservation{Image: app.Image, Message: "waiting for deployment and endpoints"}
	pods, err := p.observePods(ctx, ref.Namespace, d.Spec.Selector)
	if err != nil {
		return obs, err
	}

	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil || !podImageMatches(pod, ref.Deployment, obs.Image) {
			continue
		}

		if message, failed := previewPodFailure(pod); message != "" && (!obs.Failed || failed) {
			obs.Message = message
			obs.Failed = failed
		}
	}

	if d.Status.ObservedGeneration < d.Generation || d.Status.ReadyReplicas != 1 || d.Status.UpdatedReplicas != 1 || d.Status.Replicas != 1 {
		return obs, nil
	}

	slices, err := p.observeEndpoints(ctx, ref.Namespace, ref.Service)
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
					if message, failed := previewPodFailure(pod); failed {
						obs.Message, obs.Failed = message, true
						continue
					}

					if message := previewSupportReadiness(d.Spec.Template.Spec, pod.Status, ref.Deployment); message != "" {
						obs.Message = message
						continue
					}

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
	if ref.Kind == domain.WorkloadJob {
		return p.deleteJob(ctx, ref)
	}
	if ref.Kind == domain.WorkloadScheduledJob {
		return p.deleteCronJob(ctx, ref)
	}
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
	if ref.Kind == domain.WorkloadJob {
		if ref.Namespace == "" || ref.Job == "" {
			return false, fmt.Errorf("invalid Job workload reference")
		}
		_, err := p.client.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Job, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		return apierrors.IsNotFound(err), nil
	}
	if ref.Kind == domain.WorkloadScheduledJob {
		if ref.Namespace == "" || ref.CronJob == "" {
			return false, fmt.Errorf("invalid CronJob workload reference")
		}
		_, err := p.client.BatchV1().CronJobs(ref.Namespace).Get(ctx, ref.CronJob, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		return apierrors.IsNotFound(err), nil
	}
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

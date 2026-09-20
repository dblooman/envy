package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dblooman/envy/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func previewHash(v any) string { b, _ := json.Marshal(v); return fmt.Sprintf("%x", sha256.Sum256(b)) }

func previewReadError(kind, name string) error {
	return &domain.Error{Code: "unavailable", Message: "cannot inspect preview " + kind + " " + name + "; check source-read RBAC and resource existence", Retryable: true}
}

func (p *Provider) DiscoverPreview(ctx context.Context, b domain.Baseline, c domain.Component, sel domain.PreviewSelection) (domain.PreviewReport, error) {
	out := domain.PreviewReport{Blockers: []string{}, Warnings: []string{
		"Preview uses a separate namespace: confirm every service address and external dependency; short DNS names and file contents are not rewritten automatically.",
		"Only request routing is separated. Shared databases, caches and side effects remain shared. Secret rotation requires recreation of existing previews.",
		"Source tracking metadata is removed, the application container is renamed to the component ID, and Istio is injected again by the destination namespace.",
	}, Dependencies: []domain.PreviewDependency{}, SourceReadRules: []map[string]any{}}
	binding, ok := b.Components[c.ID]
	if !ok {
		return out, domain.Validation("component is not bound")
	}

	ns := b.Routing.Namespace
	serviceName, _, _ := strings.Cut(binding.ServiceHost, ".")
	svc, err := p.client.CoreV1().Services(ns).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return out, previewReadError("Service", serviceName)
	}

	if len(svc.Spec.Selector) == 0 {
		return out, domain.Validation("source Service must select pods")
	}

	var candidates []appsv1.Deployment
	if sel.Deployment != "" {
		if !domain.ValidCatalogID(sel.Deployment) {
			return out, domain.Validation("invalid Deployment name")
		}

		d, e := p.client.AppsV1().Deployments(ns).Get(ctx, sel.Deployment, metav1.GetOptions{})
		if e != nil {
			return out, previewReadError("Deployment", sel.Deployment)
		}

		candidates = []appsv1.Deployment{*d}
	} else {
		list, e := p.client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if e != nil {
			return out, previewReadError("Deployments", ns)
		}

		for _, d := range list.Items {
			if labels.SelectorFromSet(svc.Spec.Selector).Matches(labels.Set(d.Spec.Template.Labels)) {
				candidates = append(candidates, d)
			}
		}
	}

	if len(candidates) != 1 {
		return out, domain.Validation("Service must resolve to one Deployment; select deployment explicitly when ambiguous")
	}

	d := candidates[0]
	if !labels.SelectorFromSet(svc.Spec.Selector).Matches(labels.Set(d.Spec.Template.Labels)) || d.DeletionTimestamp != nil {
		return out, domain.Validation("selected Deployment does not serve the registered Service")
	}

	if d.Status.ObservedGeneration < d.Generation || d.Status.ReadyReplicas < 1 || d.Status.UpdatedReplicas != d.Status.Replicas {
		out.Blockers = append(out.Blockers, "source Deployment must have a completed, ready rollout")
	}

	template := d.Spec.Template.DeepCopy()
	spec := &template.Spec
	if len(spec.Containers) != 1 || len(spec.InitContainers) != 0 {
		out.Blockers = append(out.Blockers, "requires one application container and no declared init containers or sidecars; allow platform Istio injection on new Pods")
	}

	if len(spec.Containers) == 0 {
		return out, domain.Validation("Deployment has no application container")
	}

	app := &spec.Containers[0]
	if sel.Container != "" && sel.Container != app.Name {
		return out, domain.Validation("selected container is not the sole application container")
	}

	sel.Deployment = d.Name
	sel.Container = app.Name
	out.Source = domain.PreviewSource{Namespace: ns, Deployment: d.Name, UID: string(d.UID), ResourceVersion: d.ResourceVersion, Generation: d.Generation, Container: app.Name}
	// Fail closed on Pod features whose semantics cannot be preserved in a new namespace.
	allowed := map[string]bool{"containers": true, "volumes": true, "restartPolicy": true, "terminationGracePeriodSeconds": true, "dnsPolicy": true, "serviceAccountName": true, "serviceAccount": true, "automountServiceAccountToken": true, "securityContext": true, "imagePullSecrets": true, "schedulerName": true, "enableServiceLinks": true}
	raw, _ := json.Marshal(spec)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	for key := range fields {
		if !allowed[key] {
			out.Blockers = append(out.Blockers, "unsupported Pod setting: "+key)
		}
	}

	if spec.ServiceAccountName != "" && spec.ServiceAccountName != "default" {
		out.Blockers = append(out.Blockers, "application service-account or cloud identity requires an explicit future integration")
	}

	if spec.AutomountServiceAccountToken != nil && *spec.AutomountServiceAccountToken {
		out.Blockers = append(out.Blockers, "application Kubernetes API tokens are unsupported")
	}

	if spec.DNSPolicy != "" && spec.DNSPolicy != corev1.DNSClusterFirst {
		out.Blockers = append(out.Blockers, "only ClusterFirst DNS is supported")
	}

	if spec.SchedulerName != "" && spec.SchedulerName != "default-scheduler" {
		out.Blockers = append(out.Blockers, "custom schedulers are unsupported")
	}

	if spec.RestartPolicy != "" && spec.RestartPolicy != corev1.RestartPolicyAlways {
		out.Blockers = append(out.Blockers, "only Always restart policy is supported")
	}

	if len(template.Annotations) > 0 && !p.onlyLinkerdInjectionAnnotations(template.Annotations) {
		out.Blockers = append(out.Blockers, "Pod-template annotations require explicit integration; remove identity, injection, or external-controller annotations from the source template")
	}

	for key := range template.Labels {
		if strings.Contains(key, "identity") || strings.Contains(key, "iam") {
			out.Blockers = append(out.Blockers, "workload identity labels are unsupported")
		}
	}

	if app.Lifecycle != nil || len(app.VolumeDevices) > 0 || app.Stdin || app.TTY || app.RestartPolicy != nil || len(app.Resources.Claims) > 0 {
		out.Blockers = append(out.Blockers, "container lifecycle, device, interactive, restart-policy and resource-claim settings are unsupported")
	}

	sc := app.SecurityContext
	if sc != nil && ((sc.Privileged != nil && *sc.Privileged) || (sc.AllowPrivilegeEscalation != nil && *sc.AllowPrivilegeEscalation) || (sc.RunAsUser != nil && *sc.RunAsUser == 0) || (sc.Capabilities != nil && len(sc.Capabilities.Add) > 0) || sc.SELinuxOptions != nil || sc.WindowsOptions != nil || (sc.SeccompProfile != nil && sc.SeccompProfile.Type == corev1.SeccompProfileTypeUnconfined)) {
		out.Blockers = append(out.Blockers, "container security settings exceed preview policy")
	}

	psc := spec.SecurityContext
	if psc != nil && ((psc.RunAsUser != nil && *psc.RunAsUser == 0) || psc.SELinuxOptions != nil || psc.WindowsOptions != nil || len(psc.Sysctls) > 0 || (psc.SeccompProfile != nil && psc.SeccompProfile.Type == corev1.SeccompProfileTypeUnconfined)) {
		out.Blockers = append(out.Blockers, "Pod security settings exceed preview policy")
	}

	var user *int64
	var requireNonRoot *bool
	if psc != nil {
		user = psc.RunAsUser
		requireNonRoot = psc.RunAsNonRoot
	}

	if sc != nil {
		if sc.RunAsUser != nil {
			user = sc.RunAsUser
		}

		if sc.RunAsNonRoot != nil {
			requireNonRoot = sc.RunAsNonRoot
		}
	}

	nonroot := (user != nil && *user > 0) || (requireNonRoot != nil && *requireNonRoot)
	if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		out.Blockers = append(out.Blockers, "source must disable privilege escalation explicitly")
	}

	if sc != nil && sc.ProcMount != nil && *sc.ProcMount != corev1.DefaultProcMount {
		out.Blockers = append(out.Blockers, "unmasked proc mounts are unsupported")
	}

	if !nonroot {
		out.Blockers = append(out.Blockers, "source must explicitly declare a non-root application identity")
	}

	for _, port := range app.Ports {
		if port.HostPort != 0 {
			out.Blockers = append(out.Blockers, "host ports are unsupported")
		}
	}

	for _, r := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		request, rok := app.Resources.Requests[r]
		limit, lok := app.Resources.Limits[r]
		policy := p.previewPolicy.Defaults()
		maximum := resource.MustParse(policy.MaxCPU)
		if r == corev1.ResourceMemory {
			maximum = resource.MustParse(policy.MaxMemory)
		}

		if !rok || !lok || request.Sign() <= 0 || limit.Sign() <= 0 || request.Cmp(limit) > 0 || limit.Cmp(maximum) > 0 {
			out.Blockers = append(out.Blockers, "CPU/memory requests and limits must be positive, within installation maxima, with request <= limit")
		}
	}

	for r := range app.Resources.Limits {
		if r != corev1.ResourceCPU && r != corev1.ResourceMemory {
			out.Blockers = append(out.Blockers, "unsupported resource: "+string(r))
		}
	}

	for r := range app.Resources.Requests {
		if r != corev1.ResourceCPU && r != corev1.ResourceMemory {
			out.Blockers = append(out.Blockers, "unsupported resource request: "+string(r))
		}
	}

	if app.ReadinessProbe == nil {
		out.Blockers = append(out.Blockers, "source must declare a readiness probe")
	}

	// Preserve the registered Service port and discover its actual container target.
	target := int32(0)
	for _, port := range svc.Spec.Ports {
		if port.Port == binding.Port {
			target = port.TargetPort.IntVal
			if port.TargetPort.StrVal != "" {
				for _, cp := range app.Ports {
					if cp.Name == port.TargetPort.StrVal {
						target = cp.ContainerPort
					}
				}
			}
		}
	}

	if target < 1024 || target > 65535 {
		out.Blockers = append(out.Blockers, "source Service must target an unprivileged application port")
	}

	refs := map[string]bool{}
	add := func(kind, name string) { refs[kind+"/"+name] = true }
	for i := range app.Env {
		e := &app.Env[i]
		if e.Name == "POD_UID" || strings.HasPrefix(e.Name, "ENVY_") {
			out.Blockers = append(out.Blockers, "reserved application environment variable: "+e.Name)
		}

		if e.ValueFrom != nil {
			if e.ValueFrom.SecretKeyRef != nil {
				add("Secret", e.ValueFrom.SecretKeyRef.Name)
			}

			if e.ValueFrom.ConfigMapKeyRef != nil {
				add("ConfigMap", e.ValueFrom.ConfigMapKeyRef.Name)
			}

			if e.ValueFrom.ResourceFieldRef != nil && e.ValueFrom.ResourceFieldRef.ContainerName != "" {
				e.ValueFrom.ResourceFieldRef.ContainerName = c.ID
			}
		}
	}

	for _, e := range app.EnvFrom {
		if e.SecretRef != nil {
			add("Secret", e.SecretRef.Name)
		}

		if e.ConfigMapRef != nil {
			add("ConfigMap", e.ConfigMapRef.Name)
		}
	}

	for _, v := range spec.Volumes {
		switch {
		case v.ConfigMap != nil:
			add("ConfigMap", v.ConfigMap.Name)
		case v.Secret != nil:
			add("Secret", v.Secret.SecretName)
		case v.EmptyDir != nil:
		case v.DownwardAPI != nil:
			// Container references are rewritten after the template has been copied.
		default:
			out.Blockers = append(out.Blockers, "unsupported volume "+v.Name+": only ConfigMap, Secret, emptyDir and downwardAPI are supported")
		}
	}

	for _, ref := range spec.ImagePullSecrets {
		add("Secret", ref.Name)
	}

	if len(refs) > 32 {
		out.Blockers = append(out.Blockers, "at most 32 dependencies are supported")
	}

	refKeys := make([]string, 0, len(refs))
	for key := range refs {
		refKeys = append(refKeys, key)
	}

	sort.Strings(refKeys)
	for _, key := range refKeys {
		kind, name, _ := strings.Cut(key, "/")
		dep := domain.PreviewDependency{Kind: kind, Name: name}
		if kind == "Secret" {
			secret, e := p.client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
			if e != nil {
				out.Blockers = append(out.Blockers, "cannot read Secret "+name+"; grant named get permission or provision the dependency")
			} else {
				if secret.Annotations[corev1.ServiceAccountNameKey] != "" {
					out.Blockers = append(out.Blockers, "service-account credentials cannot be copied: "+name)
				}

				if secret.Type != corev1.SecretTypeOpaque && secret.Type != "" && secret.Type != corev1.SecretTypeDockerConfigJson && secret.Type != corev1.SecretTypeDockercfg && secret.Type != corev1.SecretTypeTLS {
					out.Blockers = append(out.Blockers, "unsupported Secret type for "+name)
				}

				dep.UID = string(secret.UID)
				dep.ResourceVersion = secret.ResourceVersion
			}
		} else {
			cm, e := p.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
			if e != nil {
				out.Blockers = append(out.Blockers, "cannot read ConfigMap "+name+"; grant named get permission or provision the dependency")
			} else {
				dep.UID = string(cm.UID)
				dep.ResourceVersion = cm.ResourceVersion
				for key, value := range cm.Data {
					if replacement, ok := sel.ConfigMapKeys[name][key]; ok {
						value = replacement
					}

					out.Connectivity = append(out.Connectivity, connectivityFindings(b, "config_map_keys/"+name+"/"+key, value)...)
				}

				for key := range sel.ConfigMapKeys[name] {
					if _, ok := cm.Data[key]; !ok {
						out.Blockers = append(out.Blockers, "ConfigMap replacement must identify an existing text key: "+name+"/"+key)
					}
				}
			}
		}

		out.Dependencies = append(out.Dependencies, dep)
		resourceName := "secrets"
		if kind == "ConfigMap" {
			resourceName = "configmaps"
		}

		out.SourceReadRules = append(out.SourceReadRules, map[string]any{"apiGroups": []string{""}, "resources": []string{resourceName}, "resourceNames": []string{name}, "verbs": []string{"get"}})
	}

	for name := range sel.ConfigMapKeys {
		if !refs["ConfigMap/"+name] {
			out.Blockers = append(out.Blockers, "ConfigMap override does not refer to a workload dependency: "+name)
		}
	}

	for name, value := range sel.Env {
		found := false
		for i := range app.Env {
			if app.Env[i].Name == name {
				if app.Env[i].ValueFrom != nil {
					out.Blockers = append(out.Blockers, "literal overrides cannot replace environment references: "+name)
				} else {
					app.Env[i].Value = value
				}

				found = true
			}
		}

		if !found {
			out.Blockers = append(out.Blockers, "environment override must identify an existing literal variable: "+name)
		}
	}

	for _, env := range app.Env {
		if env.ValueFrom == nil {
			out.Connectivity = append(out.Connectivity, connectivityFindings(b, "env/"+env.Name, env.Value)...)
		}
	}

	sort.Slice(out.Connectivity, func(i, j int) bool { return out.Connectivity[i].Location < out.Connectivity[j].Location })

	// The approved execution contract excludes only deliberately live fields.
	contract := template.DeepCopy()
	contractApp := &contract.Spec.Containers[0]
	contractApp.Image = ""
	contractApp.Resources = corev1.ResourceRequirements{}
	contractApp.ReadinessProbe = nil
	contractApp.LivenessProbe = nil
	contractApp.StartupProbe = nil
	for i := range contractApp.Env {
		contractApp.Env[i].Value = ""
	}

	contract.Labels = nil
	out.Contract = previewHash(struct {
		Template corev1.PodTemplateSpec
		Target   int32
		Service  map[string]string
	}{*contract, target, svc.Spec.Selector})
	out.Selection = sel
	// Remove source tracking and identities; keep ordinary application labels for downwardAPI.
	for key := range template.Labels {
		if strings.HasPrefix(key, "envy.dev/") || strings.HasPrefix(key, "argocd.argoproj.io/") || strings.HasPrefix(key, "helm.sh/") || key == "app.kubernetes.io/instance" || key == "app.kubernetes.io/managed-by" || key == "pod-template-hash" {
			delete(template.Labels, key)
		}
	}

	template.Annotations = nil
	template.Spec.ServiceAccountName = "envy-workload"
	template.Spec.DeprecatedServiceAccount = ""
	template.Spec.AutomountServiceAccountToken = new(false)
	template.Spec.EnableServiceLinks = new(false)
	app.Name = c.ID
	// Include target port as provider metadata, not a source annotation.
	template.Annotations = map[string]string{"envy.dev/target-port": fmt.Sprint(target)}
	data, _ := json.Marshal(template)
	if len(data) > 128<<10 {
		out.Blockers = append(out.Blockers, "template exceeds 128 KiB")
	}

	_ = json.Unmarshal(data, &out.Configuration)
	sort.Strings(out.Blockers)
	out.Inspection = previewHash(struct {
		Source    domain.PreviewSource
		Deps      []domain.PreviewDependency
		Selection domain.PreviewSelection
		Template  string
		Contract  string
	}{out.Source, out.Dependencies, sel, string(data), out.Contract})
	policy := p.previewPolicy.Defaults()
	out.Snapshot = domain.PreviewSnapshot{MeshBudget: map[string]string{"requests.cpu": policy.MeshRequestCPU, "requests.memory": policy.MeshRequestMemory, "limits.cpu": policy.MeshLimitCPU, "limits.memory": policy.MeshLimitMemory}, Source: out.Source, Dependencies: out.Dependencies, TemplateJSON: string(data), Selection: sel, Contract: out.Contract}
	return out, nil
}

func (p *Provider) onlyLinkerdInjectionAnnotations(annotations map[string]string) bool {
	if p.mesh != "linkerd" {
		return false
	}

	for key, value := range annotations {
		if key == "linkerd.io/inject" && value == "enabled" {
			continue
		}

		if strings.HasPrefix(key, "config.linkerd.io/proxy-") {
			continue
		}

		return false
	}

	return true
}

func decodePreview(s *domain.PreviewSnapshot) (corev1.PodTemplateSpec, error) {
	var t corev1.PodTemplateSpec
	if s == nil || json.Unmarshal([]byte(s.TemplateJSON), &t) != nil || len(t.Spec.Containers) != 1 {
		return t, fmt.Errorf("invalid persisted preview template")
	}

	return t, nil
}

// Equal versions are required before any missing dependency is copied. Existing
// immutable copies survive source rotation and controller restarts unchanged.
func sameDependency(meta metav1.Object, d domain.PreviewDependency) bool {
	return string(meta.GetUID()) == d.UID && meta.GetResourceVersion() == d.ResourceVersion && meta.GetDeletionTimestamp() == nil
}

func depName(component, kind, name string) string {
	return "envy-dep-" + previewHash([]string{component, kind, name})[:24]
}

func rewritePreview(t *corev1.PodTemplateSpec, component string) {
	name := func(kind, s string) string { return depName(component, kind, s) }
	for i := range t.Spec.Containers {
		c := &t.Spec.Containers[i]
		for j := range c.Env {
			v := c.Env[j].ValueFrom
			if v == nil {
				continue
			}

			if v.SecretKeyRef != nil {
				v.SecretKeyRef.Name = name("Secret", v.SecretKeyRef.Name)
			}

			if v.ConfigMapKeyRef != nil {
				v.ConfigMapKeyRef.Name = name("ConfigMap", v.ConfigMapKeyRef.Name)
			}
		}

		for j := range c.EnvFrom {
			v := &c.EnvFrom[j]
			if v.SecretRef != nil {
				v.SecretRef.Name = name("Secret", v.SecretRef.Name)
			}

			if v.ConfigMapRef != nil {
				v.ConfigMapRef.Name = name("ConfigMap", v.ConfigMapRef.Name)
			}
		}
	}

	for i := range t.Spec.Volumes {
		v := &t.Spec.Volumes[i]
		if v.Secret != nil {
			v.Secret.SecretName = name("Secret", v.Secret.SecretName)
		}

		if v.ConfigMap != nil {
			v.ConfigMap.Name = name("ConfigMap", v.ConfigMap.Name)
		}

		if v.DownwardAPI != nil {
			for i := range v.DownwardAPI.Items {
				ref := v.DownwardAPI.Items[i].ResourceFieldRef
				if ref != nil && ref.ContainerName != "" {
					ref.ContainerName = component
				}
			}
		}

	}

	for i := range t.Spec.ImagePullSecrets {
		t.Spec.ImagePullSecrets[i].Name = name("Secret", t.Spec.ImagePullSecrets[i].Name)
	}
}

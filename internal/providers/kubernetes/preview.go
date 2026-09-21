package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
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
	var composite *domain.CompositePreviewPolicy
	policyKey := ""
	if c.Profile == "deployment-composite" {
		policyKey = b.Project + "/" + b.ID + "/" + c.ID
		configured, ok := p.previewPolicy.Composite[policyKey]
		if !ok {
			return out, domain.Validation("deployment-composite requires an operator policy for " + policyKey)
		}

		if err := p.previewPolicy.Validate(); err != nil {
			return out, domain.Validation("invalid composite installation policy: " + err.Error())
		}

		// A snapshot must not retain mutable maps or slices from live installation
		// configuration: its complete policy is part of the frozen contract.
		encoded, _ := json.Marshal(configured)
		composite = &domain.CompositePreviewPolicy{}
		_ = json.Unmarshal(encoded, composite)
		out.Blockers = append(out.Blockers, compositeContainerBlockers(*spec, *composite, composite.ApplicationContainer)...)
		out.Warnings = append(out.Warnings, "Shared non-production dependencies: "+strings.Join(composite.SharedDependencies, ", "))
	} else if len(spec.Containers) != 1 || len(spec.InitContainers) != 0 {
		out.Blockers = append(out.Blockers, "requires one application container and no declared init containers or sidecars; allow platform Istio injection on new Pods")
	}

	if len(spec.Containers) == 0 {
		return out, domain.Validation("Deployment has no application container")
	}

	app := &spec.Containers[0]
	if composite != nil {
		app = nil
		for i := range spec.Containers {
			if spec.Containers[i].Name == composite.ApplicationContainer {
				app = &spec.Containers[i]
			}
		}

		if app == nil {
			return out, domain.Validation("approved application container is absent: " + composite.ApplicationContainer)
		}

		for _, container := range allPreviewContainers(spec) {
			if container != app && container.Name == c.ID {
				out.Blockers = append(out.Blockers, "supporting container conflicts with the rendered application name: "+c.ID)
			}
		}
	}

	if sel.Container != "" && sel.Container != app.Name {
		return out, domain.Validation("selected container is not the approved application container")
	}

	sel.Deployment = d.Name
	sel.Container = app.Name
	out.Source = domain.PreviewSource{Namespace: ns, Deployment: d.Name, UID: string(d.UID), ResourceVersion: d.ResourceVersion, Generation: d.Generation, Container: app.Name}
	// Fail closed on Pod features whose semantics cannot be preserved in a new namespace.
	allowed := map[string]bool{"containers": true, "volumes": true, "restartPolicy": true, "terminationGracePeriodSeconds": true, "dnsPolicy": true, "serviceAccountName": true, "serviceAccount": true, "automountServiceAccountToken": true, "securityContext": true, "imagePullSecrets": true, "schedulerName": true, "enableServiceLinks": true}
	if composite != nil {
		allowed["initContainers"] = true
	}

	raw, _ := json.Marshal(spec)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	for key := range fields {
		if !allowed[key] {
			out.Blockers = append(out.Blockers, "unsupported Pod setting: "+key)
		}
	}

	if composite != nil {
		sourceAccount := spec.ServiceAccountName
		if sourceAccount == "" {
			sourceAccount = "default"
		}

		if sourceAccount != composite.SourceServiceAccount || (spec.DeprecatedServiceAccount != "" && spec.DeprecatedServiceAccount != sourceAccount) {
			out.Blockers = append(out.Blockers, "source service account does not match composite policy")
		}
	} else if spec.ServiceAccountName != "" && spec.ServiceAccountName != "default" {
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

	psc := spec.SecurityContext
	if psc != nil && ((psc.RunAsUser != nil && *psc.RunAsUser == 0) || psc.SELinuxOptions != nil || psc.WindowsOptions != nil || len(psc.Sysctls) > 0 || previewUnconfinedProfiles(psc.SeccompProfile, psc.AppArmorProfile)) {
		out.Blockers = append(out.Blockers, "Pod security settings exceed preview policy")
	}

	for _, container := range allPreviewContainers(spec) {
		native := composite != nil && container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways
		out.Blockers = append(out.Blockers, p.previewContainerBlockers(*container, psc, native)...)
		if (container == app || native || isRegularPreviewContainer(spec, container.Name)) && container.ReadinessProbe == nil {
			out.Blockers = append(out.Blockers, "container "+container.Name+": source must declare a readiness probe")
		}

		if composite != nil && container != app && !supportingPreviewImage.MatchString(container.Image) {
			out.Blockers = append(out.Blockers, "container "+container.Name+": supporting image must use an immutable sha256 digest")
		}
	}

	if composite != nil {
		resources := previewPodResources(*spec)
		for name, maximum := range map[corev1.ResourceName]string{corev1.ResourceCPU: composite.MaxPodCPU, corev1.ResourceMemory: composite.MaxPodMemory} {
			limit := resources.Limits[name]
			if limit.Cmp(resource.MustParse(maximum)) > 0 {
				out.Blockers = append(out.Blockers, "effective Pod "+string(name)+" limit exceeds composite policy")
			}
		}
	}

	// Preserve the registered Service port and discover its actual container target.
	target := int32(0)
	for _, port := range svc.Spec.Ports {
		if port.Port == binding.Port {
			target = port.TargetPort.IntVal
			if port.TargetPort.StrVal != "" {
				portContainers := []*corev1.Container{app}
				if composite != nil {
					approved := map[string]bool{app.Name: true}
					for _, name := range append(append([]string{}, composite.Sidecars...), composite.NativeSidecars...) {
						approved[name] = true
					}

					portContainers = nil
					for _, container := range allPreviewContainers(spec) {
						if approved[container.Name] {
							portContainers = append(portContainers, container)
						}
					}
				}

				matches := 0
				for _, container := range portContainers {
					for _, cp := range container.Ports {
						if cp.Name == port.TargetPort.StrVal {
							target = cp.ContainerPort
							matches++
						}
					}
				}

				if composite != nil && matches > 1 {
					out.Blockers = append(out.Blockers, "source Service named target is ambiguous across approved containers: "+port.TargetPort.StrVal)
					target = 0
				}
			}
		}
	}

	if target < 1024 || target > 65535 {
		out.Blockers = append(out.Blockers, "source Service must target an unprivileged application port")
	}

	refs := map[string]bool{}
	add := func(kind, name string) { refs[kind+"/"+name] = true }
	for _, container := range allPreviewContainers(spec) {
		for i := range container.Env {
			e := &container.Env[i]
			if e.Name == "POD_UID" || strings.HasPrefix(e.Name, "ENVY_") {
				out.Blockers = append(out.Blockers, "container "+container.Name+": reserved environment variable: "+e.Name)
			}

			if e.ValueFrom != nil {
				if e.ValueFrom.SecretKeyRef != nil {
					add("Secret", e.ValueFrom.SecretKeyRef.Name)
				}

				if e.ValueFrom.ConfigMapKeyRef != nil {
					add("ConfigMap", e.ValueFrom.ConfigMapKeyRef.Name)
				}
			}
		}

		for _, e := range container.EnvFrom {
			if e.SecretRef != nil {
				add("Secret", e.SecretRef.Name)
			}

			if e.ConfigMapRef != nil {
				add("ConfigMap", e.ConfigMapRef.Name)
			}
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

	for _, container := range allPreviewContainers(spec) {
		for _, env := range container.Env {
			if env.ValueFrom == nil {
				location := "env/" + env.Name
				if composite != nil {
					location = "containers/" + container.Name + "/" + location
				}

				out.Connectivity = append(out.Connectivity, connectivityFindings(b, location, env.Value)...)
			}
		}
	}

	sort.Slice(out.Connectivity, func(i, j int) bool { return out.Connectivity[i].Location < out.Connectivity[j].Location })

	// Retain the original single-container contract normalization for persisted approvals.
	if composite == nil {
		for i := range app.Env {
			if source := app.Env[i].ValueFrom; source != nil && source.ResourceFieldRef != nil && source.ResourceFieldRef.ContainerName != "" {
				source.ResourceFieldRef.ContainerName = c.ID
			}
		}
	}

	// The approved execution contract excludes only deliberately live fields.
	contract := template.DeepCopy()
	var contractApp *corev1.Container
	for i := range contract.Spec.Containers {
		if contract.Spec.Containers[i].Name == app.Name {
			contractApp = &contract.Spec.Containers[i]
			break
		}
	}

	contractApp.Image = ""
	contractApp.Resources = corev1.ResourceRequirements{}
	contractApp.ReadinessProbe = nil
	contractApp.LivenessProbe = nil
	contractApp.StartupProbe = nil
	for i := range contractApp.Env {
		contractApp.Env[i].Value = ""
	}

	contract.Labels = nil
	contractValue := struct {
		Template corev1.PodTemplateSpec
		Target   int32
		Service  map[string]string
	}{*contract, target, svc.Spec.Selector}
	out.Contract = previewHash(contractValue)
	if composite != nil {
		out.Contract = previewHash(struct {
			Workload  any
			PolicyKey string
			Policy    domain.CompositePreviewPolicy
		}{contractValue, policyKey, *composite})
	}

	out.Selection = sel
	// Remove source tracking and identities; keep ordinary application labels for downwardAPI.
	for key := range template.Labels {
		if strings.HasPrefix(key, "envy.dev/") || strings.HasPrefix(key, "argocd.argoproj.io/") || strings.HasPrefix(key, "helm.sh/") || key == "app.kubernetes.io/instance" || key == "app.kubernetes.io/managed-by" || key == "pod-template-hash" {
			delete(template.Labels, key)
		}
	}

	template.Annotations = nil
	template.Spec.ServiceAccountName = "envy-workload"
	if composite != nil {
		template.Spec.ServiceAccountName = composite.ServiceAccount
	}

	template.Spec.DeprecatedServiceAccount = ""
	template.Spec.AutomountServiceAccountToken = new(false)
	template.Spec.EnableServiceLinks = new(false)
	rewritePreviewContainerReferences(template, app.Name, c.ID)
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
	out.Snapshot = domain.PreviewSnapshot{ApplicationContainer: c.ID, CompositePolicyKey: policyKey, CompositePolicy: composite, MeshBudget: map[string]string{"requests.cpu": policy.MeshRequestCPU, "requests.memory": policy.MeshRequestMemory, "limits.cpu": policy.MeshLimitCPU, "limits.memory": policy.MeshLimitMemory}, Source: out.Source, Dependencies: out.Dependencies, TemplateJSON: string(data), Selection: sel, Contract: out.Contract}
	out.CompositePolicyKey = policyKey
	if composite != nil {
		// Review output and the internal execution snapshot have independent values.
		// A caller preparing display output must not mutate captured execution policy.
		encoded, _ := json.Marshal(composite)
		out.CompositePolicy = &domain.CompositePreviewPolicy{}
		_ = json.Unmarshal(encoded, out.CompositePolicy)
	}

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
	if s == nil || json.Unmarshal([]byte(s.TemplateJSON), &t) != nil {
		return t, fmt.Errorf("invalid persisted preview template")
	}

	if _, err := previewApplication(&t, s); err != nil {
		return t, err
	}

	if s.CompositePolicy == nil {
		if s.CompositePolicyKey != "" || len(t.Spec.Containers) != 1 || len(t.Spec.InitContainers) != 0 {
			return t, fmt.Errorf("invalid persisted single-container preview template")
		}
	} else if s.CompositePolicyKey == "" || s.ApplicationContainer == "" || s.Source.Container != s.CompositePolicy.ApplicationContainer ||
		t.Spec.ServiceAccountName != s.CompositePolicy.ServiceAccount || t.Spec.AutomountServiceAccountToken == nil || *t.Spec.AutomountServiceAccountToken ||
		len(compositeContainerBlockers(t.Spec, *s.CompositePolicy, s.ApplicationContainer)) != 0 {
		return t, fmt.Errorf("invalid persisted composite preview template")
	}

	return t, nil
}

// previewApplication preserves legacy snapshots while selecting composite apps by
// their captured rendered name, never by their position in the container list.
func previewApplication(t *corev1.PodTemplateSpec, s *domain.PreviewSnapshot) (*corev1.Container, error) {
	name := ""
	if s != nil {
		name = s.ApplicationContainer
	}

	if name == "" && len(t.Spec.Containers) == 1 {
		return &t.Spec.Containers[0], nil
	}

	var selected *corev1.Container
	for i := range t.Spec.Containers {
		if t.Spec.Containers[i].Name == name {
			if selected != nil {
				return nil, fmt.Errorf("duplicate persisted application container")
			}

			selected = &t.Spec.Containers[i]
		}
	}

	if selected == nil {
		return nil, fmt.Errorf("persisted preview application container is absent")
	}

	return selected, nil
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
	for _, c := range allPreviewContainers(&t.Spec) {
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

		// Old single-container snapshots predate capture-time container rewrites.
		if len(t.Spec.Containers) == 1 && len(t.Spec.InitContainers) == 0 && v.DownwardAPI != nil {
			for i := range v.DownwardAPI.Items {
				if ref := v.DownwardAPI.Items[i].ResourceFieldRef; ref != nil && ref.ContainerName != "" {
					ref.ContainerName = component
				}
			}
		}
	}

	for i := range t.Spec.ImagePullSecrets {
		t.Spec.ImagePullSecrets[i].Name = name("Secret", t.Spec.ImagePullSecrets[i].Name)
	}
}

var supportingPreviewImage = regexp.MustCompile(`^[^\s@]+@sha256:[a-f0-9]{64}$`)

func allPreviewContainers(spec *corev1.PodSpec) []*corev1.Container {
	containers := make([]*corev1.Container, 0, len(spec.Containers)+len(spec.InitContainers))
	for i := range spec.Containers {
		containers = append(containers, &spec.Containers[i])
	}

	for i := range spec.InitContainers {
		containers = append(containers, &spec.InitContainers[i])
	}

	return containers
}

func isRegularPreviewContainer(spec *corev1.PodSpec, name string) bool {
	for _, container := range spec.Containers {
		if container.Name == name {
			return true
		}
	}

	return false
}

func compositeContainerBlockers(spec corev1.PodSpec, policy domain.CompositePreviewPolicy, app string) []string {
	var blockers []string
	allowed := map[string]string{app: "application"}
	for role, names := range map[string][]string{"sidecar": policy.Sidecars, "init": policy.InitContainers, "native sidecar": policy.NativeSidecars} {
		for _, name := range names {
			if _, exists := allowed[name]; exists {
				blockers = append(blockers, "duplicate composite policy container: "+name)
			}

			allowed[name] = role
		}
	}

	seen := map[string]bool{}
	for _, container := range allPreviewContainers(&spec) {
		name := container.Name
		if seen[name] || previewInjectedMeshContainer(name) {
			blockers = append(blockers, "duplicate or reserved composite container: "+name)
		}

		seen[name] = true
		role, ok := allowed[name]
		if !ok {
			blockers = append(blockers, "container is not approved by composite policy: "+name)
			continue
		}

		blockers = append(blockers, compositeContainerRoleBlockers(*container, role, isRegularPreviewContainer(&spec, name))...)
	}

	for name := range allowed {
		if !seen[name] {
			blockers = append(blockers, "approved container is absent: "+name)
		}
	}

	sort.Strings(blockers)
	return blockers
}

func compositeContainerRoleBlockers(container corev1.Container, role string, regular bool) []string {
	var blockers []string
	prefix := "container " + container.Name + ": "
	if regular != (role == "application" || role == "sidecar") {
		blockers = append(blockers, prefix+"placement does not match approved "+role+" role")
	}

	native := container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways
	if native != (role == "native sidecar") || (container.RestartPolicy != nil && !native) {
		blockers = append(blockers, prefix+"restart policy does not match approved "+role+" role")
	}

	if role == "init" && (container.ReadinessProbe != nil || container.LivenessProbe != nil || container.StartupProbe != nil) {
		blockers = append(blockers, prefix+"one-shot init containers cannot declare probes")
	}

	return blockers
}

func (p *Provider) previewContainerBlockers(container corev1.Container, psc *corev1.PodSecurityContext, native bool) []string {
	var blockers []string
	block := func(message string) { blockers = append(blockers, "container "+container.Name+": "+message) }
	// Enumerate supported fields so newly introduced Kubernetes features do not
	// become executable simply because their container name is approved.
	allowed := map[string]bool{"name": true, "image": true, "command": true, "args": true, "workingDir": true, "ports": true, "envFrom": true, "env": true, "resources": true, "volumeMounts": true, "livenessProbe": true, "readinessProbe": true, "startupProbe": true, "terminationMessagePath": true, "terminationMessagePolicy": true, "imagePullPolicy": true, "securityContext": true}
	if native {
		allowed["restartPolicy"] = true
	}

	raw, _ := json.Marshal(container)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	for field := range fields {
		if !allowed[field] {
			block("unsupported container setting: " + field)
		}
	}

	if len(container.Resources.Claims) > 0 {
		block("resource claims are unsupported")
	}

	for _, message := range previewSecurityBlockers(container.SecurityContext, psc) {
		block(message)
	}

	for _, port := range container.Ports {
		if port.HostPort != 0 {
			block("host ports are unsupported")
		}
	}

	for _, message := range p.previewResourceBlockers(container.Resources) {
		block(message)
	}

	return blockers
}

func previewSecurityBlockers(sc *corev1.SecurityContext, psc *corev1.PodSecurityContext) []string {
	var blockers []string
	if previewUnsafeContainerSecurity(sc) {
		blockers = append(blockers, "security settings exceed preview policy")
	}

	if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		blockers = append(blockers, "source must disable privilege escalation explicitly")
	}

	if sc != nil && sc.ProcMount != nil && *sc.ProcMount != corev1.DefaultProcMount {
		blockers = append(blockers, "unmasked proc mounts are unsupported")
	}

	if !previewNonRoot(sc, psc) {
		blockers = append(blockers, "source must explicitly declare a non-root application identity")
	}

	return blockers
}

func previewUnconfinedProfiles(seccomp *corev1.SeccompProfile, apparmor *corev1.AppArmorProfile) bool {
	return (seccomp != nil && seccomp.Type == corev1.SeccompProfileTypeUnconfined) ||
		(apparmor != nil && apparmor.Type == corev1.AppArmorProfileTypeUnconfined)
}

func previewUnsafeContainerSecurity(sc *corev1.SecurityContext) bool {
	if sc == nil {
		return false
	}

	return (sc.Privileged != nil && *sc.Privileged) ||
		(sc.AllowPrivilegeEscalation != nil && *sc.AllowPrivilegeEscalation) ||
		(sc.RunAsUser != nil && *sc.RunAsUser == 0) ||
		(sc.Capabilities != nil && len(sc.Capabilities.Add) > 0) ||
		sc.SELinuxOptions != nil || sc.WindowsOptions != nil ||
		previewUnconfinedProfiles(sc.SeccompProfile, sc.AppArmorProfile)
}

func previewNonRoot(sc *corev1.SecurityContext, psc *corev1.PodSecurityContext) bool {
	var user *int64
	var nonroot *bool
	if psc != nil {
		user, nonroot = psc.RunAsUser, psc.RunAsNonRoot
	}

	if sc != nil {
		if sc.RunAsUser != nil {
			user = sc.RunAsUser
		}

		if sc.RunAsNonRoot != nil {
			nonroot = sc.RunAsNonRoot
		}
	}

	return (user != nil && *user > 0) || (nonroot != nil && *nonroot)
}

func (p *Provider) previewResourceBlockers(requirements corev1.ResourceRequirements) []string {
	var blockers []string
	policy := p.previewPolicy.Defaults()
	for name, maximum := range map[corev1.ResourceName]string{corev1.ResourceCPU: policy.MaxCPU, corev1.ResourceMemory: policy.MaxMemory} {
		request, requested := requirements.Requests[name]
		limit, limited := requirements.Limits[name]
		if !requested || !limited || request.Sign() <= 0 || limit.Sign() <= 0 || request.Cmp(limit) > 0 || limit.Cmp(resource.MustParse(maximum)) > 0 {
			blockers = append(blockers, string(name)+" requests and limits must be positive, within installation maxima, with request <= limit")
		}
	}

	for name := range requirements.Limits {
		if name != corev1.ResourceCPU && name != corev1.ResourceMemory {
			blockers = append(blockers, "unsupported resource: "+string(name))
		}
	}

	for name := range requirements.Requests {
		if name != corev1.ResourceCPU && name != corev1.ResourceMemory {
			blockers = append(blockers, "unsupported resource request: "+string(name))
		}
	}

	return blockers
}

func rewritePreviewContainerReferences(t *corev1.PodTemplateSpec, source, destination string) {
	for _, container := range allPreviewContainers(&t.Spec) {
		for i := range container.Env {
			value := container.Env[i].ValueFrom
			if value != nil && value.ResourceFieldRef != nil && value.ResourceFieldRef.ContainerName == source {
				value.ResourceFieldRef.ContainerName = destination
			}
		}
	}

	for i := range t.Spec.Volumes {
		if downward := t.Spec.Volumes[i].DownwardAPI; downward != nil {
			for j := range downward.Items {
				if ref := downward.Items[j].ResourceFieldRef; ref != nil && ref.ContainerName == source {
					ref.ContainerName = destination
				}
			}
		}
	}
}

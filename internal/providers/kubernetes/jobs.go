//nolint:gocyclo,wsl_v5 // Provider lifecycle checks intentionally keep each ownership boundary explicit.
package kubernetes

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/providers/kubeapply"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (p *Provider) ensureJob(ctx context.Context, s domain.WorkloadSpec) (domain.WorkloadRef, error) {
	ns, ref, err := p.ensureExecutionNamespace(ctx, s)
	if err != nil {
		return ref, err
	}

	name := jobName(s.ComponentID, s.Execution.ID)
	ref.Kind, ref.Job = domain.WorkloadJob, name
	job, err := p.ensureJobResource(ctx, s, ns, name)
	if err != nil {
		return ref, err
	}
	ref.JobUID = string(job.UID)
	return ref, nil
}

func (p *Provider) ensureExecutionNamespace(ctx context.Context, s domain.WorkloadSpec) (string, domain.WorkloadRef, error) {
	if s.Execution == nil || s.Execution.ID == "" || s.Execution.SpecHash == "" || s.Profile.ValidateExecution() != nil {
		return "", domain.WorkloadRef{}, fmt.Errorf("workload requires a persisted, valid execution identity")
	}
	if s.WorkloadCount == 0 {
		s.WorkloadCount = 1
	}
	if s.WorkloadCount < 1 || s.WorkloadCount > domain.MaxOverrides {
		return "", domain.WorkloadRef{}, fmt.Errorf("invalid workload count")
	}
	if err := p.validateCompositeRuntime(s); err != nil {
		return "", domain.WorkloadRef{}, err
	}
	for _, name := range s.Profile.ImagePullSecrets {
		if !slicesContains(p.approvedPullSecrets, name) {
			return "", domain.WorkloadRef{}, fmt.Errorf("image pull Secret is not operator-approved: %s", name)
		}
	}
	if err := p.namespacePolicy.Ready(); err != nil {
		return "", domain.WorkloadRef{}, err
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
			return "", domain.WorkloadRef{}, err
		}
		kubeapply.Stamp(wantNS)
		currentNS, err = p.client.CoreV1().Namespaces().Create(ctx, wantNS, metav1.CreateOptions{FieldManager: kubeapply.RuntimeManager})
	} else if err == nil {
		err = p.owned(currentNS, s.OwnershipToken)
	}
	if err != nil {
		return "", domain.WorkloadRef{}, fmt.Errorf("ensure namespace: %w", err)
	}
	if currentNS.DeletionTimestamp != nil {
		return "", domain.WorkloadRef{}, fmt.Errorf("namespace is terminating")
	}
	if kubeapply.Changed(wantNS, currentNS) {
		if _, err = kubeapply.Apply(ctx, p.client.CoreV1().Namespaces(), wantNS, currentNS, "v1", "Namespace", kubeapply.RuntimeManager, p.writable); err != nil {
			return "", domain.WorkloadRef{}, err
		}
	}

	ref := domain.WorkloadRef{Namespace: ns, NamespaceUID: string(currentNS.UID), OwnershipToken: s.OwnershipToken, Image: s.Image}
	if err = p.ensureNetworkPolicy(ctx, s, ns); err != nil {
		return "", ref, err
	}
	if err = p.ensureQuota(ctx, s, ns); err != nil {
		return "", ref, err
	}
	if err = p.ensureAccount(ctx, s, ns); err != nil {
		return "", ref, err
	}
	return ns, ref, nil
}

func jobName(component, executionID string) string {
	component = strings.Trim(component, "-")
	if len(component) > 42 {
		component = component[:42]
	}
	return component + "-" + executionID[:min(20, len(executionID))]
}

func (p *Provider) ensureJobResource(ctx context.Context, s domain.WorkloadSpec, ns, name string) (*batchv1.Job, error) {
	timeout, err := parseTimeout(s.Profile.Execution.Timeout)
	if err != nil {
		return nil, err
	}
	backoff := s.Profile.Execution.RetryLimit
	deadline := int64(timeout.Seconds())
	meta := p.metadata(s, name, ns)
	annotations := map[string]string{"envy.dev/execution-id": s.Execution.ID, "envy.dev/execution-spec-hash": s.Execution.SpecHash}
	meta.Annotations["envy.dev/execution-spec-hash"] = s.Execution.SpecHash
	for key, value := range p.podAnnotations {
		annotations[key] = value
	}
	container := corev1.Container{
		Name:            s.ComponentID,
		Image:           s.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             []corev1.EnvVar{{Name: "ENVY_COMPOSITION_ID", Value: s.CompositionID}, {Name: "ENVY_EXECUTION_ID", Value: s.Execution.ID}, {Name: "POD_UID", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"}}}},
		SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: new(false), ReadOnlyRootFilesystem: new(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
		Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("64Mi")}},
	}
	keys := make([]string, 0, len(s.Profile.Env)+len(s.MessagingEnv))
	values := map[string]string{}
	for key, value := range s.Profile.Env {
		values[key] = value
	}
	for key, value := range s.MessagingEnv {
		values[key] = value
	}
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		container.Env = append(container.Env, corev1.EnvVar{Name: key, Value: values[key]})
	}
	want := &batchv1.Job{ObjectMeta: meta, Spec: batchv1.JobSpec{BackoffLimit: &backoff, ActiveDeadlineSeconds: &deadline, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: meta.Labels, Annotations: annotations}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, ServiceAccountName: "envy-workload", AutomountServiceAccountToken: new(false), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: new(true), RunAsUser: new(int64(65532)), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{container}}}}}
	for _, secret := range s.Profile.ImagePullSecrets {
		want.Spec.Template.Spec.ImagePullSecrets = append(want.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: secret})
	}
	api := p.client.BatchV1().Jobs(ns)
	got, err := unchanged(p, want, func() (*batchv1.Job, error) { return api.Get(ctx, name, metav1.GetOptions{}) })
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
	if got.Annotations["envy.dev/execution-spec-hash"] != s.Execution.SpecHash {
		return nil, fmt.Errorf("job execution identity is already bound to a different specification")
	}
	return got, nil
}

func parseTimeout(value string) (duration time.Duration, err error) { return time.ParseDuration(value) }

func (p *Provider) observeJob(ctx context.Context, ref domain.WorkloadRef) (domain.WorkloadObservation, error) {
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
	job, err := p.client.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Job, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return domain.WorkloadObservation{Message: "Job absent", Image: ref.Image}, nil
	}
	if err != nil {
		return domain.WorkloadObservation{}, err
	}
	if err = p.owned(job, ref.OwnershipToken); err != nil {
		return domain.WorkloadObservation{}, err
	}
	if ref.JobUID != "" && string(job.UID) != ref.JobUID {
		return domain.WorkloadObservation{}, fmt.Errorf("job identity changed")
	}
	obs := domain.WorkloadObservation{Image: ref.Image, WorkloadID: string(job.UID), State: domain.ExecutionPending, Message: "Job pending"}
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			obs.State, obs.Ready, obs.Message = domain.ExecutionSucceeded, true, "Job completed"
			return obs, nil
		case batchv1.JobFailed:
			obs.State, obs.Failed, obs.Message = domain.ExecutionFailed, true, condition.Message
			if obs.Message == "" {
				obs.Message = "Job failed"
			}
			return obs, nil
		}
	}
	if job.Status.Active > 0 || job.Status.StartTime != nil {
		obs.State, obs.Message = domain.ExecutionRunning, "Job running"
	}
	return obs, nil
}

func (p *Provider) deleteJob(ctx context.Context, ref domain.WorkloadRef) error {
	if ref.Namespace == "" || ref.Job == "" || ref.OwnershipToken == "" {
		return fmt.Errorf("cannot delete Job without persisted identity and ownership token")
	}
	job, err := p.client.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Job, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = p.owned(job, ref.OwnershipToken); err != nil {
		return err
	}
	if ref.JobUID != "" && string(job.UID) != ref.JobUID {
		return fmt.Errorf("refuse Job deletion: identity changed")
	}
	if err = p.writable(ctx); err != nil {
		return err
	}
	uid := job.UID
	propagation := metav1.DeletePropagationBackground
	return p.client.BatchV1().Jobs(ref.Namespace).Delete(ctx, ref.Job, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &propagation})
}

func slicesContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

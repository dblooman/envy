//nolint:gocognit,gocyclo,wsl_v5 // CronJob ownership and terminal-state checks stay explicit.
package kubernetes

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/providers/kubeapply"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (p *Provider) ensureScheduledJob(ctx context.Context, s domain.WorkloadSpec) (domain.WorkloadRef, error) {
	ns, ref, err := p.ensureExecutionNamespace(ctx, s)
	if err != nil {
		return ref, err
	}

	name := jobName(s.ComponentID, s.Execution.ID)
	ref.Kind, ref.CronJob, ref.MaxRuns = domain.WorkloadScheduledJob, name, s.Profile.Execution.MaxRuns
	cron, err := p.ensureCronJobResource(ctx, s, ns, name)
	if err != nil {
		return ref, err
	}
	ref.CronJobUID = string(cron.UID)
	return ref, nil
}

func (p *Provider) ensureCronJobResource(ctx context.Context, s domain.WorkloadSpec, ns, name string) (*batchv1.CronJob, error) {
	timeout, err := parseTimeout(s.Profile.Execution.Timeout)
	if err != nil {
		return nil, err
	}
	backoff, deadline, missed := s.Profile.Execution.RetryLimit, int64(timeout.Seconds()), int64(30)
	suspend := !s.ScheduleActive
	meta := p.metadata(s, name, ns)
	meta.Annotations["envy.dev/execution-spec-hash"] = s.Execution.SpecHash
	annotations := map[string]string{"envy.dev/execution-id": s.Execution.ID, "envy.dev/execution-spec-hash": s.Execution.SpecHash}
	maps.Copy(annotations, p.podAnnotations)
	container := corev1.Container{Name: s.ComponentID, Image: s.Image, ImagePullPolicy: corev1.PullIfNotPresent, Env: []corev1.EnvVar{{Name: "ENVY_COMPOSITION_ID", Value: s.CompositionID}, {Name: "ENVY_EXECUTION_ID", Value: s.Execution.ID}, {Name: "POD_UID", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"}}}}, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: new(false), ReadOnlyRootFilesystem: new(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("64Mi")}}}
	values := map[string]string{}
	maps.Copy(values, s.Profile.Env)
	maps.Copy(values, s.MessagingEnv)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		container.Env = append(container.Env, corev1.EnvVar{Name: key, Value: values[key]})
	}
	// Keep enough completed children to enforce the persisted run bound. A
	// one-item history would make a long-lived schedule appear to have run once.
	history := s.Profile.Execution.MaxRuns
	want := &batchv1.CronJob{ObjectMeta: meta, Spec: batchv1.CronJobSpec{Schedule: s.Profile.Execution.Schedule, Suspend: &suspend, ConcurrencyPolicy: batchv1.ForbidConcurrent, StartingDeadlineSeconds: &missed, SuccessfulJobsHistoryLimit: &history, FailedJobsHistoryLimit: &history, JobTemplate: batchv1.JobTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: meta.Labels, Annotations: annotations}, Spec: batchv1.JobSpec{BackoffLimit: &backoff, ActiveDeadlineSeconds: &deadline, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: meta.Labels, Annotations: annotations}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, ServiceAccountName: "envy-workload", AutomountServiceAccountToken: new(false), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: new(true), RunAsUser: new(int64(65532)), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{container}}}}}}}
	for _, secret := range s.Profile.ImagePullSecrets {
		want.Spec.JobTemplate.Spec.Template.Spec.ImagePullSecrets = append(want.Spec.JobTemplate.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: secret})
	}
	api := p.client.BatchV1().CronJobs(ns)
	got, err := unchanged(p, want, func() (*batchv1.CronJob, error) { return api.Get(ctx, name, metav1.GetOptions{}) })
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
		return nil, fmt.Errorf("scheduled Job execution identity is already bound to a different specification")
	}
	if kubeapply.Changed(want, got) {
		return kubeapply.Apply(ctx, api, want, got, "batch/v1", "CronJob", kubeapply.RuntimeManager, p.writable)
	}
	return got, nil
}

func (p *Provider) observeScheduledJob(ctx context.Context, ref domain.WorkloadRef, maxRuns int32) (domain.WorkloadObservation, error) {
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
	cron, err := p.client.BatchV1().CronJobs(ref.Namespace).Get(ctx, ref.CronJob, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return domain.WorkloadObservation{Message: "CronJob absent"}, nil
	}
	if err != nil {
		return domain.WorkloadObservation{}, err
	}
	if err = p.owned(cron, ref.OwnershipToken); err != nil {
		return domain.WorkloadObservation{}, err
	}
	if ref.CronJobUID != "" && string(cron.UID) != ref.CronJobUID {
		return domain.WorkloadObservation{}, fmt.Errorf("CronJob identity changed")
	}
	jobs, err := p.client.BatchV1().Jobs(ref.Namespace).List(ctx, metav1.ListOptions{LabelSelector: CompositionLabel + "=" + cron.Labels[CompositionLabel] + "," + ComponentLabel + "=" + cron.Labels[ComponentLabel]})
	if err != nil {
		return domain.WorkloadObservation{}, err
	}
	obs := domain.WorkloadObservation{Ready: true, State: domain.ExecutionReady, Message: "schedule enabled"}
	allSucceeded := true
	for _, job := range jobs.Items {
		if !ownedByCronJob(job, cron.UID) {
			continue
		}
		obs.Runs++
		terminal := false
		for _, condition := range job.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				continue
			}
			if condition.Type == batchv1.JobFailed {
				obs.State, obs.Ready, obs.Failed, obs.Message = domain.ExecutionFailed, false, true, "scheduled Job failed"
				return obs, nil
			}
			if condition.Type == batchv1.JobComplete {
				terminal = true
			}
		}
		if !terminal {
			allSucceeded = false
		}
	}
	if !allSucceeded || len(cron.Status.Active) > 0 {
		obs.State, obs.Message = domain.ExecutionRunning, "scheduled Job running"
		return obs, nil
	}
	if obs.Runs >= maxRuns {
		obs.State, obs.Message = domain.ExecutionSucceeded, "schedule run limit reached"
		return obs, nil
	}
	if cron.Spec.Suspend != nil && *cron.Spec.Suspend {
		obs.State, obs.Ready, obs.Message = domain.ExecutionSuspended, false, "schedule suspended"
		return obs, nil
	}
	return obs, nil
}

func ownedByCronJob(job batchv1.Job, cronUID types.UID) bool {
	for _, owner := range job.OwnerReferences {
		if owner.UID == cronUID && owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}

func (p *Provider) deleteCronJob(ctx context.Context, ref domain.WorkloadRef) error {
	if ref.Namespace == "" || ref.CronJob == "" || ref.OwnershipToken == "" {
		return fmt.Errorf("cannot delete CronJob without persisted identity and ownership token")
	}
	cron, err := p.client.BatchV1().CronJobs(ref.Namespace).Get(ctx, ref.CronJob, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = p.owned(cron, ref.OwnershipToken); err != nil {
		return err
	}
	if ref.CronJobUID != "" && string(cron.UID) != ref.CronJobUID {
		return fmt.Errorf("refuse CronJob deletion: identity changed")
	}
	if err = p.writable(ctx); err != nil {
		return err
	}
	uid := cron.UID
	propagation := metav1.DeletePropagationBackground
	return p.client.BatchV1().CronJobs(ref.Namespace).Delete(ctx, ref.CronJob, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &propagation})
}

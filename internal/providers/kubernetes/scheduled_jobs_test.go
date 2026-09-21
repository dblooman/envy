//nolint:wsl_v5 // Lifecycle assertions stay adjacent to their state transition.
package kubernetes

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func scheduledJobSpec() domain.WorkloadSpec {
	return domain.WorkloadSpec{
		Profile:       domain.Component{ID: "nightly", Profile: "scheduled-job", Execution: &domain.WorkloadExecution{Kind: domain.WorkloadScheduledJob, Timeout: "2m", RetryLimit: 1, Schedule: "0 1 * * *", MaxRuns: 2, ConcurrencyPolicy: "forbid"}},
		Execution:     &domain.ExecutionRef{ID: "abcdef12345678901234", SpecHash: "scheduled-spec", Generation: 1},
		CompositionID: "scheduled-test", ComponentID: "nightly", ProjectID: "demo", Image: "example/nightly@sha256:123", OwnershipToken: "claim-token",
	}
}

func TestScheduledJobStartsSuspendedThenEnablesBoundedCronJob(t *testing.T) {
	ctx := context.Background()
	p, client, _ := fixture()
	s := scheduledJobSpec()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind != domain.WorkloadScheduledJob || ref.CronJob == "" || ref.CronJobUID == "" || ref.MaxRuns != 2 {
		t.Fatalf("unexpected CronJob reference: %#v", ref)
	}
	cron, err := client.BatchV1().CronJobs(ref.Namespace).Get(ctx, ref.CronJob, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cron.Spec.Suspend == nil || !*cron.Spec.Suspend || cron.Spec.ConcurrencyPolicy != batchv1.ForbidConcurrent || cron.Spec.StartingDeadlineSeconds == nil || *cron.Spec.StartingDeadlineSeconds != 30 || cron.Spec.SuccessfulJobsHistoryLimit == nil || *cron.Spec.SuccessfulJobsHistoryLimit != 2 {
		t.Fatalf("unbounded or active CronJob: %#v", cron.Spec)
	}
	if cron.Spec.JobTemplate.Spec.ActiveDeadlineSeconds == nil || *cron.Spec.JobTemplate.Spec.ActiveDeadlineSeconds != 120 || cron.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("invalid scheduled Job template: %#v", cron.Spec.JobTemplate.Spec)
	}
	s.ScheduleActive = true
	if _, err = p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}
	cron, err = client.BatchV1().CronJobs(ref.Namespace).Get(ctx, ref.CronJob, metav1.GetOptions{})
	if err != nil || cron.Spec.Suspend == nil || *cron.Spec.Suspend {
		t.Fatalf("CronJob did not activate: %#v err=%v", cron, err)
	}
}

func TestScheduledJobObservationStopsAtRunLimit(t *testing.T) {
	ctx := context.Background()
	p, client, _ := fixture()
	s := scheduledJobSpec()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{InstallationLabel: "test", CompositionLabel: s.CompositionID, ComponentLabel: s.ComponentID}
	for _, name := range []string{"nightly-one", "nightly-two"} {
		owner := true
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ref.Namespace, Labels: labels, OwnerReferences: []metav1.OwnerReference{{UID: types.UID(ref.CronJobUID), Controller: &owner}}},
			Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}},
		}
		if _, err = client.BatchV1().Jobs(ref.Namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	otherOwner := true
	if _, err = client.BatchV1().Jobs(ref.Namespace).Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "old-schedule", Namespace: ref.Namespace, Labels: labels, OwnerReferences: []metav1.OwnerReference{{UID: "old-cronjob", Controller: &otherOwner}}}, Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	obs, err := p.Observe(ctx, ref)
	if err != nil || obs.Runs != 2 || obs.State != domain.ExecutionSucceeded || !obs.Ready {
		t.Fatalf("run limit observation=%#v err=%v", obs, err)
	}
	if err = p.DeleteWorkload(ctx, ref); err != nil {
		t.Fatal(err)
	}
	absent, err := p.WorkloadAbsent(ctx, ref)
	if err != nil || !absent {
		t.Fatalf("CronJob absent=%v err=%v", absent, err)
	}
}

func TestScheduledJobWaitsForTheLastChildToFinish(t *testing.T) {
	ctx := context.Background()
	p, client, _ := fixture()
	s := scheduledJobSpec()
	s.Profile.Execution.MaxRuns = 1
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	owner := true
	job, err := client.BatchV1().Jobs(ref.Namespace).Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "nightly-active", Namespace: ref.Namespace, Labels: map[string]string{InstallationLabel: "test", CompositionLabel: s.CompositionID, ComponentLabel: s.ComponentID}, OwnerReferences: []metav1.OwnerReference{{UID: types.UID(ref.CronJobUID), Controller: &owner}}}, Status: batchv1.JobStatus{Active: 1}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	obs, err := p.Observe(ctx, ref)
	if err != nil || obs.State != domain.ExecutionRunning || obs.Runs != 1 {
		t.Fatalf("active final run observation=%#v err=%v", obs, err)
	}
	job.Status.Active = 0
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, err = client.BatchV1().Jobs(ref.Namespace).UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	obs, err = p.Observe(ctx, ref)
	if err != nil || obs.State != domain.ExecutionSucceeded {
		t.Fatalf("completed final run observation=%#v err=%v", obs, err)
	}
}

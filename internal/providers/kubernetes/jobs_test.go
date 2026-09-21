//nolint:wsl_v5 // Assertions stay adjacent to the lifecycle transition they verify.
package kubernetes

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/domain"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func jobSpec() domain.WorkloadSpec {
	return domain.WorkloadSpec{
		Profile:       domain.Component{ID: "report", Profile: "job", Execution: &domain.WorkloadExecution{Kind: domain.WorkloadJob, Timeout: "2m", RetryLimit: 2}},
		Execution:     &domain.ExecutionRef{ID: "12345678901234567890", SpecHash: "spec-hash", Generation: 1},
		CompositionID: "job-test", ComponentID: "report", ProjectID: "demo", Image: "example/report@sha256:123", OwnershipToken: "claim-token",
	}
}

func TestEnsureJobCreatesBoundedEndpointFreeWorkload(t *testing.T) {
	ctx := context.Background()
	p, client, _ := fixture()
	s := jobSpec()
	ref, err := p.Ensure(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind != domain.WorkloadJob || ref.Job == "" || ref.JobUID == "" || ref.Service != "" || ref.Deployment != "" {
		t.Fatalf("unexpected Job reference: %#v", ref)
	}
	if _, err := client.CoreV1().Services(ref.Namespace).Get(ctx, s.ComponentID, metav1.GetOptions{}); err == nil {
		t.Fatal("Job must not create a Service")
	}
	job, err := client.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Job, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 2 || job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 120 {
		t.Fatalf("Job bounds=%#v", job.Spec)
	}
	pod := job.Spec.Template.Spec
	if pod.RestartPolicy != corev1.RestartPolicyNever || pod.ServiceAccountName != "envy-workload" || pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatalf("unsafe Job pod: %#v", pod)
	}
	if job.Annotations["envy.dev/execution-spec-hash"] != s.Execution.SpecHash {
		t.Fatalf("execution identity annotation missing: %v", job.Annotations)
	}
	client.ClearActions()
	if _, err = p.Ensure(ctx, s); err != nil {
		t.Fatal(err)
	}
	if n := mutations(client.Actions()); n != 0 {
		t.Fatalf("unchanged Job Ensure produced %d mutations", n)
	}
}

func TestObserveAndDeleteJob(t *testing.T) {
	ctx := context.Background()
	p, client, _ := fixture()
	ref, err := p.Ensure(ctx, jobSpec())
	if err != nil {
		t.Fatal(err)
	}
	job, err := client.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Job, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Active = 1
	if _, err = client.BatchV1().Jobs(ref.Namespace).UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	obs, err := p.Observe(ctx, ref)
	if err != nil || obs.State != domain.ExecutionRunning || obs.Ready {
		t.Fatalf("running observation=%#v err=%v", obs, err)
	}
	job.Status.Active = 0
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if _, err = client.BatchV1().Jobs(ref.Namespace).UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	obs, err = p.Observe(ctx, ref)
	if err != nil || obs.State != domain.ExecutionSucceeded || !obs.Ready {
		t.Fatalf("completed observation=%#v err=%v", obs, err)
	}
	if err = p.DeleteWorkload(ctx, ref); err != nil {
		t.Fatal(err)
	}
	absent, err := p.WorkloadAbsent(ctx, ref)
	if err != nil || !absent {
		t.Fatalf("Job absent=%v err=%v", absent, err)
	}
}

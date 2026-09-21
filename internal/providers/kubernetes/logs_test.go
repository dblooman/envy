package kubernetes

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLogBoundsAndPodSelection(t *testing.T) {
	ctx := context.Background()
	runtime, kube, spec := fixture()
	ref, err := runtime.Ensure(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	for i, name := range []string{"old", "new", "newest", "extra"} {
		_, err = kube.CoreV1().Pods(ref.Namespace).Create(ctx, &corev1.Pod{Name: name, Labels: map[string]string{InstallationLabel: "test", CompositionLabel: spec.CompositionID, ComponentLabel: spec.ComponentID}, CreationTimestamp: metav1.NewTime(time.Unix(int64(i), 0)), Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: spec.ComponentID}}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}

	reader := NewLogReader(kube, "test")
	var calls []string
	reader.stream = func(ctx context.Context, ns, pod string, o *corev1.PodLogOptions) (io.ReadCloser, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing read deadline")
		}

		if ns != ref.Namespace || o.Container != "service-b" || *o.TailLines != 7 || *o.SinceSeconds != 60 || !o.Previous || !o.Timestamps || o.Follow {
			t.Fatalf("wrong log options: %+v", o)
		}

		calls = append(calls, pod)
		return io.NopCloser(strings.NewReader("0123456789")), nil
	}
	kube.ClearActions()
	out, err := reader.ReadLogs(ctx, domain.LogTarget{Composition: spec.CompositionID, Component: "service-b", Source: "override", Workload: ref}, domain.LogOptions{TailLines: 7, MaxBytes: 15, SinceSeconds: 60, Previous: true})
	if err != nil || !out.Truncated || out.Partial || len(out.Streams) != 2 || out.Streams[0].Text != "0123456789" || out.Streams[1].Text != "01234" || strings.Join(calls, ",") != "extra,newest" {
		t.Fatalf("log bound/ordering failed: %+v %v calls=%v", out, err, calls)
	}

	if mutations(kube.Actions()) != 0 {
		t.Fatal("log reads mutated resources")
	}
}

func TestLogOwnershipAndSharedBaselineSelection(t *testing.T) {
	ctx := context.Background()
	runtime, kube, spec := fixture()
	ref, err := runtime.Ensure(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	reader := NewLogReader(kube, "test")
	calls := 0
	reader.stream = func(context.Context, string, string, *corev1.PodLogOptions) (io.ReadCloser, error) {
		calls++
		return io.NopCloser(strings.NewReader("baseline log")), nil
	}
	for _, mutate := range []func(*domain.WorkloadRef){func(r *domain.WorkloadRef) { r.Namespace = "other" }, func(r *domain.WorkloadRef) { r.NamespaceUID = "replaced" }, func(r *domain.WorkloadRef) { r.ServiceUID = "replaced" }, func(r *domain.WorkloadRef) { r.DeploymentUID = "replaced" }, func(r *domain.WorkloadRef) { r.OwnershipToken = "wrong" }} {
		bad := ref
		mutate(&bad)
		_, err = reader.ReadLogs(ctx, domain.LogTarget{Composition: spec.CompositionID, Component: "service-b", Source: "override", Workload: bad}, domain.LogOptions{})
		var public *domain.Error
		if !errors.As(err, &public) || public.Code != "conflict" {
			t.Fatalf("unsafe ref accepted %+v: %v", bad, err)
		}
	}

	if calls != 0 {
		t.Fatal("read logs before ownership validation")
	}

	_, err = kube.CoreV1().Services("baseline").Create(ctx, &corev1.Service{Name: "gateway", Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "gateway", "scope": "baseline"}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, scope := range []string{"baseline", "override"} {
		_, err = kube.CoreV1().Pods("baseline").Create(ctx, &corev1.Pod{Name: scope, Labels: map[string]string{"app": "gateway", "scope": scope}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "gateway"}, {Name: "istio-proxy"}}}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}

	out, err := reader.ReadLogs(ctx, domain.LogTarget{Component: "gateway", Source: "shared-baseline", BaselineServiceHost: "gateway.baseline.svc.cluster.local"}, domain.LogOptions{})
	if err != nil || len(out.Streams) != 1 || out.Streams[0].Pod != "baseline" || out.Streams[0].Container != "gateway" {
		t.Fatalf("incorrect baseline selection: %+v %v", out, err)
	}
}

func TestLogPartialErrorsAndIdentityRecheck(t *testing.T) {
	ctx := context.Background()
	runtime, kube, spec := fixture()
	ref, err := runtime.Ensure(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	pod, err := kube.CoreV1().Pods(ref.Namespace).Create(ctx, &corev1.Pod{Name: "pod", Labels: map[string]string{InstallationLabel: "test", CompositionLabel: spec.CompositionID, ComponentLabel: spec.ComponentID}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "service-b"}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	reader := NewLogReader(kube, "test")
	target := domain.LogTarget{Composition: spec.CompositionID, Component: "service-b", Source: "override", Workload: ref}
	reader.stream = func(context.Context, string, string, *corev1.PodLogOptions) (io.ReadCloser, error) {
		return nil, errors.New("backend credential=do-not-expose")
	}
	out, err := reader.ReadLogs(ctx, target, domain.LogOptions{})
	if err != nil || !out.Partial || len(out.Streams) != 1 || out.Streams[0].Error == nil || strings.Contains(out.Streams[0].Error.Message, "credential") {
		t.Fatalf("bad partial error: %+v %v", out, err)
	}

	reader.stream = func(context.Context, string, string, *corev1.PodLogOptions) (io.ReadCloser, error) {
		pod.UID = "replacement"
		_, _ = kube.CoreV1().Pods(ref.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
		return io.NopCloser(strings.NewReader("wrong identity")), nil
	}
	out, err = reader.ReadLogs(ctx, target, domain.LogOptions{})
	if err != nil || !out.Partial || out.Streams[0].Text != "" {
		t.Fatalf("read logs from replaced pod: %+v %v", out, err)
	}
}

func TestApprovedInitContainerLogs(t *testing.T) {
	ctx := context.Background()
	runtime, kube, spec := fixture()
	ref, err := runtime.Ensure(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	_, err = kube.CoreV1().Pods(ref.Namespace).Create(ctx, &corev1.Pod{Name: "initializing", Labels: map[string]string{InstallationLabel: "test", CompositionLabel: spec.CompositionID, ComponentLabel: spec.ComponentID}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: spec.ComponentID}}, InitContainers: []corev1.Container{{Name: "bootstrap"}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	reader := NewLogReader(kube, "test")
	calls := 0
	reader.stream = func(_ context.Context, _, _ string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
		calls++
		if opts.Container != "bootstrap" || !opts.Previous {
			t.Fatalf("wrong selection: %+v", opts)
		}

		return io.NopCloser(strings.NewReader("initialization failed")), nil
	}
	target := domain.LogTarget{Composition: spec.CompositionID, Component: spec.ComponentID, Source: "override", Workload: ref}
	if _, err := reader.ReadLogs(ctx, target, domain.LogOptions{Container: "bootstrap"}); err == nil {
		t.Fatal("unapproved init accepted")
	}

	target.AllowedContainers = []string{"bootstrap"}
	out, err := reader.ReadLogs(ctx, target, domain.LogOptions{Container: "bootstrap", Previous: true})
	if err != nil || len(out.Streams) != 1 || out.Streams[0].Container != "bootstrap" || out.Streams[0].Text != "initialization failed" || calls != 1 {
		t.Fatalf("init logs: %+v %v calls=%d", out, err, calls)
	}
}

func TestInheritedCompositeLogsSelectInstalledApplication(t *testing.T) {
	p, k, b, c := compositePreviewFixture(t)
	ctx := context.Background()
	d, _ := k.AppsV1().Deployments("staging").Get(ctx, "pricing", metav1.GetOptions{})
	_, err := k.CoreV1().Pods("staging").Create(ctx, &corev1.Pod{Name: "baseline", Labels: d.Spec.Template.Labels, Spec: d.Spec.Template.Spec}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	reader := NewLogReader(k, "test").WithPreviewPolicy(p.previewPolicy)
	calls := 0
	reader.stream = func(_ context.Context, ns, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
		calls++
		if ns != "staging" || pod != "baseline" || opts.Container != "app" {
			t.Fatalf("wrong baseline application selection: %s/%s %+v", ns, pod, opts)
		}

		return io.NopCloser(strings.NewReader("shared app log")), nil
	}
	target := domain.LogTarget{Project: b.Project, Baseline: b.ID, BaselineComposite: true, Component: c.ID, Source: "shared-baseline", BaselineServiceHost: b.Components[c.ID].ServiceHost}
	k.ClearActions()
	for _, options := range []domain.LogOptions{{}, {Container: c.ID}} {
		out, err := reader.ReadLogs(ctx, target, options)
		if err != nil || len(out.Streams) != 1 || out.Streams[0].Container != "app" || out.Streams[0].Text != "shared app log" {
			t.Fatalf("inherited logs: %+v %v", out, err)
		}
	}

	for _, container := range []string{"app", "kafka-proxy", "identity-init"} {
		if _, err := reader.ReadLogs(ctx, target, domain.LogOptions{Container: container}); err == nil {
			t.Fatalf("caller-selected source container %q bypassed logical application selection", container)
		}
	}

	other := target
	other.Baseline = "other-baseline"
	if _, err := reader.ReadLogs(ctx, other, domain.LogOptions{}); err == nil {
		t.Fatal("baseline log policy escaped exact scope")
	}

	if calls != 2 || mutations(k.Actions()) != 0 {
		t.Fatal("invalid selections read logs or mutated cluster state")
	}
}

func TestInheritedCompositeLogsDoNotGuessMissingApplication(t *testing.T) {
	p, k, b, c := compositePreviewFixture(t)
	ctx := context.Background()
	_, err := k.CoreV1().Pods("staging").Create(ctx, &corev1.Pod{Name: "proxy-only", Labels: map[string]string{"app": "pricing"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "kafka-proxy"}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	reader := NewLogReader(k, "test").WithPreviewPolicy(p.previewPolicy)
	reader.stream = func(context.Context, string, string, *corev1.PodLogOptions) (io.ReadCloser, error) {
		t.Fatal("supporting container guessed as baseline application")
		return nil, nil
	}
	target := domain.LogTarget{Project: b.Project, Baseline: b.ID, BaselineComposite: true, Component: c.ID, Source: "shared-baseline", BaselineServiceHost: b.Components[c.ID].ServiceHost}
	out, err := reader.ReadLogs(ctx, target, domain.LogOptions{})
	if err != nil || len(out.Streams) != 0 {
		t.Fatalf("missing application should not select another container: %+v %v", out, err)
	}
}

package application

import (
	"context"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type diagnosticsRepo struct {
	Repository
	c domain.Composition
}

func (r diagnosticsRepo) Get(context.Context, string) (domain.Composition, error) { return r.c, nil }
func (r diagnosticsRepo) Component(context.Context, string, string) (domain.Component, error) {
	return domain.Component{}, nil
}
func (r diagnosticsRepo) Baseline(context.Context, string, string) (domain.Baseline, error) {
	return domain.Baseline{Revision: "1", Components: map[string]domain.BaselineBinding{"gateway": {ServiceHost: "gateway.baseline.svc.cluster.local"}}}, nil
}

type logCapture struct {
	target domain.LogTarget
	calls  int
}

func (l *logCapture) ReadLogs(_ context.Context, t domain.LogTarget, _ domain.LogOptions) (domain.ComponentLogs, error) {
	l.target = t
	l.calls++
	return domain.ComponentLogs{CompositionFiltered: true}, nil
}
func TestLogsResolveLogicalScopeAndLabelSharedOutput(t *testing.T) {
	c := domain.Composition{ID: "abc", Project: "demo", Baseline: "staging", BaselineRevision: "1", Phase: domain.PhaseReady, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "v2"}}, Components: map[string]domain.ComponentObservation{"gateway": {}, "service-b": {}}, Runtime: domain.RuntimeState{Workload: domain.WorkloadRef{Deployment: "service-b", Service: "service-b", Namespace: "envy-abc", OwnershipToken: "owner"}}}
	reader := &logCapture{}
	s := New(diagnosticsRepo{c: c}, Config{Logs: reader})
	out, err := s.Logs(context.Background(), "abc", "gateway", domain.LogOptions{})
	if err != nil || out.Source != "shared-baseline" || out.CompositionFiltered || !strings.Contains(out.Message, "other compositions") || reader.target.BaselineServiceHost != "gateway.baseline.svc.cluster.local" || out.Streams == nil {
		t.Fatalf("shared scope missing: %+v %v", out, err)
	}

	out, err = s.Logs(context.Background(), "abc", "service-b", domain.LogOptions{})
	if err != nil || out.Source != "override" || reader.target.Workload.OwnershipToken != "owner" || out.CompositionFiltered {
		t.Fatalf("override scope missing: %+v %v", out, err)
	}

	for _, component := range []string{"postgres", "istio-proxy", "../gateway"} {
		if _, err = s.Logs(context.Background(), "abc", component, domain.LogOptions{}); err == nil {
			t.Fatalf("unknown component accepted: %s", component)
		}
	}

	if _, err = s.Logs(context.Background(), "abc", "gateway", domain.LogOptions{MaxBytes: 262145}); err == nil {
		t.Fatal("oversized logs accepted")
	}

	c.Phase = domain.PhaseDestroyed
	s = New(diagnosticsRepo{c: c}, Config{Logs: reader})
	if _, err = s.Logs(context.Background(), "abc", "gateway", domain.LogOptions{}); err == nil {
		t.Fatal("destroyed logs accepted")
	}

	if reader.calls != 2 {
		t.Fatal("invalid request reached log provider")
	}
}

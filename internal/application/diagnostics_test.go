package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type diagnosticsRepo struct {
	Repository
	c               domain.Composition
	profile         domain.Component
	verification    []domain.VerificationEvidence
	verificationErr error
}

func (r diagnosticsRepo) Get(context.Context, string) (domain.Composition, error) { return r.c, nil }

func (r diagnosticsRepo) Component(context.Context, string, string) (domain.Component, error) {
	return r.profile, nil
}

func (r diagnosticsRepo) Baseline(context.Context, string, string) (domain.Baseline, error) {
	return domain.Baseline{Revision: "1", Components: map[string]domain.BaselineBinding{"gateway": {ServiceHost: "gateway.baseline.svc.cluster.local"}}}, nil
}

func (r diagnosticsRepo) Verification(context.Context, string, string, int) (domain.VerificationPage, error) {
	return domain.VerificationPage{Items: r.verification}, r.verificationErr
}

func TestVerificationFreshnessDoesNotUpgradeHistoricalEvidence(t *testing.T) {
	c := domain.Composition{ID: "abc", Generation: 2, BaselineObservation: &domain.BaselineObservation{Installation: "synthetic", Project: "demo", Baseline: "staging", State: "current", Fingerprint: "observed-two", ObservedAt: time.Now().UTC()}, Runtime: domain.RuntimeState{Plan: &domain.ResolvedPlan{}}}
	contract := domain.VerificationContractFingerprint(domain.VerificationContract{})
	base := domain.VerificationEvidence{Composition: c.ID, Generation: 2, Outcome: "passed", BaselineFingerprint: "observed-two", BaselineScope: "synthetic/demo/staging", ContractFingerprint: contract}
	repo := diagnosticsRepo{c: c, verification: []domain.VerificationEvidence{base, {Composition: c.ID, Generation: 1, Outcome: "passed", BaselineFingerprint: "observed-one", ContractFingerprint: contract}, {Composition: c.ID, Generation: 2, Outcome: "passed"}}}
	s := New(repo, Config{})
	page, err := s.Verification(context.Background(), c.ID, "", 20)
	if err != nil || page.Items[0].Freshness != "current" || page.Items[1].Freshness != "stale" || page.Items[2].Freshness != "unknown_coverage" {
		t.Fatalf("historical coverage was upgraded: %+v %v", page, err)
	}

	repo.c.BaselineObservation.State = "unavailable"
	s = New(repo, Config{})
	page, err = s.Verification(context.Background(), c.ID, "", 20)
	if err != nil || page.Items[0].Freshness != "unavailable" {
		t.Fatalf("observation outage retained current proof: %+v %v", page, err)
	}
}

func TestDiagnosisUsesLatestRecordedEvidenceAndKeepsUnknownCoverage(t *testing.T) {
	c := domain.Composition{ID: "synthetic", Generation: 2, Phase: domain.PhaseReady, BaselineObservation: &domain.BaselineObservation{Installation: "synthetic", Project: "demo", Baseline: "staging", State: "current", Fingerprint: "new", ObservedAt: time.Now().UTC()}}
	latest := domain.VerificationEvidence{ID: "2", Generation: 2, Outcome: "passed", BaselineFingerprint: "old", BaselineScope: "synthetic/demo/staging", ContractFingerprint: "contract"}
	s := New(diagnosticsRepo{c: c, verification: []domain.VerificationEvidence{latest}}, Config{})
	d, err := s.Diagnosis(context.Background(), c.ID)
	if err != nil || d.Verification != "stale" || d.State != "blocked" || d.EvidenceID != "2" {
		t.Fatalf("stale evidence promoted: %+v %v", d, err)
	}

	s = New(diagnosticsRepo{c: c}, Config{})
	d, err = s.Diagnosis(context.Background(), c.ID)
	if err != nil || d.Verification != "unknown_coverage" || len(d.Notes) == 0 {
		t.Fatalf("missing evidence presented as proven: %+v %v", d, err)
	}
}

func TestDiagnosisDoesNotClaimHealthyWhenEvidenceReadFails(t *testing.T) {
	c := domain.Composition{ID: "synthetic", Phase: domain.PhaseReady}
	s := New(diagnosticsRepo{c: c, verificationErr: errors.New("database unavailable")}, Config{})
	d, err := s.Diagnosis(context.Background(), c.ID)
	if err != nil || d.State != "unknown" || d.Verification != "unavailable" || len(d.Notes) == 0 || d.Notes[len(d.Notes)-1].Code != "verification_history_unavailable" {
		t.Fatalf("unreadable evidence presented as healthy: %+v %v", d, err)
	}
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

func TestLogsRequireCapturedCompositeContainer(t *testing.T) {
	c := domain.Composition{ID: "abc", Project: "demo", Baseline: "staging", BaselineRevision: "1", Phase: domain.PhaseReady, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "v2"}}, Components: map[string]domain.ComponentObservation{"gateway": {}, "service-b": {}}}
	c.Runtime.Plan = &domain.ResolvedPlan{Previews: map[string]domain.PreviewSnapshot{"service-b": {CompositePolicy: &domain.CompositePreviewPolicy{Sidecars: []string{"proxy"}, InitContainers: []string{"bootstrap"}, NativeSidecars: []string{"database"}}}}}
	reader := &logCapture{}
	s := New(diagnosticsRepo{c: c}, Config{Logs: reader})
	for _, name := range []string{"service-b", "proxy", "bootstrap", "database"} {
		if _, err := s.Logs(context.Background(), "abc", "service-b", domain.LogOptions{Container: name}); err != nil {
			t.Fatalf("approved container %s: %v", name, err)
		}
	}

	for _, component := range []string{"gateway", "service-b"} {
		if _, err := s.Logs(context.Background(), "abc", component, domain.LogOptions{Container: "unapproved"}); err == nil {
			t.Fatal("unapproved logs accepted")
		}
	}

	if _, err := s.Logs(context.Background(), "abc", "gateway", domain.LogOptions{Container: "proxy"}); err == nil {
		t.Fatal("override policy used for inherited logs")
	}

	if reader.calls != 4 {
		t.Fatal("invalid selection reached log provider")
	}
}

func TestInheritedCompositeLogsCarryExactPolicyScope(t *testing.T) {
	c := domain.Composition{ID: "abc", Project: "shop", Baseline: "staging", BaselineRevision: "1", Phase: domain.PhaseReady, Components: map[string]domain.ComponentObservation{"gateway": {}}}
	reader := &logCapture{}
	s := New(diagnosticsRepo{c: c, profile: domain.Component{Profile: "deployment-composite"}}, Config{Logs: reader})
	out, err := s.Logs(context.Background(), c.ID, "gateway", domain.LogOptions{})
	if err != nil || out.Source != "shared-baseline" || reader.target.Project != "shop" || reader.target.Baseline != "staging" || !reader.target.BaselineComposite {
		t.Fatalf("missing scoped composite selection: target=%+v output=%+v err=%v", reader.target, out, err)
	}

	if _, err := s.Logs(context.Background(), c.ID, "gateway", domain.LogOptions{Container: "proxy"}); err == nil || reader.calls != 1 {
		t.Fatal("inherited caller selected supporting container")
	}
}

package application

import (
	"context"
	"encoding/json"
	"github.com/dblooman/envy/internal/domain"
	"strings"
	"testing"
)

type previewRepo struct {
	createRepository
	profile *domain.PreviewProfile
	current domain.Composition
}

func (r *previewRepo) PreviewProfile(context.Context, string, string, string) (*domain.PreviewProfile, error) {
	return r.profile, nil
}
func (r *previewRepo) ApprovePreview(_ context.Context, p domain.PreviewProfile, expected int64) (domain.PreviewProfile, error) {
	p.Revision = expected + 1
	r.profile = &p
	return p, nil
}
func (r *previewRepo) Get(context.Context, string) (domain.Composition, error) { return r.current, nil }
func (r *previewRepo) Update(_ context.Context, _ string, req domain.UpdateRequest, _ string) (domain.Composition, error) {
	r.current.Runtime.Plan = req.Plan
	r.current.Overrides = req.Overrides
	r.current.Generation++
	r.current.RefreshPreviewProvenance()
	return r.current, nil
}

type previewDiscoverer struct {
	report domain.PreviewReport
	calls  int
}

func (p *previewDiscoverer) DiscoverPreview(context.Context, domain.Baseline, domain.Component, domain.PreviewSelection) (domain.PreviewReport, error) {
	p.calls++
	return p.report, nil
}
func TestPreviewApprovalAndCapturedUpdates(t *testing.T) {
	ctx := context.Background()
	r := &previewRepo{}
	discovery := &previewDiscoverer{report: domain.PreviewReport{Inspection: "inspected", Contract: "contract", Source: domain.PreviewSource{UID: "deployment"}, Selection: domain.PreviewSelection{Deployment: "app"}, Snapshot: domain.PreviewSnapshot{TemplateJSON: `{"spec":{"containers":[{"image":"v1"}]}}`, Source: domain.PreviewSource{UID: "deployment"}, Contract: "contract"}}}
	s := New(r, Config{PreviewDiscoverer: discovery})
	a := domain.PreviewApproval{Inspection: "stale", ConfirmConnectivity: true}
	if _, err := s.ApprovePreview(ctx, "demo", "staging", "service-b", a); err == nil {
		t.Fatal("stale approval accepted")
	}
	a.Inspection = "inspected"
	a.ConfirmConnectivity = false
	if _, err := s.ApprovePreview(ctx, "demo", "staging", "service-b", a); err == nil {
		t.Fatal("unconfirmed connectivity accepted")
	}
	a.ConfirmConnectivity = true
	profile, err := s.ApprovePreview(ctx, "demo", "staging", "service-b", a)
	if err != nil || profile.Revision != 1 {
		t.Fatal(err)
	}
	req := validRequest()
	req.Overrides["service-b"] = domain.ComponentOverride{Image: "example/app@sha256:" + strings.Repeat("a", 64)}
	req.ExpectedPreviewRevisions = map[string]int64{"service-b": 1}
	composition, err := s.Create(ctx, req, "create")
	if err != nil {
		t.Fatal(err)
	}
	if composition.PreviewProfiles["service-b"].Revision != 1 {
		t.Fatal("missing provenance")
	}
	r.current = composition
	calls := discovery.calls
	discovery.report.Contract = "incompatible"
	discovery.report.Snapshot.TemplateJSON = "new configuration"
	updated, err := s.Update(ctx, composition.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: req.Overrides})
	if err != nil {
		t.Fatal(err)
	}
	if discovery.calls != calls || updated.Runtime.Plan.Previews["service-b"].TemplateJSON != composition.Runtime.Plan.Previews["service-b"].TemplateJSON {
		t.Fatal("image update recaptured configuration")
	}
	if _, err := s.Create(ctx, req, "another"); err == nil {
		t.Fatal("contract drift accepted")
	}
	public, _ := json.Marshal(updated)
	if strings.Contains(string(public), "template_json") {
		t.Fatal("execution snapshot leaked into public composition")
	}
}

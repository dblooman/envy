package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
)

type previewFake struct {
	*fakeService
	calls int
}

func (p *previewFake) DiscoverPreview(_ context.Context, project, baseline, component string, sel domain.PreviewSelection) (domain.PreviewReport, error) {
	p.calls++
	return domain.PreviewReport{Selection: sel, Inspection: project + "/" + baseline + "/" + component, Snapshot: domain.PreviewSnapshot{TemplateJSON: "internal-only"}}, nil
}

func (p *previewFake) ApprovePreview(_ context.Context, project, baseline, component string, a domain.PreviewApproval) (domain.PreviewProfile, error) {
	p.calls++
	if !a.ConfirmConnectivity {
		return domain.PreviewProfile{}, domain.Validation("confirmation required")
	}

	return domain.PreviewProfile{Project: project, Baseline: baseline, Component: component, Revision: 1}, nil
}

func (p *previewFake) InspectPreview(_ context.Context, project, baseline, component string) (domain.PreviewProfile, error) {
	p.calls++
	return domain.PreviewProfile{Project: project, Baseline: baseline, Component: component, Revision: 1}, nil
}

func TestPreviewHTTPAndClient(t *testing.T) {
	p := &previewFake{fakeService: &fakeService{}}
	h := NewHandler(p, "secret", nil)
	path := "/v1/projects/shop/baselines/staging/components/pricing/preview-profile"
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{{"POST", path + "/discover", `{}`, "", 401}, {"POST", path + "/discover", `{"secret_data":"forbidden"}`, "secret", 400}, {"POST", path + "/approve", `{"confirm_connectivity":false}`, "secret", 400}, {"GET", path, "", "secret", 200}} {
		w := request(h, tc.method, tc.path, tc.body, tc.token)
		if w.Code != tc.status {
			t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
		}
	}

	server := httptest.NewServer(h)
	defer server.Close()
	c, err := client.New(server.URL, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}

	report, err := c.DiscoverPreview(context.Background(), "shop", "staging", "pricing", domain.PreviewSelection{Deployment: "pricing"})
	if err != nil || report.Inspection != "shop/staging/pricing" || report.Snapshot.TemplateJSON != "" {
		t.Fatalf("client discovery %+v %v", report, err)
	}

	approved, err := c.ApprovePreview(context.Background(), "shop", "staging", "pricing", domain.PreviewApproval{Inspection: report.Inspection, ConfirmConnectivity: true})
	if err != nil || approved.Revision != 1 {
		t.Fatal(err)
	}

	inspected, err := c.InspectPreview(context.Background(), "shop", "staging", "pricing")
	if err != nil || inspected.Revision != 1 {
		t.Fatal(err)
	}
}

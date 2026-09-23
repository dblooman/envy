package application

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestPlanCreateSharesCreateChecksWithoutAllocating(t *testing.T) {
	r := &createRepository{}
	s := New(r, Config{Installation: "synthetic", PreviewBaseURL: "http://envy.localhost:18080"})
	plan, err := s.PlanCreate(context.Background(), validRequest())
	if err != nil || !plan.Ready || len(plan.Blockers) != 0 || r.received.ID != "" {
		t.Fatalf("planning allocated or failed: %+v %v", plan, err)
	}

	if plan.BaselineRevision != "revision-42" || plan.Inherited["gateway"].Image != "envy/gateway:v1" || len(plan.Selected) != 1 || plan.Selected[0].Image != "envy/service-b:v2" {
		t.Fatalf("plan lost selected or inherited state: %+v", plan)
	}

	if len(plan.Cautions) < 2 || plan.Cautions[0].Code != "routing_proof_unknown" {
		t.Fatalf("HTTP reachability was overstated: %+v", plan.Cautions)
	}

	if plan.Selected[0].Immutable {
		t.Fatal("mutable direct image was reported immutable")
	}

	req := validRequest()
	req.ExpectedBaselineRevision = "old"
	plan, err = s.PlanCreate(context.Background(), req)
	if err != nil || plan.Ready || len(plan.Blockers) != 1 || plan.Blockers[0].Code != "stale_approval" {
		t.Fatalf("stale plan lacks blocker: %+v %v", plan, err)
	}

	if _, err := s.Create(context.Background(), req, ""); err == nil {
		t.Fatal("create accepted a stale baseline")
	}
}

func TestPlanningBlockersCarryActions(t *testing.T) {
	for _, tc := range []struct{ message, code string }{
		{"forbidden reading deployment", "permission_denied"},
		{"unsupported volume", "unsupported_shape"},
		{"deployment-derived component requires preview discovery and approval", "approval_required"},
	} {
		got := planningBlocker(domain.Validation(tc.message), "api", "source cluster")
		if got.Code != tc.code || got.Component != "api" || got.Source == "" || got.NextAction == "" {
			t.Fatalf("blocker not actionable: %+v", got)
		}
	}
}

func TestPlanRejectsInvalidEndpointConfigurationBeforeCreation(t *testing.T) {
	r := &createRepository{}
	s := New(r, Config{PreviewBaseURL: "https://user:private@preview.example.test"})
	plan, err := s.PlanCreate(context.Background(), validRequest())
	if err != nil || plan.Ready || len(plan.Blockers) != 1 || r.received.ID != "" {
		t.Fatalf("invalid destination was presented as ready: %+v %v", plan, err)
	}
}

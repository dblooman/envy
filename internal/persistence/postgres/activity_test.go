package postgres

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
)

func TestCompositionActivityAndRevisionAttribution(t *testing.T) {
	s := testStore(t)
	ctx := domain.WithRequestIdentity(context.Background(), domain.RequestIdentity{Principal: domain.Principal{Kind: "service", ID: "agent-one", DisplayName: "Agent one"}, Channel: "mcp", Task: "task-42"})
	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("audited"), "audit-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Create(ctx, request("audited"), "audit-key"); err != nil {
		t.Fatal(err)
	}
	c.Phase = domain.PhaseReady
	c.ObservedGeneration = c.Generation
	c.LatestOperation.Status = "succeeded"
	if err = s.SaveObservation(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	human := domain.WithRequestIdentity(context.Background(), domain.RequestIdentity{Principal: domain.Principal{Kind: "human", ID: "user-123", DisplayName: "Alice"}, Channel: "web"})
	updated, err := app.Update(human, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "envy/service-b:v3"}}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LatestOperation.Initiator == nil || updated.LatestOperation.Initiator.ID != "user-123" {
		t.Fatalf("initiator not persisted: %+v", updated.LatestOperation)
	}
	activity, err := s.Activity(context.Background(), domain.ActivityFilter{ResourceType: "composition", ResourceID: c.ID, Outcome: "accepted", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Items) != 2 || activity.Items[0].Actor.ID != "user-123" || activity.Items[1].Actor.ID != "agent-one" || activity.Items[1].Task != "task-42" {
		t.Fatalf("activity=%+v", activity.Items)
	}
	outcomes, err := s.Activity(context.Background(), domain.ActivityFilter{ResourceType: "composition", ResourceID: c.ID, Outcome: "succeeded", Limit: 20})
	if err != nil || len(outcomes.Items) != 1 || outcomes.Items[0].Actor.ID != "reconciler" {
		t.Fatalf("outcomes=%+v err=%v", outcomes.Items, err)
	}
	revisions, err := s.Revisions(context.Background(), c.ID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions.Items) != 2 || revisions.Items[0].Generation != 1 || revisions.Items[1].Overrides["service-b"].Image != "envy/service-b:v3" {
		t.Fatalf("revisions=%+v", revisions.Items)
	}
}

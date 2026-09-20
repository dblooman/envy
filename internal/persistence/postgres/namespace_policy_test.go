package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/application"
)

func TestNamespacePolicyMigrationAndDrain(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.BindInstallation(ctx, "legacy", "istio"); err != nil {
		t.Fatal(err)
	}

	mode, err := s.BindNamespacePolicy(ctx, "", "a")
	if err != nil || mode != "legacy" {
		t.Fatalf("legacy default: %s %v", mode, err)
	}

	app := application.New(s, application.Config{})
	if _, err = app.Create(ctx, request("policy"), "policy"); err != nil {
		t.Fatal(err)
	}

	if _, err = s.BindNamespacePolicy(ctx, "isolated", "b"); err == nil {
		t.Fatal("active previews allowed policy change")
	}

	if err = s.CheckNamespacePolicy(ctx, "legacy", "a"); err != nil {
		t.Fatal(err)
	}
}

func TestFreshNamespacePolicyAndLeadershipFence(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "TRUNCATE projects CASCADE"); err != nil {
		t.Fatal(err)
	}

	if err := s.BindInstallation(ctx, "fresh", "istio"); err != nil {
		t.Fatal(err)
	}

	mode, err := s.BindNamespacePolicy(ctx, "", "a")
	if err != nil || mode != "isolated" {
		t.Fatalf("fresh default: %s %v", mode, err)
	}

	lease, err := s.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = s.BindNamespacePolicy(ctx, "legacy", "a"); err == nil {
		t.Fatal("live leader allowed policy change")
	}

	if err = lease.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = s.BindNamespacePolicy(ctx, "legacy", "b"); err != nil {
		t.Fatal(err)
	}

	if err = s.CheckNamespacePolicy(ctx, "isolated", "a"); err == nil {
		t.Fatal("stale policy accepted")
	}
}

func TestDesiredNotificationsAfterCommit(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	messages := make(chan string, 10)
	done := make(chan struct{})
	go func() { defer close(done); s.ListenChanges(ctx, func(id string) { messages <- id }) }()
	select {
	case <-messages:
	case <-ctx.Done():
		t.Fatal("listener failed")
	}

	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("notify"), "notify")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case id := <-messages:
		if id != c.ID {
			t.Fatalf("wrong notification %s", id)
		}
	case <-ctx.Done():
		t.Fatal("missing desired notification")
	}

	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}

	select {
	case id := <-messages:
		t.Fatalf("observation notified: %s", id)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	<-done
}

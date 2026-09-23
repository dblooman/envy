package postgres

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestOnboardingDraftPersistenceScopesAndConflicts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	draft := domain.OnboardingDraft{Project: "shop", Stage: 1, Configuration: domain.CatalogManifest{Project: domain.Project{ID: "shop"}}}
	saved, err := s.SaveOnboardingDraft(ctx, "one", "human:alice", draft)
	if err != nil || saved.Revision != 1 {
		t.Fatalf("save: %+v %v", saved, err)
	}

	got, err := s.GetOnboardingDraft(ctx, "one", "shop", "human:alice")
	if err != nil || got.Revision != 1 || got.Stage != 1 {
		t.Fatalf("reload: %+v %v", got, err)
	}

	restarted, err := Open(ctx, s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	got, err = restarted.GetOnboardingDraft(ctx, "one", "shop", "human:alice")
	if err != nil || got.Revision != 1 || got.Stage != 1 {
		t.Fatalf("restart lost draft: %+v %v", got, err)
	}

	for _, scope := range [][2]string{{"two", "human:alice"}, {"one", "human:bob"}} {
		if _, err := s.GetOnboardingDraft(ctx, scope[0], "shop", scope[1]); err == nil {
			t.Fatalf("draft leaked to %v", scope)
		}
	}

	if _, err := s.SaveOnboardingDraft(ctx, "one", "human:alice", draft); err == nil {
		t.Fatal("stale revision accepted")
	}

	saved.Stage = 0
	saved, err = s.SaveOnboardingDraft(ctx, "one", "human:alice", saved)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("update: %+v %v", saved, err)
	}

	if err := s.DeleteOnboardingDraft(ctx, "one", "shop", "human:alice", 1); err == nil {
		t.Fatal("stale deletion accepted")
	}

	if err := s.DeleteOnboardingDraft(ctx, "one", "shop", "human:alice", 2); err != nil {
		t.Fatal(err)
	}
}

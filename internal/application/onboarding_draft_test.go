package application

import (
	"context"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type draftFixture struct {
	Repository
	lastInstallation, lastAuthor string
	saved                        domain.OnboardingDraft
}

func (f *draftFixture) GetOnboardingDraft(_ context.Context, installation, _, author string) (domain.OnboardingDraft, error) {
	f.lastInstallation, f.lastAuthor = installation, author
	return f.saved, nil
}

func (f *draftFixture) SaveOnboardingDraft(_ context.Context, installation, author string, draft domain.OnboardingDraft) (domain.OnboardingDraft, error) {
	f.lastInstallation, f.lastAuthor, f.saved = installation, author, draft
	return draft, nil
}

func (f *draftFixture) DeleteOnboardingDraft(_ context.Context, installation, _, author string, _ int64) error {
	f.lastInstallation, f.lastAuthor = installation, author
	return nil
}

func TestDraftPreparationRequiresAuthenticatedAuthorAndRejectsEnvironmentValues(t *testing.T) {
	store := &draftFixture{}
	s := New(store, Config{Installation: "one"})
	draft := domain.OnboardingDraft{Project: "shop", Configuration: domain.CatalogManifest{Project: domain.Project{ID: "shop"}}}
	if _, err := s.SaveOnboardingDraft(context.Background(), draft); err == nil {
		t.Fatal("anonymous draft accepted")
	}

	ctx := domain.WithRequestIdentity(context.Background(), domain.RequestIdentity{Principal: domain.Principal{Kind: "human", ID: "alice"}})
	draft.Configuration.Components = []domain.Component{{ID: "api", Project: "shop", Env: map[string]string{"TOKEN": "private"}}}
	if _, err := s.SaveOnboardingDraft(ctx, draft); err == nil {
		t.Fatal("environment value persisted")
	}

	draft.Configuration.Components[0].Env = nil
	if _, err := s.SaveOnboardingDraft(ctx, draft); err != nil {
		t.Fatal(err)
	}

	if store.lastInstallation != "one" || store.lastAuthor != "human:alice" {
		t.Fatalf("wrong draft scope: %s/%s", store.lastInstallation, store.lastAuthor)
	}
}

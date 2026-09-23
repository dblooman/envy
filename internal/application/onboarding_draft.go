package application

import (
	"context"

	"github.com/dblooman/envy/internal/domain"
)

type draftRepository interface {
	GetOnboardingDraft(context.Context, string, string, string) (domain.OnboardingDraft, error)
	SaveOnboardingDraft(context.Context, string, string, domain.OnboardingDraft) (domain.OnboardingDraft, error)
	DeleteOnboardingDraft(context.Context, string, string, string, int64) error
}

func (s *Service) draftStore(ctx context.Context) (draftRepository, string, error) {
	store, ok := s.store.(draftRepository)
	if !ok || s.cfg.Installation == "" {
		return nil, "", &domain.Error{Code: "unavailable", Message: "onboarding drafts are unavailable"}
	}

	principal := domain.RequestIdentityFromContext(ctx).Principal
	if principal.ID == "" || principal.Kind == "" || principal.Kind == "unknown" {
		return nil, "", &domain.Error{Code: "unauthorized", Message: "an authenticated author is required for drafts"}
	}

	return store, principal.Kind + ":" + principal.ID, nil
}

func (s *Service) GetOnboardingDraft(ctx context.Context, project string) (domain.OnboardingDraft, error) {
	if !domain.ValidCatalogID(project) {
		return domain.OnboardingDraft{}, domain.Validation("invalid project ID")
	}

	store, author, err := s.draftStore(ctx)
	if err != nil {
		return domain.OnboardingDraft{}, err
	}

	return store.GetOnboardingDraft(ctx, s.cfg.Installation, project, author)
}

func (s *Service) SaveOnboardingDraft(ctx context.Context, draft domain.OnboardingDraft) (domain.OnboardingDraft, error) {
	if err := draft.Validate(); err != nil {
		return domain.OnboardingDraft{}, err
	}

	store, author, err := s.draftStore(ctx)
	if err != nil {
		return domain.OnboardingDraft{}, err
	}

	return store.SaveOnboardingDraft(ctx, s.cfg.Installation, author, draft)
}

func (s *Service) DeleteOnboardingDraft(ctx context.Context, project string, revision int64) error {
	if !domain.ValidCatalogID(project) || revision < 1 {
		return domain.Validation("draft deletion requires a valid project and revision")
	}

	store, author, err := s.draftStore(ctx)
	if err != nil {
		return err
	}

	return store.DeleteOnboardingDraft(ctx, s.cfg.Installation, project, author, revision)
}

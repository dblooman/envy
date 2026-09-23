package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetOnboardingDraft(ctx context.Context, installation, project, author string) (domain.OnboardingDraft, error) {
	var draft domain.OnboardingDraft
	var body []byte
	err := s.pool.QueryRow(ctx, `SELECT body, revision, updated_at FROM onboarding_drafts WHERE installation_id=$1 AND project=$2 AND author=$3`, installation, project, author).Scan(&body, &draft.Revision, &draft.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return draft, domain.NotFound("onboarding draft not found")
	}

	if err != nil {
		return draft, unavailable("read onboarding draft")
	}

	revision, updated := draft.Revision, draft.UpdatedAt
	if err := json.Unmarshal(body, &draft); err != nil {
		return domain.OnboardingDraft{}, unavailable("decode onboarding draft")
	}

	draft.Project, draft.Revision, draft.UpdatedAt = project, revision, updated
	return draft, nil
}

func (s *Store) SaveOnboardingDraft(ctx context.Context, installation, author string, draft domain.OnboardingDraft) (domain.OnboardingDraft, error) {
	requested := draft.Revision
	draft.UpdatedAt = time.Time{}
	data, err := json.Marshal(draft)
	if err != nil {
		return domain.OnboardingDraft{}, domain.Validation("invalid onboarding draft")
	}

	if len(data) > 64<<10 {
		return domain.OnboardingDraft{}, domain.Validation("onboarding draft must be at most 64 KiB")
	}

	if requested == 0 {
		err = s.pool.QueryRow(ctx, `INSERT INTO onboarding_drafts (installation_id,project,author,revision,body)
		 VALUES ($1,$2,$3,1,$4::jsonb) ON CONFLICT DO NOTHING
		 RETURNING revision,updated_at`, installation, draft.Project, author, data).Scan(&draft.Revision, &draft.UpdatedAt)
	} else {
		err = s.pool.QueryRow(ctx, `UPDATE onboarding_drafts SET revision=revision+1,body=$1::jsonb,updated_at=now()
		 WHERE installation_id=$2 AND project=$3 AND author=$4 AND revision=$5
		 RETURNING revision,updated_at`, data, installation, draft.Project, author, requested).Scan(&draft.Revision, &draft.UpdatedAt)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OnboardingDraft{}, &domain.Error{Code: "conflict", Message: "onboarding draft revision changed; reload before saving"}
	}

	if err != nil {
		return domain.OnboardingDraft{}, unavailable("save onboarding draft")
	}

	return draft, nil
}

func (s *Store) DeleteOnboardingDraft(ctx context.Context, installation, project, author string, revision int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM onboarding_drafts WHERE installation_id=$1 AND project=$2 AND author=$3 AND revision=$4`, installation, project, author, revision)
	if err != nil {
		return unavailable("delete onboarding draft")
	}

	if tag.RowsAffected() == 0 {
		return &domain.Error{Code: "conflict", Message: "onboarding draft revision changed or draft does not exist"}
	}

	return nil
}

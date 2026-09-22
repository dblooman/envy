package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres/sqlc"
)

func saveVerification(ctx context.Context, q *sqlc.Queries, c domain.Composition) error {
	if c.PendingVerification == nil || c.PendingVerification.Generation != c.Generation {
		return nil
	}

	next := *c.PendingVerification
	rows, err := q.LatestVerification(ctx, c.ID)
	if err != nil {
		return err
	}

	if len(rows) > 0 {
		var old domain.VerificationEvidence
		if err = json.Unmarshal(rows[0].Body, &old); err != nil {
			return err
		}

		first, last := old.FirstCheckedAt, old.LastCheckedAt
		old.FirstCheckedAt, old.LastCheckedAt = time.Time{}, time.Time{}
		comparison := next
		comparison.FirstCheckedAt, comparison.LastCheckedAt = time.Time{}, time.Time{}
		if reflect.DeepEqual(old, comparison) {
			next.FirstCheckedAt = first
			if next.LastCheckedAt.Before(last) {
				next.LastCheckedAt = last
			}

			body, err := json.Marshal(next)
			if err != nil {
				return err
			}

			return q.UpdateVerification(ctx, sqlc.UpdateVerificationParams{ID: rows[0].ID, Body: body})
		}
	}

	body, err := json.Marshal(next)
	if err != nil {
		return err
	}

	return q.InsertVerification(ctx, sqlc.InsertVerificationParams{CompositionID: c.ID, Body: body})
}

func (s *Store) Verification(ctx context.Context, id, after string, limit int) (domain.VerificationPage, error) {
	page := domain.VerificationPage{Items: []domain.VerificationEvidence{}}
	cursor, err := domain.EventCursor(after)
	if err != nil {
		return page, err
	}

	if limit < 1 || limit > 100 {
		return page, domain.Validation("limit must be between 1 and 100")
	}

	if _, err = s.Get(ctx, id); err != nil {
		return page, err
	}

	rows, err := s.queries.ListVerification(ctx, sqlc.ListVerificationParams{CompositionID: id, Before: cursor, Limit: int32(limit + 1)})
	if err != nil {
		return page, err
	}

	for _, row := range rows {
		var item domain.VerificationEvidence
		if err = json.Unmarshal(row.Body, &item); err != nil {
			return page, err
		}

		item.ID = strconv.FormatInt(row.ID, 10)
		page.Items = append(page.Items, item)
	}

	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = page.Items[limit-1].ID
	}

	return page, nil
}

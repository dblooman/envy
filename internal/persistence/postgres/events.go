package postgres

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres/sqlc"
)

func (s *Store) Events(ctx context.Context, id, after string, limit int) (domain.EventsPage, error) {
	page := domain.EventsPage{Items: []domain.LifecycleEvent{}}
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
	rows, err := s.queries.ListLifecycleEvents(ctx, sqlc.ListLifecycleEventsParams{
		CompositionID: id,
		ID:            cursor,
		Limit:         int32(limit + 1),
	})
	if err != nil {
		return page, unavailable("list lifecycle events")
	}
	for _, row := range rows {
		var event domain.LifecycleEvent
		if err = json.Unmarshal(row.Body, &event); err != nil {
			return page, unavailable("decode lifecycle event")
		}
		event.ID = strconv.FormatInt(row.ID, 10)
		event.OccurredAt = row.OccurredAt.Time
		event.Type = row.Kind
		page.Items = append(page.Items, event)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = page.Items[limit-1].ID
	}
	return page, nil
}

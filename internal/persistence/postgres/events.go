package postgres

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/dblooman/envy/internal/domain"
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
	rows, err := s.pool.Query(ctx, "SELECT id,occurred_at,kind,body FROM lifecycle_events WHERE composition_id=$1 AND id>$2 ORDER BY id LIMIT $3", id, cursor, limit+1)
	if err != nil {
		return page, unavailable("list lifecycle events")
	}
	defer rows.Close()
	for rows.Next() {
		var event domain.LifecycleEvent
		var n int64
		var body []byte
		if err = rows.Scan(&n, &event.OccurredAt, &event.Type, &body); err != nil {
			return page, unavailable("read lifecycle event")
		}
		if err = json.Unmarshal(body, &event); err != nil {
			return page, unavailable("decode lifecycle event")
		}
		event.ID = strconv.FormatInt(n, 10)
		page.Items = append(page.Items, event)
	}
	if rows.Err() != nil {
		return page, unavailable("list lifecycle events")
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = page.Items[limit-1].ID
	}
	return page, nil
}

package postgres

import (
	"context"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) ActiveDomain(ctx context.Context, project, baseline string) ([]domain.Composition, error) {
	rows, err := s.pool.Query(ctx, "SELECT body,runtime,deletion_requested FROM compositions WHERE project=$1 AND baseline=$2 AND phase <> 'destroyed' ORDER BY id", project, baseline)
	if err != nil {
		return nil, unavailable("read routing domain")
	}
	defer rows.Close()
	var out []domain.Composition
	for rows.Next() {
		var body, runtime []byte
		var deletion bool
		if err = rows.Scan(&body, &runtime, &deletion); err != nil {
			return nil, unavailable("read composition")
		}

		c, e := decodeComposition(body, runtime, deletion)
		if e != nil {
			return nil, e
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// ListenChanges uses a separate connection so waiting cannot exhaust the query
// pool. Notifications are hints: reconnect and missed messages are repaired by
// the durable recovery sweep.
func (s *Store) ListenChanges(ctx context.Context, notify func(string)) {
	for ctx.Err() == nil {
		conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
		if err == nil {
			_, err = conn.Exec(ctx, "LISTEN envy_desired_changed")
			if err == nil {
				notify("")
			}

			for err == nil {
				var n *pgconn.Notification
				n, err = conn.WaitForNotification(ctx)
				if err == nil {
					notify(n.Payload)
				}
			}

			closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			_ = conn.Close(closeCtx)
			cancel()
		}

		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

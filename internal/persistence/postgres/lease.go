package postgres

import (
	"context"
	"sync"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotLeader = domain.ErrNotLeader

const leaseID int64 = 818818

// Lease holds a dedicated PostgreSQL session; all access to that connection is
// serialized because pgx connections must not be used concurrently.
type Lease struct {
	mu      sync.Mutex
	conn    *pgxpool.Conn
	queries *sqlc.Queries
	closed  bool
}

func (s *Store) AcquireLease(ctx context.Context) (*Lease, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, unavailable("acquire reconciler connection")
	}

	queries := sqlc.New(conn)
	acquired, err := queries.TryAdvisoryLock(ctx, leaseID)
	if err != nil {
		conn.Release()
		return nil, unavailable("acquire reconciler lease")
	}

	if !acquired {
		conn.Release()
		return nil, ErrNotLeader
	}

	return &Lease{conn: conn, queries: queries}, nil
}

func (l *Lease) Check(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrNotLeader
	}

	owned, err := l.queries.CheckAdvisoryLock(ctx, leaseID)
	if err != nil {
		return unavailable("check reconciler lease")
	}

	if !owned {
		return ErrNotLeader
	}

	return nil
}

func (l *Lease) Close(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}

	l.closed = true
	// Use a bounded fresh context if shutdown already cancelled the run context.
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
	}

	unlocked, err := l.queries.AdvisoryUnlock(ctx, leaseID)
	if err != nil || !unlocked {
		// A session that might still own the advisory lock must never re-enter
		// the pool, where an unrelated caller could accidentally retain it.
		_ = l.conn.Conn().Close(context.Background())
	}

	l.conn.Release()
	if err != nil {
		return unavailable("release reconciler lease")
	}

	return nil
}

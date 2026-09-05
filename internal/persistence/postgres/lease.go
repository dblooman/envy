package postgres

import (
	"context"
	"sync"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotLeader = domain.ErrNotLeader

const leaseID int64 = 818818

// Lease holds a dedicated PostgreSQL session; all access to that connection is
// serialized because pgx connections must not be used concurrently.
type Lease struct {
	mu     sync.Mutex
	conn   *pgxpool.Conn
	closed bool
}

func (s *Store) AcquireLease(ctx context.Context) (*Lease, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, unavailable("acquire reconciler connection")
	}
	var acquired bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", leaseID).Scan(&acquired)
	if err != nil {
		conn.Release()
		return nil, unavailable("acquire reconciler lease")
	}
	if !acquired {
		conn.Release()
		return nil, ErrNotLeader
	}
	return &Lease{conn: conn}, nil
}
func (l *Lease) Check(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrNotLeader
	}
	var owned bool
	err := l.conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid() AND classid=0 AND objid=$1::oid AND objsubid=1 AND granted)", leaseID).Scan(&owned)
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
	var unlocked bool
	err := l.conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", leaseID).Scan(&unlocked)
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

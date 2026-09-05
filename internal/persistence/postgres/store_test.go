package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := os.Getenv("ENVY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ENVY_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	bootstrap, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("invalid integration database configuration")
	}
	schema := fmt.Sprintf("envy_test_%d", time.Now().UnixNano())
	if _, err = bootstrap.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		bootstrap.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		_, _ = bootstrap.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		bootstrap.Close()
	})
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err = s.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
func request(name string) domain.CreateRequest {
	return domain.CreateRequest{Project: "demo", Baseline: "staging", Name: name, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "envy/service-b:v2"}}}
}
func checkCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *domain.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error=%v want code=%s", err, code)
	}
}

func TestPersistenceLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("first"), "repeatable")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := app.Create(ctx, request("first"), "repeatable")
	if err != nil || replayed.ID != c.ID {
		t.Fatalf("idempotency replay: %+v %v", replayed, err)
	}
	_, err = app.Create(ctx, request("different"), "repeatable")
	checkCode(t, err, "conflict")
	loaded, err := s.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Runtime.OwnershipToken != c.Runtime.OwnershipToken || loaded.Runtime.OwnershipToken == "" {
		t.Fatal("ownership token did not survive persistence")
	}
	c.Runtime.RoutingActive = true
	c.Runtime.Workload = domain.WorkloadRef{Namespace: "envy-" + c.ID, NamespaceUID: "ns-uid"}
	c.Phase = domain.PhaseReady
	c.ObservedGeneration = 1
	c.LatestOperation.Status = "succeeded"
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}
	d, err := app.Destroy(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Generation != 2 || !d.DeletionRequested || d.Phase != domain.PhaseDestroying || !d.Runtime.RoutingActive || d.Runtime.Workload.NamespaceUID != "ns-uid" {
		t.Fatalf("bad deletion transition: %+v", d)
	}
	if err = s.SaveObservation(ctx, c); !errors.Is(err, domain.ErrStaleObservation) {
		t.Fatalf("stale create clobbered deletion: %v", err)
	}
	d2, err := app.Destroy(ctx, c.ID)
	if err != nil || d2.Generation != d.Generation || d2.LatestOperation.ID != d.LatestOperation.ID {
		t.Fatalf("destroy not repeatable: %+v %v", d2, err)
	}
	d.Phase = domain.PhaseDestroyed
	d.ObservedGeneration = d.Generation
	d.LatestOperation.Status = "succeeded"
	if err = s.SaveObservation(ctx, d); err != nil {
		t.Fatal(err)
	}
	active, err := s.Active(ctx)
	if err != nil || len(active) != 0 {
		t.Fatalf("destroyed composition still active: %+v %v", active, err)
	}
	replayed, err = app.Create(ctx, request("first"), "repeatable")
	if err != nil || replayed.ID != c.ID || replayed.Phase != domain.PhaseDestroyed {
		t.Fatalf("tombstone idempotency: %+v %v", replayed, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM operations WHERE composition_id=$1", c.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("durable operation history count=%d err=%v", count, err)
	}
}

func TestConcurrentCapacityAndIdempotency(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{MaxCompositions: 2})
	var wg sync.WaitGroup
	ids := make(chan string, 10)
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := app.Create(ctx, request("concurrent"), "same-key")
			if err != nil {
				errs <- err
				return
			}
			ids <- c.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent replay error: %v", err)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("replay created duplicate %s %s", first, id)
		}
	}
	_, err := app.Create(ctx, request("second"), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Create(ctx, request("third"), "")
	checkCode(t, err, "capacity_exceeded")
	if _, err = app.Destroy(ctx, first); err != nil {
		t.Fatal(err)
	}
	_, err = app.Create(ctx, request("third"), "")
	checkCode(t, err, "capacity_exceeded")
	page, next, err := app.List(ctx, "demo", "", 1)
	if err != nil || len(page) != 1 || next == "" {
		t.Fatalf("first page: %+v %s %v", page, next, err)
	}
	page2, next2, err := app.List(ctx, "demo", next, 1)
	if err != nil || len(page2) != 1 || next2 != "" || page2[0].ID == page[0].ID {
		t.Fatalf("second page: %+v %s %v", page2, next2, err)
	}
}

func TestExpiryAndCatalog(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	if _, err := s.Component(ctx, "unknown", "service-b"); err == nil {
		t.Fatal("cross-project component lookup succeeded")
	}
	b, err := s.Baseline(ctx, "demo", "staging")
	if err != nil {
		t.Fatal(err)
	}
	b.Revision = "registered-later"
	body, _ := json.Marshal(b)
	if _, err = s.pool.Exec(ctx, "UPDATE baselines SET body=$1 WHERE project='demo' AND id='staging'", body); err != nil {
		t.Fatal(err)
	}
	if err = s.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	b, err = s.Baseline(ctx, "demo", "staging")
	if err != nil || b.Revision != "registered-later" {
		t.Fatalf("seed overwrote registration: %+v %v", b, err)
	}
	r := request("expires")
	r.TTL = "1s"
	c, err := app.Create(ctx, r, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Expire(ctx, c.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	expired, err := s.Get(ctx, c.ID)
	if err != nil || !expired.DeletionRequested || expired.Generation != 2 {
		t.Fatalf("expiry failed: %+v %v", expired, err)
	}
	if err = s.Expire(ctx, c.ExpiresAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	latest, err := s.Get(ctx, c.ID)
	if err != nil || latest.LatestOperation.ID != expired.LatestOperation.ID {
		t.Fatalf("expiry changed durable operation: %+v %v", latest, err)
	}
}

func TestLeaseOwnershipAndRecovery(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	l, err := s.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcquireLease(ctx); !errors.Is(err, domain.ErrNotLeader) {
		t.Fatalf("second leader acquired lease: %v", err)
	}
	if err = l.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err = l.Check(ctx); !errors.Is(err, domain.ErrNotLeader) {
		t.Fatalf("closed lease still usable: %v", err)
	}
	l, err = s.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close(ctx)
	var pid int
	if err = l.conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatal(err)
	}
	if err = l.Check(ctx); err == nil {
		t.Fatal("lost database session retained leadership")
	}
	_ = l.Close(ctx)
	recovered, err := s.AcquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close(ctx)
	if err = recovered.Check(ctx); err != nil {
		t.Fatal(err)
	}
}

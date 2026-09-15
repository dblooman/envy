package postgres

import (
	"context"
	"encoding/json"
	"github.com/dblooman/envy/internal/domain"
	"os"
	"sync"
	"testing"
)

func onboardingManifest(t *testing.T) domain.CatalogManifest {
	t.Helper()
	data, err := os.ReadFile("../../../examples/shop/application.json")
	if err != nil {
		t.Fatal(err)
	}

	var m domain.CatalogManifest
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	return m
}
func TestCatalogBundleIsReadOnlyUntilAtomicRepeatableApply(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	m := onboardingManifest(t)
	if err := s.CheckCatalog(ctx, m); err != nil {
		t.Fatal(err)
	}

	if err := s.ensureProject(ctx, "shop"); err == nil {
		t.Fatal("validation registered project")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Go(func() { errs <- s.ApplyCatalog(ctx, m) })
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent identical apply: %v", err)
		}
	}

	if _, err := s.Baseline(ctx, "shop", "staging"); err != nil {
		t.Fatal(err)
	}

	m.Components[0].Env["DOWNSTREAM_URL"] = "http://changed"
	if err := s.ApplyCatalog(ctx, m); err == nil {
		t.Fatal("changed immutable component accepted")
	} else {
		checkCode(t, err, "conflict")
	}

	got, err := s.Component(ctx, "shop", "storefront")
	if err != nil || got.Env["DOWNSTREAM_URL"] == "http://changed" {
		t.Fatal("conflicting write changed stored profile")
	}
}
func TestCatalogBundleRollsBackEveryEntryOnHostConflict(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	m := onboardingManifest(t)
	m.Baseline.Components["pricing"] = domain.BaselineBinding{ServiceHost: "service-b.envy-baseline.svc.cluster.local", Port: 8080, Image: "v1"}
	if err := s.ApplyCatalog(ctx, m); err == nil {
		t.Fatal("claimed host stolen")
	} else {
		checkCode(t, err, "conflict")
	}

	if err := s.ensureProject(ctx, "shop"); err == nil {
		t.Fatal("failed atomic apply leaked a project")
	}

	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM baseline_host_claims WHERE project='shop'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed apply leaked host claims")
	}
}

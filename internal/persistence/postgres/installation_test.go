package postgres

import (
	"context"
	"testing"
)

func TestInstallationBinding(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.BindInstallation(ctx, "legacy", "cilium"); e == nil {
		t.Fatal("legacy catalog accepted new mesh")
	}
	if e := s.BindInstallation(ctx, "legacy", "istio"); e != nil {
		t.Fatal(e)
	}
	if e := s.BindInstallation(ctx, "legacy", "istio"); e != nil {
		t.Fatal(e)
	}
	if e := s.BindInstallation(ctx, "legacy", "linkerd"); e == nil {
		t.Fatal("provider switch accepted")
	}
	if e := s.BindInstallation(ctx, "different", "istio"); e == nil {
		t.Fatal("installation switch accepted")
	}
}

func TestFreshInstallationProfiles(t *testing.T) {
	for _, profile := range []string{"istio", "cilium", "linkerd"} {
		t.Run(profile, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			if _, err := s.pool.Exec(ctx, "TRUNCATE projects CASCADE"); err != nil {
				t.Fatal(err)
			}
			if err := s.BindInstallation(ctx, "fresh", profile); err != nil {
				t.Fatal(err)
			}
			if err := s.BindInstallation(ctx, "fresh", profile); err != nil {
				t.Fatal(err)
			}
		})
	}
}

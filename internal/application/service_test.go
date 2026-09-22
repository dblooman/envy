package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func validRequest() domain.CreateRequest {
	return domain.CreateRequest{Project: "demo", Baseline: "staging", Name: "change", Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "envy/service-b:v2"}}}
}

func TestCreateGuardsSavedBaselineRevision(t *testing.T) {
	r := &createRepository{}
	s := New(r, Config{PreviewBaseURL: "http://envy.localhost:18080"})
	req := validRequest()
	req.ExpectedBaselineRevision = "old-revision"
	_, err := s.Create(context.Background(), req, "recipe-retry")
	var public *domain.Error
	if !errors.As(err, &public) || public.Code != "conflict" || r.received.ID != "" {
		t.Fatalf("changed baseline accepted: %v", err)
	}

	req.ExpectedBaselineRevision = "revision-42"
	if _, err := s.Create(context.Background(), req, "recipe-retry"); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeCreate(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.CreateRequest)
		want   bool
	}{
		{"default", func(r *domain.CreateRequest) {}, true},
		{"positive short TTL", func(r *domain.CreateRequest) { r.TTL = "1ms" }, true},
		{"maximum TTL", func(r *domain.CreateRequest) { r.TTL = "24h" }, true},
		{"invalid duration", func(r *domain.CreateRequest) { r.TTL = "tomorrow" }, false},
		{"negative duration", func(r *domain.CreateRequest) { r.TTL = "-1h" }, false},
		{"zero duration", func(r *domain.CreateRequest) { r.TTL = "0s" }, false},
		{"long duration", func(r *domain.CreateRequest) { r.TTL = "25h" }, false},
		{"missing override", func(r *domain.CreateRequest) { r.Overrides = nil }, false},
		{"multiple overrides", func(r *domain.CreateRequest) { r.Overrides["gateway"] = domain.ComponentOverride{Image: "x"} }, true},
		{"another component", func(r *domain.CreateRequest) {
			r.Overrides = map[string]domain.ComponentOverride{"service-a": {Image: "x"}}
		}, true},
		{"empty image", func(r *domain.CreateRequest) { r.Overrides["service-b"] = domain.ComponentOverride{} }, false},
		{"whitespace image", func(r *domain.CreateRequest) { r.Overrides["service-b"] = domain.ComponentOverride{Image: "x\ny"} }, false},
		{"clone resource", func(r *domain.CreateRequest) {
			r.Resources = map[string]domain.ResourceOverride{"db": {Strategy: "clone"}}
		}, false},
		{"blank name", func(r *domain.CreateRequest) { r.Name = " " }, false},
		{"blank project", func(r *domain.CreateRequest) { r.Project = "" }, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			r := validRequest()
			test.change(&r)
			_, _, err := NormalizeCreate(r, Config{})
			if (err == nil) != test.want {
				t.Fatalf("error=%v want success=%v", err, test.want)
			}

			if err != nil {
				var public *domain.Error
				if !errors.As(err, &public) || public.Code != "validation_error" {
					t.Fatalf("wrong validation error: %v", err)
				}
			}
		})
	}
}

type createRepository struct {
	Repository
	received domain.Composition
	hash     string
	key      string
	cap      int
}

func (r *createRepository) Baseline(context.Context, string, string) (domain.Baseline, error) {
	return domain.Baseline{ID: "staging", Project: "demo", Revision: "revision-42", Verification: domain.VerificationContract{Kind: "http"}, Components: map[string]domain.BaselineBinding{"gateway": {Image: "envy/gateway:v1"}, "service-a": {Image: "envy/service-a:v1"}, "service-b": {Image: "envy/service-b:v1"}}}, nil
}

func (r *createRepository) Component(context.Context, string, string) (domain.Component, error) {
	return domain.Component{ID: "service-b", Overridable: true}, nil
}

func (r *createRepository) Create(_ context.Context, c domain.Composition, key, hash string, cap int) (domain.Composition, error) {
	r.received = c
	r.hash = hash
	r.key = key
	r.cap = cap
	return c, nil
}

func TestCreateAllocatesIntentAndCanonicalHash(t *testing.T) {
	r := &createRepository{}
	s := New(r, Config{PreviewBaseURL: "http://envy.localhost:18080"})
	c, err := s.Create(context.Background(), validRequest(), "retry-1")
	if err != nil {
		t.Fatal(err)
	}

	if c.Phase != domain.PhaseCreated || c.Generation != 1 || c.ObservedGeneration != 0 || c.Endpoints["public"].Ready {
		t.Fatalf("new composition was observed ready: %+v", c)
	}

	if c.Runtime.OwnershipToken == "" || c.LatestOperation.ID == "" || c.BaselineRevision != "revision-42" {
		t.Fatalf("missing durable identity: %+v", c)
	}

	if c.Endpoints["public"].URL != "http://cmp-"+c.ID+".envy.localhost:18080" {
		t.Fatalf("wrong URL: %s", c.Endpoints["public"].URL)
	}

	if c.ExpiresAt.Sub(c.CreatedAt) != 8*time.Hour || r.cap != 20 {
		t.Fatalf("wrong defaults: %+v", c)
	}

	if len(c.Components) != 3 || c.Components["service-b"].Source != "override" || c.Components["gateway"].Source != "baseline" {
		t.Fatalf("wrong component inheritance: %+v", c.Components)
	}

	firstHash := r.hash
	req := validRequest()
	req.TTL = "480m"
	if _, err = s.Create(context.Background(), req, "retry-1"); err != nil {
		t.Fatal(err)
	}

	if r.hash != firstHash {
		t.Fatal("equivalent TTL did not normalize for idempotency")
	}

	req.Name = "different"
	_, _ = s.Create(context.Background(), req, "retry-1")
	if r.hash == firstHash {
		t.Fatal("changed logical request produced same hash")
	}
}

func TestInheritedHTTPCompositionGetsEndpoint(t *testing.T) {
	r := &createRepository{}
	s := New(r, Config{PreviewBaseURL: "http://envy.localhost:18080"})
	req := validRequest()
	req.Overrides = map[string]domain.ComponentOverride{}
	c, err := s.Create(context.Background(), req, "inherited-http")
	if err != nil {
		t.Fatal(err)
	}

	if c.Endpoints["public"].URL != "http://cmp-"+c.ID+".envy.localhost:18080" {
		t.Fatalf("inherited HTTP composition has no endpoint: %+v", c.Endpoints)
	}
}

func TestInvalidIdempotencyNeverTouchesRepository(t *testing.T) {
	s := New(nil, Config{})
	for _, key := range []string{"bad\nkey", "non-ascii-é", string(make([]byte, 129))} {
		if _, err := s.Create(context.Background(), validRequest(), key); err == nil {
			t.Fatalf("accepted invalid key %q", key)
		}
	}
}

func TestInvalidUpdateNeverTouchesRepository(t *testing.T) {
	s := New(nil, Config{})
	for _, req := range []domain.UpdateRequest{
		{ExpectedGeneration: 0, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "v3"}}},
		{ExpectedGeneration: 1},
		{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"bad/key": {Image: "v3"}}},
		{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "bad image"}}},
	} {
		if _, err := s.Update(context.Background(), "abc", req); err == nil {
			t.Fatalf("invalid update accepted: %+v", req)
		}
	}
}

func TestOverrideBoundsRejectInvalidMembers(t *testing.T) {
	for _, overrides := range []map[string]domain.ComponentOverride{
		{"a": {Image: "a"}, "b": {Image: "b"}, "c": {Image: "c"}, "d": {Image: "d"}},
		{"a": {Image: "valid"}, "b": {Image: "bad image"}},
		{"a": {Image: "valid"}, "bad/id": {Image: "valid"}},
	} {
		if err := ValidateOverrides(overrides); err == nil {
			t.Fatal("invalid override map accepted")
		}
	}

	if err := ValidateOverrides(map[string]domain.ComponentOverride{"a": {Image: "a"}, "b": {Image: "b"}, "c": {Image: "c"}}); err != nil {
		t.Fatal(err)
	}
}

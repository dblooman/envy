package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestRequestErrorsAndAuth(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing auth")
		}

		if r.Method == "POST" && r.Header.Get("Idempotency-Key") != "retry" {
			t.Error("missing retry key")
		}

		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"error": &domain.Error{Code: "conflict", Message: "key reused"}})
	}))
	defer s.Close()
	c, err := New(s.URL, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Create(context.Background(), domain.CreateRequest{}, "retry")
	var apiErr *domain.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "conflict" {
		t.Fatalf("lost structured error: %v", err)
	}

	for _, id := range []string{"", "../secret", "a/b", "a?x=1"} {
		if _, err := c.Get(context.Background(), id); err == nil {
			t.Fatalf("accepted ID %q", id)
		}
	}
}

func TestWaitTimeoutAndCancellationNeverDelete(t *testing.T) {
	var deletes atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes.Add(1)
		}

		json.NewEncoder(w).Encode(domain.Composition{ID: "abc", Phase: domain.PhaseProvisioning})
	}))
	defer s.Close()
	c, _ := New(s.URL, "secret", nil)
	start := time.Now()
	got, err := c.Wait(context.Background(), "abc", 35*time.Millisecond)
	if err != nil || got.ID != "abc" || got.Phase != domain.PhaseProvisioning || time.Since(start) > time.Second {
		t.Fatalf("timeout should return latest: %+v %v", got, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Wait(ctx, "abc", time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}

	if deletes.Load() != 0 {
		t.Fatal("waiting must not delete")
	}

	if _, err = c.Wait(context.Background(), "abc", 61*time.Second); err == nil {
		t.Fatal("unbounded timeout accepted")
	}
}

func TestRedirectDoesNotForwardToken(t *testing.T) {
	var forwarded atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Store(true) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, _ := New(source.URL, "secret", nil)
	if _, err := c.Get(context.Background(), "abc"); err == nil {
		t.Fatal("redirect unexpectedly succeeded")
	}

	if forwarded.Load() {
		t.Fatal("redirect followed")
	}
}

func TestClientConfiguration(t *testing.T) {
	for _, base := range []string{"", "file:///tmp/api", "http://user:pass@localhost", "http://localhost?token=foo"} {
		if _, err := New(base, "secret", nil); err == nil {
			t.Fatalf("accepted URL %q", base)
		}
	}

	if _, err := New("http://localhost", "", nil); err != nil {
		t.Fatal("empty token must be allowed for dev mode")
	}
}

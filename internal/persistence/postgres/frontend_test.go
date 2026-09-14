package postgres

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/api"
	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/domain"
)

// Exercises real migrations, transactions, application validation and authenticated REST.
func TestFrontendLifecycleThroughREST(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	server := httptest.NewServer(api.NewHandler(app, "test-token", nil))
	defer server.Close()
	path := "/v1/projects/demo/frontend-bindings/web/" + strings.Repeat("a", 40)
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"GET", path, "", "", 401},
		{"PUT", path, `{"composition":"abc","repository":"https://example.com/web","unknown":true}`, "test-token", 400},
		{"POST", path + "/deployment", `{"expected_version":1,"url":"javascript:alert(1)"}`, "test-token", 400},
		{"POST", path + "/check", `{"expected_version":1,"composition_generation":1,"status":"invented","message":"x"}`, "test-token", 400},
	} {
		req, _ := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if tc.token != "" && res.Header.Get("Cache-Control") != "no-store" {
			t.Error("frontend responses must not be cached")
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("contract %d %s", res.StatusCode, body)
		}
	}
	rest, err := client.New(server.URL, "test-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := rest.Create(ctx, request("frontend"), "frontend")
	if err != nil {
		t.Fatal(err)
	}
	// Reconciler fixtures need internal execution state, which REST intentionally omits.
	c, err = s.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	k := domain.FrontendKey{Project: "demo", Frontend: "web", Revision: strings.Repeat("a", 40)}
	req := domain.BindFrontendRequest{Composition: c.ID, Repository: "https://example.com/org/web"}
	b, err := rest.BindFrontend(ctx, k, req)
	if err != nil || b.Binding.Version != 1 || b.Ready {
		t.Fatalf("bind: %+v %v", b, err)
	}
	replay, err := rest.BindFrontend(ctx, k, req)
	if err != nil || replay.Binding.CreatedAt != b.Binding.CreatedAt {
		t.Fatalf("retry: %+v %v", replay, err)
	}
	wrong := k
	wrong.Project = "another"
	_, err = rest.BindFrontend(ctx, wrong, req)
	checkCode(t, err, "not_found")
	changed := req
	changed.Repository = "https://example.com/other"
	_, err = rest.BindFrontend(ctx, k, changed)
	checkCode(t, err, "conflict")
	_, err = rest.ResolveFrontend(ctx, k, 0)
	checkCode(t, err, "conflict")
	c.Phase = domain.PhaseReady
	c.ObservedGeneration = c.Generation
	c.Endpoints = map[string]domain.Endpoint{"public": {URL: "https://preview.example", Ready: true}}
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}
	receipt, err := rest.ResolveFrontend(ctx, k, 0)
	if err != nil || receipt.APIURL != "https://preview.example" || receipt.Composition != c.ID {
		t.Fatalf("resolve: %+v %v", receipt, err)
	}
	b, err = rest.PublishFrontend(ctx, k, domain.PublishFrontendRequest{ExpectedVersion: 1, URL: "https://web.pages.dev"})
	if err != nil || b.Binding.Version != 2 {
		t.Fatalf("publish: %+v %v", b, err)
	}
	_, err = rest.PublishFrontend(ctx, k, domain.PublishFrontendRequest{ExpectedVersion: 1, URL: "https://other.pages.dev"})
	checkCode(t, err, "conflict")
	b, err = rest.CheckFrontend(ctx, k, domain.FrontendCheckRequest{ExpectedVersion: 2, CompositionGeneration: 1, Status: "passed", Message: "Browser rendered preview price"})
	if err != nil || b.CheckState != "current" || b.Binding.Version != 3 {
		t.Fatalf("check: %+v %v", b, err)
	}
	// Reconciler observations cannot erase separate metadata.
	if err = s.SaveObservation(ctx, c); err != nil {
		t.Fatal(err)
	}
	b, err = rest.FrontendBinding(ctx, k)
	if err != nil || b.Binding.Check == nil {
		t.Fatalf("lost check: %+v %v", b, err)
	}
	updated, err := app.Update(ctx, c.ID, domain.UpdateRequest{ExpectedGeneration: 1, Overrides: map[string]domain.ComponentOverride{"service-b": {Image: "envy/service-b:v3"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err = rest.FrontendBinding(ctx, k)
	if err != nil || b.Ready || b.CheckState != "stale" {
		t.Fatalf("stale: %+v %v", b, err)
	}
	_, err = rest.ResolveFrontend(ctx, k, 0)
	checkCode(t, err, "conflict")
	updated.Phase = domain.PhaseReady
	updated.ObservedGeneration = updated.Generation
	updated.Endpoints = c.Endpoints
	if err = s.SaveObservation(ctx, updated); err != nil {
		t.Fatal(err)
	}
	_, err = rest.CheckFrontend(ctx, k, domain.FrontendCheckRequest{ExpectedVersion: 3, CompositionGeneration: 1, Status: "passed", Message: "old"})
	checkCode(t, err, "conflict")
	b, err = rest.PublishFrontend(ctx, k, domain.PublishFrontendRequest{ExpectedVersion: 3, URL: "https://new.pages.dev"})
	if err != nil || b.Binding.Check != nil {
		t.Fatalf("new URL retained check: %+v %v", b, err)
	}
	page, err := rest.FrontendBindings(ctx, c.ID, "", 1)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	// Expire desired state without running a controller or expiry scan.
	_, err = s.pool.Exec(ctx, `UPDATE compositions SET body=jsonb_set(body,'{expires_at}',to_jsonb($2::text)) WHERE id=$1`, c.ID, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	_, err = rest.ResolveFrontend(ctx, k, time.Second)
	checkCode(t, err, "gone")
	_, err = rest.PublishFrontend(ctx, k, domain.PublishFrontendRequest{ExpectedVersion: 4, URL: "https://expired.pages.dev"})
	checkCode(t, err, "gone")
	_, err = rest.CheckFrontend(ctx, k, domain.FrontendCheckRequest{ExpectedVersion: 4, CompositionGeneration: 2, Status: "passed", Message: "expired"})
	checkCode(t, err, "gone")
	expiredKey := k
	expiredKey.Revision = strings.Repeat("d", 40)
	_, err = rest.BindFrontend(ctx, expiredKey, req)
	checkCode(t, err, "gone")
	b, err = rest.FrontendBinding(ctx, k)
	if err != nil || b.Ready {
		t.Fatalf("tombstone: %+v %v", b, err)
	}
}

func TestFrontendConcurrentPublicationAndPagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	app := application.New(s, application.Config{})
	c, err := app.Create(ctx, request("bindings"), "")
	if err != nil {
		t.Fatal(err)
	}
	k := domain.FrontendKey{Project: "demo", Frontend: "web", Revision: strings.Repeat("b", 40)}
	req := domain.BindFrontendRequest{Composition: c.ID, Repository: "https://example.com/web"}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := app.BindFrontend(ctx, k, req); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	results := make(chan error, 2)
	for i := range 2 {
		wg.Go(func() {
			_, err := app.PublishFrontend(ctx, k, domain.PublishFrontendRequest{ExpectedVersion: 1, URL: fmt.Sprintf("https://web%d.pages.dev", i)})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			checkCode(t, err, "conflict")
		}
	}
	if successes != 1 {
		t.Fatalf("%d publications won", successes)
	}
	k.Revision = strings.Repeat("c", 40)
	if _, err = app.BindFrontend(ctx, k, req); err != nil {
		t.Fatal(err)
	}
	first, next, err := app.FrontendBindings(ctx, c.ID, "", 1)
	if err != nil || len(first) != 1 || next == "" {
		t.Fatalf("first page %v %s %v", first, next, err)
	}
	second, next, err := app.FrontendBindings(ctx, c.ID, next, 1)
	if err != nil || len(second) != 1 || next != "" || first[0].Binding.Revision == second[0].Binding.Revision {
		t.Fatalf("second page %v %s %v", second, next, err)
	}
	for i := range 98 {
		k.Revision = fmt.Sprintf("%040x", i)
		if _, err = app.BindFrontend(ctx, k, req); err != nil {
			t.Fatal(err)
		}
	}
	k.Revision = strings.Repeat("d", 40)
	_, err = app.BindFrontend(ctx, k, req)
	checkCode(t, err, "capacity_exceeded")

}

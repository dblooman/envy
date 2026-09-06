package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type catalogServiceFake struct {
	*fakeService
	writes  int
	project string
}

func (s *catalogServiceFake) RegisterProject(_ context.Context, p domain.Project) (domain.Project, error) {
	s.writes++
	return p, nil
}
func (s *catalogServiceFake) RegisterComponent(_ context.Context, c domain.Component) (domain.Component, error) {
	s.writes++
	s.project = c.Project
	return c, nil
}
func (s *catalogServiceFake) RegisterBaseline(_ context.Context, b domain.Baseline) (domain.Baseline, error) {
	s.writes++
	s.project = b.Project
	return b, nil
}
func TestCatalogHTTPBoundaries(t *testing.T) {
	s := &catalogServiceFake{fakeService: &fakeService{}}
	h := NewHandler(s, "secret", nil)
	for _, tc := range []struct {
		path, body, token string
		code              int
	}{
		{"/v1/projects", `{"id":"orders","name":"Orders"}`, "", 401},
		{"/v1/projects", `{"id":"orders","name":"Orders","unknown":true}`, "secret", 400},
		{"/v1/projects", `{"id":"orders"} {}`, "secret", 400},
		{"/v1/projects/orders/components", `{"id":"worker","project":"other"}`, "secret", 400},
		{"/v1/projects/orders/baselines", `{"id":"staging","project":"other"}`, "secret", 400},
		{"/v1/projects", `{"id":"orders","name":"Orders"}`, "secret", 201},
		{"/v1/projects/orders/components", `{"id":"worker"}`, "secret", 201},
		{"/v1/projects/orders/baselines", `{"id":"staging"}`, "secret", 201},
	} {
		w := request(h, "POST", tc.path, tc.body, tc.token)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		var body any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	if s.writes != 3 || s.project != "orders" {
		t.Fatal("invalid registration reached application, or project scope was lost")
	}
}

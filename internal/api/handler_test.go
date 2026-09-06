package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type fakeService struct {
	Service
	composition    domain.Composition
	createCalls    int
	updateCalls    int
	updateRequest  domain.UpdateRequest
	key            string
	request        domain.CreateRequest
	err            error
	project, after string
	limit          int
}

func (s *fakeService) Create(_ context.Context, req domain.CreateRequest, key string) (domain.Composition, error) {
	s.createCalls++
	s.request = req
	s.key = key
	return s.composition, s.err
}
func (s *fakeService) Get(context.Context, string) (domain.Composition, error) {
	return s.composition, s.err
}
func (s *fakeService) Destroy(context.Context, string) (domain.Composition, error) {
	return s.composition, s.err
}
func (s *fakeService) List(_ context.Context, project, after string, limit int) ([]domain.Composition, string, error) {
	s.project, s.after, s.limit = project, after, limit
	return nil, "", s.err
}
func (s *fakeService) Components(_ context.Context, project, after string, limit int) ([]domain.Component, string, error) {
	s.project, s.after, s.limit = project, after, limit
	return []domain.Component{{ID: "service-b", Project: project}}, "service-b", s.err
}
func (s *fakeService) Component(_ context.Context, project, id string) (domain.Component, error) {
	s.project = project
	return domain.Component{ID: id, Project: project}, s.err
}

const validCreate = `{"project":"demo","baseline":"staging","name":"test","overrides":{"service-b":{"image":"envy/service-b:v2"}}}`

func request(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthenticationAndHealth(t *testing.T) {
	s := &fakeService{composition: domain.Composition{ID: "abc"}}
	h := NewHandler(s, "secret", func(context.Context) error { return errors.New("postgres://password@host") })
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"absent", "", 401}, {"wrong", "wrong", 401}, {"valid", "secret", 200}} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(h, "GET", "/v1/compositions/abc", "", tc.token)
			if w.Code != tc.want {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("responses must not be cached")
			}
		})
	}
	if w := request(h, "GET", "/healthz", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := request(h, "GET", "/readyz", "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "password") {
		t.Fatalf("unsafe readiness response %d %s", w.Code, w.Body.String())
	}
	if w := request(NewHandler(s, "", nil), "GET", "/v1/compositions/abc", "", ""); w.Code != 401 {
		t.Fatal("empty configured token must fail closed")
	}
	r := httptest.NewRequest("GET", "/v1/compositions/abc", nil)
	r.Header.Add("Authorization", "Bearer secret")
	r.Header.Add("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("duplicate authorization accepted")
	}
}

func TestCreateStrictBodyAndAcceptedContract(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"unknown", strings.TrimSuffix(validCreate, "}") + `,"surprise":true}`},
		{"unknown nested", strings.Replace(validCreate, `"image":"envy/service-b:v2"`, `"image":"envy/service-b:v2","privileged":true`, 1)},
		{"trailing", validCreate + ` {}`}, {"malformed", `{`},
		{"oversized", strings.Replace(validCreate, `"test"`, `"`+strings.Repeat("a", 65<<10)+`"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeService{}
			w := request(NewHandler(s, "secret", nil), "POST", "/v1/compositions", tc.body, "secret")
			if w.Code != 400 || s.createCalls != 0 {
				t.Fatalf("status %d calls %d", w.Code, s.createCalls)
			}
		})
	}
	s := &fakeService{composition: domain.Composition{ID: "abc", Phase: domain.PhaseCreated, Generation: 1, LatestOperation: domain.Operation{ID: "op", Kind: "create", Status: "pending"}, Endpoints: map[string]domain.Endpoint{"public": {URL: "http://cmp-abc.envy.localhost:8080"}}}}
	h := NewHandler(s, "secret", nil)
	r := httptest.NewRequest("POST", "/v1/compositions", strings.NewReader(validCreate))
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Idempotency-Key", "retry-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 || w.Header().Get("Location") != "/v1/compositions/abc" || s.key != "retry-1" {
		t.Fatalf("invalid create response %d %s", w.Code, w.Body.String())
	}
	var got domain.Composition
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.LatestOperation.ID != "op" || got.Endpoints["public"].Ready {
		t.Fatal("accepted response must include operation and unready endpoint")
	}
}

func TestPaginationAndProjectScope(t *testing.T) {
	s := &fakeService{}
	h := NewHandler(s, "secret", nil)
	for _, path := range []string{"/v1/compositions?limit=0", "/v1/compositions?limit=101", "/v1/compositions?limit=nope", "/v1/compositions?limit=1&limit=2"} {
		if w := request(h, "GET", path, "", "secret"); w.Code != 400 {
			t.Fatalf("%s status %d", path, w.Code)
		}
	}
	w := request(h, "GET", "/v1/compositions?project=demo&after=abc&limit=7", "", "secret")
	if w.Code != 200 || s.project != "demo" || s.after != "abc" || s.limit != 7 {
		t.Fatal("pagination was not passed through")
	}
	if w.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("empty list should be []: %s", w.Body.String())
	}
	w = request(h, "GET", "/v1/projects/other/components?after=a", "", "secret")
	if w.Code != 200 || s.project != "other" || s.limit != 20 || !strings.Contains(w.Body.String(), `"next_cursor":"service-b"`) {
		t.Fatalf("bad scoped catalog %s", w.Body.String())
	}
	w = request(h, "GET", "/v1/projects/demo/components/service-b", "", "secret")
	if w.Code != 200 || s.project != "demo" {
		t.Fatal("component lookup lost project scope")
	}
}

func TestErrorsAndNarrowViews(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{{"validation_error", 400}, {"not_found", 404}, {"conflict", 409}, {"capacity_exceeded", 429}, {"unavailable", 503}} {
		s := &fakeService{err: &domain.Error{Code: tc.code, Message: "safe diagnostic"}}
		w := request(NewHandler(s, "secret", nil), "GET", "/v1/compositions/abc", "", "secret")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("%s: %d %s", tc.code, w.Code, w.Body.String())
		}
	}
	s := &fakeService{err: errors.New("database password=must-not-leak")}
	w := request(NewHandler(s, "secret", nil), "GET", "/v1/compositions/abc", "", "secret")
	if w.Code != 503 || strings.Contains(w.Body.String(), "must-not-leak") {
		t.Fatal("internal diagnostic leaked")
	}
	s.err = nil
	s.composition = domain.Composition{ID: "abc", Name: "not-in-status", Phase: domain.PhaseReady, Endpoints: map[string]domain.Endpoint{"public": {URL: "http://example", Ready: true}}, LastError: &domain.Error{Code: "historical", Message: "message"}}
	h := NewHandler(s, "secret", nil)
	w = request(h, "GET", "/v1/compositions/abc/status", "", "secret")
	if w.Code != 200 || strings.Contains(w.Body.String(), "not-in-status") || !strings.Contains(w.Body.String(), "last_error") {
		t.Fatal(w.Body.String())
	}
	w = request(h, "GET", "/v1/compositions/abc/endpoints", "", "secret")
	if w.Code != 200 || strings.Contains(w.Body.String(), "phase") || !strings.Contains(w.Body.String(), `"ready":true`) {
		t.Fatal(w.Body.String())
	}
	w = request(h, "DELETE", "/v1/compositions/abc", "", "secret")
	if w.Code != 202 || !strings.Contains(w.Body.String(), "not-in-status") {
		t.Fatal(w.Body.String())
	}
}

func (s *fakeService) Update(_ context.Context, id string, req domain.UpdateRequest) (domain.Composition, error) {
	s.updateCalls++
	s.updateRequest = req
	return s.composition, s.err
}
func TestUpdateHTTPContract(t *testing.T) {
	s := &fakeService{composition: domain.Composition{ID: "abc", Generation: 2, Phase: domain.PhaseUpdating}}
	h := NewHandler(s, "secret", nil)
	body := `{"expected_generation":1,"overrides":{"service-b":{"image":"envy/service-b:v3"}}}`
	for _, bad := range []string{body + ` {}`, `{"image":"unexpected"}`, `{"expected_generation":"1"}`} {
		if w := request(h, "PATCH", "/v1/compositions/abc", bad, "secret"); w.Code != 400 {
			t.Fatalf("accepted %s: %d", bad, w.Code)
		}
	}
	if s.updateCalls != 0 {
		t.Fatal("malformed updates reached application")
	}
	w := request(h, "PATCH", "/v1/compositions/abc", body, "secret")
	if w.Code != 202 || w.Header().Get("Location") != "/v1/compositions/abc" || s.updateRequest.ExpectedGeneration != 1 || s.updateRequest.Overrides["service-b"].Image != "envy/service-b:v3" {
		t.Fatalf("wrong update contract: %d %s", w.Code, w.Body)
	}
	s.err = &domain.Error{Code: "conflict", Message: "stale generation"}
	if w = request(h, "PATCH", "/v1/compositions/abc", body, "secret"); w.Code != 409 {
		t.Fatalf("conflict status=%d", w.Code)
	}
}

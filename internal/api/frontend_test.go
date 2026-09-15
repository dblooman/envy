package api

import (
	"context"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type frontendAPI struct {
	*fakeService
	call          string
	key           domain.FrontendKey
	bind          domain.BindFrontendRequest
	publish       domain.PublishFrontendRequest
	check         domain.FrontendCheckRequest
	composition   string
	after         string
	limit         int
	frontendError error
}

func (s *frontendAPI) view() domain.FrontendBindingView {
	return domain.FrontendBindingView{Binding: domain.FrontendBinding{Project: s.key.Project, Frontend: s.key.Frontend, Revision: s.key.Revision, Composition: "abc"}}
}
func (s *frontendAPI) BindFrontend(_ context.Context, key domain.FrontendKey, request domain.BindFrontendRequest) (domain.FrontendBindingView, error) {
	s.call, s.key, s.bind = "bind", key, request
	return s.view(), s.frontendError
}
func (s *frontendAPI) FrontendBinding(_ context.Context, key domain.FrontendKey) (domain.FrontendBindingView, error) {
	s.call, s.key = "get", key
	return s.view(), s.frontendError
}
func (s *frontendAPI) ResolveFrontend(_ context.Context, key domain.FrontendKey) (domain.FrontendResolution, error) {
	s.call, s.key = "resolve", key
	return domain.FrontendResolution{Project: key.Project, Frontend: key.Frontend, Revision: key.Revision, Composition: "abc"}, s.frontendError
}
func (s *frontendAPI) FrontendBindings(_ context.Context, composition, after string, limit int) ([]domain.FrontendBindingView, string, error) {
	s.call, s.composition, s.after, s.limit = "list", composition, after, limit
	return []domain.FrontendBindingView{}, "next", s.frontendError
}
func (s *frontendAPI) PublishFrontend(_ context.Context, key domain.FrontendKey, request domain.PublishFrontendRequest) (domain.FrontendBindingView, error) {
	s.call, s.key, s.publish = "publish", key, request
	return s.view(), s.frontendError
}
func (s *frontendAPI) CheckFrontend(_ context.Context, key domain.FrontendKey, request domain.FrontendCheckRequest) (domain.FrontendBindingView, error) {
	s.call, s.key, s.check = "check", key, request
	return s.view(), s.frontendError
}

func TestFrontendRoutesUseDedicatedHandlers(t *testing.T) {
	revision := strings.Repeat("a", 40)
	keyPath := "/v1/projects/demo/frontend-bindings/web/" + revision
	for _, test := range []struct {
		name, method, path, body, want string
	}{
		{"bind", "PUT", keyPath, `{"composition":"abc","repository":"https://example.com/web"}`, "bind"},
		{"get", "GET", keyPath, "", "get"},
		{"resolve", "GET", keyPath + "/resolve", "", "resolve"},
		{"publish", "POST", keyPath + "/deployment", `{"expected_version":2,"url":"https://web.example.com"}`, "publish"},
		{"check", "POST", keyPath + "/check", `{"expected_version":2,"composition_generation":3,"status":"passed","message":"ok"}`, "check"},
		{"list", "GET", "/v1/compositions/abc/frontend-bindings?after=cursor&limit=7", "", "list"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &frontendAPI{fakeService: &fakeService{}}
			response := request(NewHandler(service, "secret", nil), test.method, test.path, test.body, "secret")
			if response.Code != 200 || service.call != test.want {
				t.Fatalf("status=%d call=%q body=%s", response.Code, service.call, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("frontend response must not be cached")
			}
			if test.want == "list" {
				if service.composition != "abc" || service.after != "cursor" || service.limit != 7 {
					t.Fatalf("list scope=%q after=%q limit=%d", service.composition, service.after, service.limit)
				}
				return
			}
			if service.key != (domain.FrontendKey{Project: "demo", Frontend: "web", Revision: revision}) {
				t.Fatalf("key=%+v", service.key)
			}
		})
	}
}

func TestFrontendRoutesRejectInvalidInputAndMapServiceErrors(t *testing.T) {
	revision := strings.Repeat("a", 40)
	path := "/v1/projects/demo/frontend-bindings/web/" + revision
	service := &frontendAPI{fakeService: &fakeService{}}
	h := NewHandler(service, "secret", nil)
	if response := request(h, "PUT", path, `{"composition":"abc","unexpected":true}`, "secret"); response.Code != 400 || service.call != "" {
		t.Fatalf("invalid bind status=%d call=%q", response.Code, service.call)
	}
	if response := request(h, "GET", "/v1/compositions/abc/frontend-bindings?limit=101", "", "secret"); response.Code != 400 || service.call != "" {
		t.Fatalf("invalid list status=%d call=%q", response.Code, service.call)
	}
	service.frontendError = domain.NotFound("binding missing")
	if response := request(h, "GET", path, "", "secret"); response.Code != 404 {
		t.Fatalf("error status=%d body=%s", response.Code, response.Body.String())
	}
}

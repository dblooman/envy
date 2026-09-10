package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type buildAPI struct {
	Service
	buildService
	calls int
}

func (s *buildAPI) RecordBuild(_ context.Context, project, repo string, in domain.BuildReport) (domain.Build, error) {
	s.calls++
	return domain.Build{Project: project, Repository: repo, BuildReport: in}, nil
}
func TestCICredentialsAreRestrictedToExactReportingScope(t *testing.T) {
	s := &buildAPI{}
	ci := strings.Repeat("a", 32)
	h := NewHandlerWithBuildCredentials(s, "admin", nil, []BuildCredential{{Token: ci, Project: "demo", Repository: "backend", Components: []string{"service-b"}}})
	path := "/v1/projects/demo/repositories/backend/builds"
	for _, test := range []struct {
		method, path, body, token string
		code                      int
	}{
		{"POST", path, `{"component":"service-b"}`, ci, 200},
		{"POST", path, `{"component":"service-a"}`, ci, 401},
		{"POST", path, `{"component":"service-b"}`, "admin", 401},
		{"POST", "/v1/projects/other/repositories/backend/builds", `{"component":"service-b"}`, ci, 401},
		{"POST", "/v1/projects/demo/repositories/other/builds", `{"component":"service-b"}`, ci, 401},
		{"GET", "/v1/projects", "", ci, 401},
		{"PATCH", "/v1/projects/demo/repositories/backend", `{"enabled":true}`, ci, 401},
		{"POST", "/v1/compositions", `{}`, ci, 401},
		{"POST", path, `{"component":"service-b","source":"forged"}`, ci, 400},
	} {
		res := request(h, test.method, test.path, test.body, test.token)
		if res.Code != test.code {
			t.Errorf("%s %s: got %d want %d (%s)", test.method, test.path, res.Code, test.code, res.Body.String())
		}
	}
	if s.calls != 1 {
		t.Fatalf("unauthorized build reports reached service: %d", s.calls)
	}
	// Unknown paths remain unauthorized for CI credentials, including escaped slashes.
	res := request(h, http.MethodPost, "/v1/projects/demo/repositories/backend%2fbuilds", `{}`, ci)
	if res.Code != 401 {
		t.Fatal("encoded path bypass")
	}
}

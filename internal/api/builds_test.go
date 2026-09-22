package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

type buildAPI struct {
	*fakeService
	calls int
	call  string
}

func (s *buildAPI) SourceRepositories(context.Context, string, string, int) ([]domain.SourceRepository, string, error) {
	s.call = "list"
	return []domain.SourceRepository{}, "", nil
}

func (s *buildAPI) RegisterSourceRepository(_ context.Context, repository domain.SourceRepository) (domain.SourceRepository, error) {
	s.call = "register"
	return repository, nil
}

func (s *buildAPI) EnableSourceRepository(_ context.Context, project, repository string, enabled bool) (domain.SourceRepository, error) {
	s.call = "enable"
	return domain.SourceRepository{Project: project, ID: repository, Enabled: enabled}, nil
}

func (s *buildAPI) SourceBranches(context.Context, string, string, int) ([]domain.GitBranch, error) {
	s.call = "branches"
	return []domain.GitBranch{}, nil
}

func (s *buildAPI) SourceCommits(context.Context, string, string, string, int) ([]domain.GitCommit, error) {
	s.call = "commits"
	return []domain.GitCommit{}, nil
}

func (s *buildAPI) ResolveRevision(context.Context, string, string, string, string, string, int) (domain.RevisionResolution, error) {
	s.call = "resolve"
	return domain.RevisionResolution{}, nil
}

func (s *buildAPI) RecordBuild(_ context.Context, project, repo string, in domain.BuildReport) (domain.Build, error) {
	s.calls, s.call = s.calls+1, "build"
	return domain.Build{Project: project, Repository: repo, BuildReport: in}, nil
}

func TestBuildRoutesUseDedicatedHandlers(t *testing.T) {
	s := &buildAPI{fakeService: &fakeService{}}
	h := NewHandlerWithBuildCredentials(s, "admin", nil, []BuildCredential{{Token: strings.Repeat("a", 32), Project: "demo", Repository: "backend", Components: []string{"service-b"}}})
	for _, test := range []struct {
		method, path, body, token, want string
	}{
		{"GET", "/v1/projects/demo/repositories", "", "admin", "list"},
		{"POST", "/v1/projects/demo/repositories", `{"id":"backend"}`, "admin", "register"},
		{"PATCH", "/v1/projects/demo/repositories/backend", `{"enabled":true}`, "admin", "enable"},
		{"GET", "/v1/projects/demo/repositories/backend/branches?page=2", "", "admin", "branches"},
		{"GET", "/v1/projects/demo/repositories/backend/commits?branch=main&page=2", "", "admin", "commits"},
		{"GET", "/v1/projects/demo/repositories/backend/resolve?component=api&ref=main", "", "admin", "resolve"},
		{"POST", "/v1/projects/demo/repositories/backend/builds", `{"component":"service-b"}`, strings.Repeat("a", 32), "build"},
	} {
		s.call = ""
		response := request(h, test.method, test.path, test.body, test.token)
		if response.Code != 200 || s.call != test.want {
			t.Fatalf("%s %s: status=%d call=%q body=%s", test.method, test.path, response.Code, s.call, response.Body.String())
		}
	}
}

func TestCICredentialsAreRestrictedToExactReportingScope(t *testing.T) {
	s := &buildAPI{fakeService: &fakeService{}}
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

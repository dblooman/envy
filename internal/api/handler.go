// Package api exposes the authoritative HTTP API. It never calls providers.
package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dblooman/envy/internal/domain"
)

// Service is the application boundary shared by HTTP handlers and their tests.
type Service interface {
	Logs(context.Context, string, string, domain.LogOptions) (domain.ComponentLogs, error)
	Events(context.Context, string, string, int) (domain.EventsPage, error)
	Create(context.Context, domain.CreateRequest, string) (domain.Composition, error)
	Get(context.Context, string) (domain.Composition, error)
	Destroy(context.Context, string) (domain.Composition, error)
	Update(context.Context, string, domain.UpdateRequest) (domain.Composition, error)
	List(context.Context, string, string, int) ([]domain.Composition, string, error)
	Projects(context.Context, string, int) ([]domain.Project, string, error)
	Components(context.Context, string, string, int) ([]domain.Component, string, error)
	Component(context.Context, string, string) (domain.Component, error)
	Baselines(context.Context, string, string, int) ([]domain.Baseline, string, error)
	activityService
	buildService
	catalogService
	frontendService
	onboardingService
	previewService
	recipeService
	RecordRejectedActivity(context.Context, domain.Activity) error
}

type handler struct {
	buildCredentials []BuildCredential
	service          Service
	auth             AuthConfig
	ready            func(context.Context) error
	installation     Installation
}

type Installation struct {
	NamespacePolicyMode  string `json:"namespace_policy_mode,omitempty"`
	NamespacePolicyReady bool   `json:"namespace_policy_ready"`
	ID                   string `json:"id"`
	Version              string `json:"version"`
	AuthMode             string `json:"auth_mode"`
	DefaultTTL           string `json:"default_ttl"`
	MaxTTL               string `json:"max_ttl"`
	MaxCompositions      int    `json:"max_compositions"`
	AuditRetention       string `json:"audit_retention,omitempty"`
	WebDir               string `json:"-"`
}

// NewHandler installs authenticated v1 routes and unauthenticated health probes.
// Empty credentials fail closed. ready checks dependencies, not reconciliation leadership.
func NewHandler(service Service, token string, ready func(context.Context) error) http.Handler {
	return NewHandlerWithBuildCredentials(service, token, ready, nil)
}

func NewHandlerWithBuildCredentials(service Service, token string, ready func(context.Context) error, credentials []BuildCredential) http.Handler {
	return NewConfiguredHandler(service, AuthConfig{Mode: "token", SharedToken: token}, Installation{AuthMode: "token"}, ready, credentials)
}

// NewAuthenticationHandler installs login and OAuth endpoints without an API service.
// Its protected API fallback is useful for authentication-only deployments and tests.
func NewAuthenticationHandler(auth AuthConfig) http.Handler {
	h := &handler{auth: normalizeAuth(auth)}
	mux := http.NewServeMux()
	installLoginRoutes(mux, h.auth)
	fallback := http.NewServeMux()
	fallback.HandleFunc("GET /v1/session", h.session)
	fallback.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, domain.NotFound("API route not found"))
	})
	mux.Handle("/v1/", h.authenticate(fallback))
	mux.Handle("/mcp", h.authenticate(h.remoteMCP(fallback)))
	return mux
}

func NewConfiguredHandler(service Service, auth AuthConfig, installation Installation, ready func(context.Context) error, credentials []BuildCredential) http.Handler {
	if service == nil {
		panic("api service is required")
	}

	auth = normalizeAuth(auth)
	installation.AuthMode = auth.Mode
	h := &handler{buildCredentials: credentials, service: service, auth: auth, ready: ready, installation: installation}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if h.ready != nil {
			if err := h.ready(r.Context()); err != nil {
				writeError(w, &domain.Error{Code: "unavailable", Message: "service is not ready", Retryable: true})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	v1 := http.NewServeMux()
	h.installGitHubRoutes(mux, v1)
	v1.HandleFunc("GET /v1/session", h.session)
	v1.HandleFunc("GET /v1/installation", h.installationInfo)
	v1.HandleFunc("GET /v1/activity", h.activity)
	v1.HandleFunc("GET /v1/compositions/{id}/revisions", h.revisions)
	v1.HandleFunc("GET /v1/compositions/{id}/revisions/{generation}", h.revision)
	v1.HandleFunc("POST /v1/recipes/export", h.exportRecipe)
	v1.HandleFunc("POST /v1/recipes/validate", h.validateRecipe)
	v1.HandleFunc("POST /v1/recipes/recreate", h.recreateRecipe)
	v1.HandleFunc("GET /v1/projects/{project}/repositories", h.sourceRepositories)
	v1.HandleFunc("POST /v1/projects/{project}/repositories", h.registerSourceRepository)
	v1.HandleFunc("PATCH /v1/projects/{project}/repositories/{repository}", h.enableSourceRepository)
	v1.HandleFunc("GET /v1/projects/{project}/repositories/{repository}/branches", h.sourceBranches)
	v1.HandleFunc("GET /v1/projects/{project}/repositories/{repository}/commits", h.sourceCommits)
	v1.HandleFunc("GET /v1/projects/{project}/repositories/{repository}/resolve", h.resolveRevision)
	v1.HandleFunc("POST /v1/projects/{project}/repositories/{repository}/builds", h.recordBuild)
	v1.HandleFunc("PUT /v1/projects/{project}/frontend-bindings/{frontend}/{revision}", h.bindFrontend)
	v1.HandleFunc("GET /v1/projects/{project}/frontend-bindings/{frontend}/{revision}", h.frontendBinding)
	v1.HandleFunc("GET /v1/projects/{project}/frontend-bindings/{frontend}/{revision}/resolve", h.resolveFrontend)
	v1.HandleFunc("POST /v1/projects/{project}/frontend-bindings/{frontend}/{revision}/deployment", h.publishFrontend)
	v1.HandleFunc("POST /v1/projects/{project}/frontend-bindings/{frontend}/{revision}/check", h.checkFrontend)
	v1.HandleFunc("GET /v1/compositions/{id}/frontend-bindings", h.listFrontendBindings)
	v1.HandleFunc("GET /v1/projects/{project}/baselines/{baseline}/components/{component}/preview-profile", h.inspectPreview)
	v1.HandleFunc("POST /v1/projects/{project}/baselines/{baseline}/components/{component}/preview-profile/discover", h.discoverPreview)
	v1.HandleFunc("POST /v1/projects/{project}/baselines/{baseline}/components/{component}/preview-profile/approve", h.approvePreview)
	v1.HandleFunc("POST /v1/catalog/validate", h.validateCatalog)
	v1.HandleFunc("POST /v1/catalog/apply", h.applyCatalog)
	v1.HandleFunc("POST /v1/projects", h.registerProject)
	v1.HandleFunc("POST /v1/projects/{project}/components", h.registerComponent)
	v1.HandleFunc("POST /v1/projects/{project}/baselines", h.registerBaseline)
	v1.HandleFunc("GET /v1/projects", h.projects)
	v1.HandleFunc("GET /v1/projects/{project}/components", h.components)
	v1.HandleFunc("GET /v1/projects/{project}/components/{component}", h.component)
	v1.HandleFunc("GET /v1/projects/{project}/baselines", h.baselines)
	v1.HandleFunc("POST /v1/compositions", h.create)
	v1.HandleFunc("GET /v1/compositions", h.list)
	v1.HandleFunc("GET /v1/compositions/{id}", h.get)
	v1.HandleFunc("GET /v1/compositions/{id}/status", h.status)
	v1.HandleFunc("GET /v1/compositions/{id}/endpoints", h.endpoints)
	v1.HandleFunc("DELETE /v1/compositions/{id}", h.destroy)
	v1.HandleFunc("PATCH /v1/compositions/{id}", h.update)
	v1.HandleFunc("GET /v1/compositions/{id}/components/{component}/logs", h.logs)
	v1.HandleFunc("GET /v1/compositions/{id}/events", h.events)
	v1.HandleFunc("GET /v1/compositions/{id}/verification", h.verification)
	v1.HandleFunc("GET /v1/compositions/{id}/observability", h.observability)
	v1.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) { writeError(w, domain.NotFound("API route not found")) })
	mux.Handle("/v1/", h.authenticate(h.recordRejected(v1)))
	installLoginRoutes(mux, auth)
	mux.Handle("/mcp", h.authenticate(h.remoteMCP(h.recordRejected(v1))))

	if installation.WebDir != "" {
		mux.Handle("/", spa(installation.WebDir))
	}

	return mux
}

func installLoginRoutes(mux *http.ServeMux, auth AuthConfig) {
	if auth.Login == nil {
		return
	}

	login := auth.Login.Handler()
	mux.Handle("/auth/", login)
	mux.Handle("/oauth/", login)
	mux.Handle("/.well-known/", login)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	return w.ResponseWriter.Write(data)
}

func (h *handler) recordRejected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracked := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete
		if r.URL.Path == "/v1/catalog/validate" || r.URL.Path == "/v1/recipes/export" || r.URL.Path == "/v1/recipes/validate" {
			tracked = false
		}

		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if !tracked || sw.status < 400 {
			return
		}

		resourceType, resourceID := "api_request", r.Pattern
		composition, project := "", r.PathValue("project")
		if id := r.PathValue("id"); id != "" {
			resourceType, resourceID, composition = "composition", id, id
		}

		_ = h.service.RecordRejectedActivity(r.Context(), domain.Activity{Action: r.Method + " " + r.Pattern, Outcome: "rejected", Project: project, ResourceType: resourceType, ResourceID: resourceID, Composition: composition})
	})
}

func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean("/" + r.URL.Path)
		candidate := filepath.Join(dir, clean)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}

		if filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

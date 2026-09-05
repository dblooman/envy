// Package application validates requests and commits durable intent before any
// runtime or routing provider is called.
package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type Repository interface {
	Create(context.Context, domain.Composition, string, string, int) (domain.Composition, error)
	Get(context.Context, string) (domain.Composition, error)
	List(context.Context, string, string, int) ([]domain.Composition, string, error)
	Destroy(context.Context, string) (domain.Composition, error)
	Projects(context.Context, string, int) ([]domain.Project, string, error)
	Components(context.Context, string, string, int) ([]domain.Component, string, error)
	Component(context.Context, string, string) (domain.Component, error)
	Baselines(context.Context, string, string, int) ([]domain.Baseline, string, error)
	Baseline(context.Context, string, string) (domain.Baseline, error)
}
type Config struct {
	DefaultTTL      time.Duration
	MaxTTL          time.Duration
	MaxCompositions int
	PreviewBaseURL  string
}
type Service struct {
	store Repository
	cfg   Config
}

func New(store Repository, cfg Config) *Service {
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 8 * time.Hour
	}
	if cfg.MaxTTL == 0 {
		cfg.MaxTTL = 24 * time.Hour
	}
	if cfg.MaxCompositions == 0 {
		cfg.MaxCompositions = 20
	}
	if cfg.PreviewBaseURL == "" {
		cfg.PreviewBaseURL = "http://envy.localhost:8080"
	}
	return &Service{store: store, cfg: cfg}
}

// NormalizeCreate produces the canonical logical request used by idempotency.
// TTL spellings with equal duration and omitted/default TTL normalize equally.
func NormalizeCreate(req domain.CreateRequest, cfg Config) (domain.CreateRequest, time.Duration, error) {
	if strings.TrimSpace(req.Project) == "" || strings.TrimSpace(req.Baseline) == "" {
		return req, 0, domain.Validation("project and baseline are required")
	}
	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 128 {
		return req, 0, domain.Validation("name must contain 1–128 characters")
	}
	if len(req.Overrides) != 1 {
		return req, 0, domain.Validation("exactly one service-b override is required")
	}
	o, ok := req.Overrides["service-b"]
	if !ok {
		return req, 0, domain.Validation("only service-b can be overridden")
	}
	if strings.TrimSpace(o.Image) == "" || len(o.Image) > 512 || strings.ContainsAny(o.Image, " \t\r\n") {
		return req, 0, domain.Validation("image must be a nonempty container image reference without whitespace")
	}
	if len(req.Resources) != 0 {
		return req, 0, domain.Validation("resource overrides are not supported; baseline resources are inherited")
	}
	req.Resources = nil
	ttl := cfg.DefaultTTL
	if ttl == 0 {
		ttl = 8 * time.Hour
	}
	max := cfg.MaxTTL
	if max == 0 {
		max = 24 * time.Hour
	}
	if req.TTL != "" {
		var err error
		ttl, err = time.ParseDuration(req.TTL)
		if err != nil {
			return req, 0, domain.Validation("ttl must be a valid Go duration")
		}
	}
	if ttl <= 0 || ttl > max {
		return req, 0, domain.Validation("ttl must be positive and no greater than " + max.String())
	}
	req.TTL = ttl.String()
	return req, ttl, nil
}
func (s *Service) Create(ctx context.Context, req domain.CreateRequest, key string) (domain.Composition, error) {
	var zero domain.Composition
	if len(key) > 128 {
		return zero, domain.Validation("idempotency key must be at most 128 printable ASCII characters")
	}
	for _, b := range []byte(key) {
		if b < 32 || b > 126 {
			return zero, domain.Validation("idempotency key must contain printable ASCII characters")
		}
	}
	req, ttl, err := NormalizeCreate(req, s.cfg)
	if err != nil {
		return zero, err
	}
	b, err := s.store.Baseline(ctx, req.Project, req.Baseline)
	if err != nil {
		return zero, err
	}
	profile, err := s.store.Component(ctx, req.Project, "service-b")
	if err != nil {
		return zero, err
	}
	if !profile.Overridable {
		return zero, domain.Validation("component does not allow image overrides")
	}
	if _, ok := b.Components["service-b"]; !ok {
		return zero, domain.Validation("baseline has no service-b binding")
	}
	canonical, _ := json.Marshal(req)
	digest := sha256.Sum256(canonical)
	id, err := RandomID()
	if err != nil {
		return zero, err
	}
	owner, err := RandomID()
	if err != nil {
		return zero, err
	}
	op, err := RandomID()
	if err != nil {
		return zero, err
	}
	u, err := url.Parse(s.cfg.PreviewBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return zero, &domain.Error{Code: "unavailable", Message: "preview base URL configuration is invalid"}
	}
	u.Host = "cmp-" + id + "." + u.Host
	u.Path = ""
	now := time.Now().UTC()
	c := domain.Composition{ID: id, Project: req.Project, Baseline: req.Baseline, BaselineRevision: b.Revision, Name: req.Name, Overrides: req.Overrides, Generation: 1, Phase: domain.PhaseCreated, ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now, Components: map[string]domain.ComponentObservation{}, Endpoints: map[string]domain.Endpoint{"public": {URL: u.String()}}, Conditions: []domain.Condition{{Type: "WorkloadReady", Message: "waiting for reconciliation"}, {Type: "RoutingConfigured", Message: "waiting for workload readiness"}, {Type: "RequestRoutingVerified", Message: "waiting for ingress verification"}}, LatestOperation: domain.Operation{ID: op, Kind: "create", Status: "pending"}, Runtime: domain.RuntimeState{OwnershipToken: owner}}
	for name, binding := range b.Components {
		c.Components[name] = domain.ComponentObservation{Source: "baseline", Status: "inherited", Image: binding.Image}
	}
	c.Components["service-b"] = domain.ComponentObservation{Source: "override", Status: "pending", Image: req.Overrides["service-b"].Image}
	return s.store.Create(ctx, c, key, hex.EncodeToString(digest[:]), s.cfg.MaxCompositions)
}
func RandomID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", &domain.Error{Code: "unavailable", Message: "secure identity generation is unavailable", Retryable: true}
	}
	return hex.EncodeToString(b[:]), nil
}
func ValidatePage(after string, limit int) error {
	if limit < 1 || limit > 100 {
		return domain.Validation("limit must be between 1 and 100")
	}
	if len(after) > 128 || strings.ContainsAny(after, "\x00\r\n") {
		return domain.Validation("invalid pagination cursor")
	}
	return nil
}
func (s *Service) Get(ctx context.Context, id string) (domain.Composition, error) {
	return s.store.Get(ctx, id)
}
func (s *Service) Destroy(ctx context.Context, id string) (domain.Composition, error) {
	return s.store.Destroy(ctx, id)
}
func (s *Service) List(ctx context.Context, project, after string, limit int) ([]domain.Composition, string, error) {
	if err := ValidatePage(after, limit); err != nil {
		return nil, "", err
	}
	return s.store.List(ctx, project, after, limit)
}
func (s *Service) Projects(ctx context.Context, after string, limit int) ([]domain.Project, string, error) {
	if err := ValidatePage(after, limit); err != nil {
		return nil, "", err
	}
	return s.store.Projects(ctx, after, limit)
}
func (s *Service) Components(ctx context.Context, project, after string, limit int) ([]domain.Component, string, error) {
	if err := ValidatePage(after, limit); err != nil {
		return nil, "", err
	}
	return s.store.Components(ctx, project, after, limit)
}
func (s *Service) Component(ctx context.Context, project, id string) (domain.Component, error) {
	return s.store.Component(ctx, project, id)
}
func (s *Service) Baselines(ctx context.Context, project, after string, limit int) ([]domain.Baseline, string, error) {
	if err := ValidatePage(after, limit); err != nil {
		return nil, "", err
	}
	return s.store.Baselines(ctx, project, after, limit)
}

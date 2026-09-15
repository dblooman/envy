package application

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type CatalogRepository interface {
	RegisterProject(context.Context, domain.Project) (domain.Project, error)
	RegisterComponent(context.Context, domain.Component) (domain.Component, error)
	RegisterBaseline(context.Context, domain.Baseline) (domain.Baseline, error)
}

func (s *Service) catalog() (CatalogRepository, error) {
	r, ok := s.store.(CatalogRepository)
	if !ok {
		return nil, &domain.Error{Code: "unavailable", Message: "catalog registration is unavailable"}
	}

	return r, nil
}

func (s *Service) RegisterProject(ctx context.Context, p domain.Project) (domain.Project, error) {
	if !domain.ValidCatalogID(p.ID) || strings.TrimSpace(p.Name) == "" || len(p.Name) > 128 {
		return domain.Project{}, domain.Validation("project requires a DNS-label ID and a name of 1–128 characters")
	}

	r, err := s.catalog()
	if err != nil {
		return domain.Project{}, err
	}

	return r.RegisterProject(ctx, p)
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validPath(path string) bool {
	u, err := url.Parse(path)
	return err == nil && strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") && len(path) <= 256 && u.Host == "" && u.RawQuery == "" && u.Fragment == "" && !strings.ContainsAny(path, "\r\n")
}

func ValidateComponent(c domain.Component) error {
	if !domain.ValidCatalogID(c.ID) || !domain.ValidCatalogID(c.Project) {
		return domain.Validation("component and project IDs must be DNS labels")
	}

	if c.Protocol != "http" || (c.Profile != "http-small" && c.Profile != "deployment") || c.Port < 1 || c.Port > 65535 || (c.Profile == "http-small" && c.Port < 1024) {
		return domain.Validation("component requires http, a supported profile and a valid Service port; http-small ports must be unprivileged")
	}

	if c.Profile == "deployment" {
		if c.HealthPath != "" || c.ReadinessPath != "" || len(c.Env) > 0 || len(c.ImagePullSecrets) > 0 {
			return domain.Validation("deployment profiles derive configuration through preview discovery and approval")
		}
	} else if !validPath(c.HealthPath) || !validPath(c.ReadinessPath) {
		return domain.Validation("health_path and readiness_path must be absolute HTTP paths")
	}

	if len(c.Repository) > 2048 || len(c.Env) > 32 {
		return domain.Validation("repository or environment exceeds catalog bounds")
	}

	if len(c.ImagePullSecrets) > 8 {
		return domain.Validation("at most eight image pull Secrets are allowed")
	}

	seenPullSecrets := map[string]bool{}
	for _, name := range c.ImagePullSecrets {
		if !domain.ValidCatalogID(name) || seenPullSecrets[name] {
			return domain.Validation("image pull Secret names must be unique DNS labels")
		}

		seenPullSecrets[name] = true
	}

	total := 0
	for key, value := range c.Env {
		if !envName.MatchString(key) || len(key) > 128 || key == "POD_UID" || strings.HasPrefix(key, "ENVY_") || strings.ContainsRune(value, 0) {
			return domain.Validation("environment contains an invalid or reserved variable")
		}

		total += len(key) + len(value)
	}

	if total > 8192 {
		return domain.Validation("environment must not exceed 8 KiB")
	}

	return nil
}

func (s *Service) RegisterComponent(ctx context.Context, c domain.Component) (domain.Component, error) {
	if err := ValidateComponent(c); err != nil {
		return domain.Component{}, err
	}

	if err := s.validatePullSecrets(c); err != nil {
		return domain.Component{}, err
	}

	r, err := s.catalog()
	if err != nil {
		return domain.Component{}, err
	}

	return r.RegisterComponent(ctx, c)
}

func (s *Service) validateBaseline(ctx context.Context, b domain.Baseline, profiles map[string]domain.Component) (domain.Baseline, error) {
	zero := domain.Baseline{}
	if !domain.ValidCatalogID(b.ID) || !domain.ValidCatalogID(b.Project) || !domain.ValidCatalogID(b.Revision) {
		return zero, domain.Validation("baseline, project and revision IDs must be DNS labels")
	}

	if (b.Routing.GatewayNamespace != "" && !domain.ValidCatalogID(b.Routing.GatewayNamespace)) || (b.Routing.GatewaySectionName != "" && !domain.ValidCatalogID(b.Routing.GatewaySectionName)) {
		return b, domain.Validation("invalid gateway namespace or section name")
	}

	if !domain.ValidCatalogID(b.Routing.Namespace) || !domain.ValidCatalogID(b.Routing.Gateway) {
		return zero, domain.Validation("routing requires an existing namespace and Gateway name")
	}

	u, err := url.Parse(b.Endpoint)
	base, baseErr := url.Parse(s.cfg.PreviewBaseURL)
	if err != nil || baseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Scheme != base.Scheme || u.Port() != base.Port() || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Hostname(), "."+base.Hostname()) || strings.HasPrefix(u.Hostname(), "cmp-") {
		return zero, domain.Validation("baseline endpoint must be an HTTP(S) hostname under the configured preview domain and port, outside the cmp- prefix")
	}

	if !domain.ValidCatalogID(strings.TrimSuffix(u.Hostname(), "."+base.Hostname())) {
		return zero, domain.Validation("baseline hostname must be a single DNS label under the preview domain")
	}

	u.Path = ""
	b.Endpoint = u.String()
	if len(b.Components) < 1 || len(b.Components) > 20 {
		return zero, domain.Validation("baseline requires 1–20 components")
	}

	names := make([]string, 0, len(b.Components))
	switch b.Verification.Kind {
	case "envy-chain":
		if len(b.Verification.Chain) != len(b.Components) || b.Verification.Path != "" || b.Verification.ExpectedStatus != 0 {
			return zero, domain.Validation("envy-chain verification must list every component and cannot include HTTP probe settings")
		}

		names = b.Verification.Chain
		if b.Routing.EntryComponent != names[0] {
			return zero, domain.Validation("entry_component must be the first verification hop")
		}
	case "http":
		if b.Verification.Path == "" {
			b.Verification.Path = "/"
		}

		if b.Verification.ExpectedStatus == 0 {
			b.Verification.ExpectedStatus = 200
		}

		if len(b.Verification.Chain) != 0 || !validPath(b.Verification.Path) || b.Verification.ExpectedStatus < 200 || b.Verification.ExpectedStatus > 299 {
			return zero, domain.Validation("http verification requires an absolute path, a 2xx expected_status and no chain")
		}

		for id := range b.Components {
			names = append(names, id)
		}

		slices.Sort(names)
		if _, ok := b.Components[b.Routing.EntryComponent]; !ok {
			return zero, domain.Validation("entry_component must be bound in the baseline")
		}
	default:
		return zero, domain.Validation("verification kind must be envy-chain or http")
	}

	seen := map[string]bool{}
	hosts := map[string]bool{}
	if profiles == nil {
		profiles = map[string]domain.Component{}
	}

	for _, id := range names {
		binding, ok := b.Components[id]
		if !ok || seen[id] {
			return zero, domain.Validation("verification chain must list each bound component exactly once")
		}

		seen[id] = true
		parts := strings.Split(binding.ServiceHost, ".")
		if len(parts) != 5 || parts[1] != b.Routing.Namespace || !domain.ValidCatalogID(parts[0]) || strings.Join(parts[2:], ".") != "svc.cluster.local" || binding.Port < 1 || binding.Port > 65535 || hosts[binding.ServiceHost] {
			return zero, domain.Validation("bindings require distinct Service FQDNs in the routing namespace and valid ports")
		}

		hosts[binding.ServiceHost] = true
		if binding.Image == "" || len(binding.Image) > 512 || strings.ContainsAny(binding.Image, " \t\r\n") {
			return zero, domain.Validation("binding image is required")
		}

		if _, ok := profiles[id]; !ok {
			c, err := s.store.Component(ctx, b.Project, id)
			if err != nil {
				return zero, err
			}

			profiles[id] = c
		}
	}

	if err := domain.ValidateMessaging(b); err != nil {
		return zero, err
	}

	if len(b.PubSub) > 0 {
		if s.cfg.Messaging == nil {
			return zero, domain.Validation("Pub/Sub provider is not enabled")
		}

		if err := s.cfg.Messaging.Validate(ctx, b); err != nil {
			return zero, err
		}
	}

	if s.cfg.CatalogValidator == nil {
		return zero, &domain.Error{Code: "unavailable", Message: "baseline validation is unavailable", Retryable: true}
	}

	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.cfg.CatalogValidator.ValidateBaseline(check, b, profiles); err != nil {
		return zero, err
	}

	return b, nil
}

func (s *Service) RegisterBaseline(ctx context.Context, b domain.Baseline) (domain.Baseline, error) {
	b, err := s.validateBaseline(ctx, b, nil)
	if err != nil {
		return domain.Baseline{}, err
	}

	r, err := s.catalog()
	if err != nil {
		return domain.Baseline{}, err
	}

	return r.RegisterBaseline(ctx, b)
}

// BaselineChecks composes the concrete read-only connectivity/ownership checks.
type BaselineChecks []domain.CatalogValidator

func (checks BaselineChecks) ValidateBaseline(ctx context.Context, b domain.Baseline, c map[string]domain.Component) error {
	for _, check := range checks {
		if err := check.ValidateBaseline(ctx, b, c); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) validatePullSecrets(c domain.Component) error {
	for _, name := range c.ImagePullSecrets {
		if !slices.Contains(s.cfg.ApprovedImagePullSecrets, name) {
			return domain.Validation("image pull Secret is not operator-approved: " + name)
		}
	}

	return nil
}

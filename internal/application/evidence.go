package application

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func (s *Service) Verification(ctx context.Context, id, after string, limit int) (domain.VerificationPage, error) {
	if err := ValidatePage(after, limit); err != nil {
		return domain.VerificationPage{}, err
	}

	if _, err := domain.EventCursor(after); err != nil {
		return domain.VerificationPage{}, err
	}

	r, ok := s.store.(interface {
		Verification(context.Context, string, string, int) (domain.VerificationPage, error)
	})
	if !ok {
		return domain.VerificationPage{}, &domain.Error{Code: "unavailable", Message: "verification history is unavailable"}
	}

	page, err := r.Verification(ctx, id, after, limit)
	if err != nil {
		return page, err
	}

	composition, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.VerificationPage{}, err
	}

	for i := range page.Items {
		page.Items[i].Freshness = domain.EvidenceFreshness(composition, page.Items[i])
	}

	return page, nil
}

// Templates are operator configuration, never preview-provided URLs. Substitutions
// are restricted to paths and queries; credentials and fragments are rejected.
func ValidateObservabilityTemplates(templates []domain.ObservabilityTemplate) error {
	for _, t := range templates {
		if err := validateObservabilityTemplate(t); err != nil {
			return err
		}
	}

	return nil
}

func validateObservabilityTemplate(t domain.ObservabilityTemplate) error {
	u, err := url.Parse(t.URL)
	if err != nil {
		return domain.Validation("invalid observability URL")
	}

	if t.Project == "" || t.Label == "" {
		return domain.Validation("observability project and label are required")
	}

	if t.Kind != "logs" && t.Kind != "traces" && t.Kind != "dashboard" {
		return domain.Validation("invalid observability kind")
	}

	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(u.Host, "{}") {
		return domain.Validation("invalid observability URL")
	}

	if err := validateLinkQuery(u); err != nil {
		return err
	}

	return validateLinkPlaceholders(t.URL)
}

func validateLinkQuery(u *url.URL) error {
	for key := range u.Query() {
		for _, secret := range []string{"token", "secret", "password", "api_key", "apikey", "authorization"} {
			if strings.Contains(strings.ToLower(key), secret) {
				return domain.Validation("observability links must not contain credentials")
			}
		}
	}

	return nil
}

func validateLinkPlaceholders(raw string) error {
	allowed := map[string]bool{"installation": true, "project": true, "preview": true, "component": true, "from": true, "to": true}
	for {
		start := strings.Index(raw, "{")
		if start < 0 {
			break
		}

		if strings.Contains(raw[:start], "}") {
			return domain.Validation("invalid observability placeholder")
		}

		end := strings.Index(raw[start:], "}")
		if end < 0 || !allowed[raw[start+1:start+end]] {
			return domain.Validation("unsupported observability placeholder")
		}

		raw = raw[start+end+1:]
	}

	if strings.Contains(raw, "}") {
		return domain.Validation("invalid observability placeholder")
	}

	return nil
}

func (s *Service) Observability(ctx context.Context, id, component string) (domain.ObservabilityLinks, error) {
	out := domain.ObservabilityLinks{Items: []domain.ObservabilityLink{}}
	c, err := s.store.Get(ctx, id)
	if err != nil {
		return out, err
	}

	if component != "" {
		if _, ok := c.Components[component]; !ok {
			return out, domain.NotFound("component is not part of composition")
		}
	}

	if err = ValidateObservabilityTemplates(s.cfg.Observability); err != nil {
		return out, err
	}

	now := time.Now().UTC()
	from := c.UpdatedAt.Add(-15 * time.Minute)
	values := map[string]string{"installation": s.cfg.Installation, "project": c.Project, "preview": c.ID, "component": component, "from": from.Format(time.RFC3339), "to": now.Format(time.RFC3339)}
	for _, t := range s.cfg.Observability {
		if t.Project != c.Project || (component == "" && strings.Contains(t.URL, "{component}")) {
			continue
		}

		u, _ := url.Parse(t.URL)
		// Escape raw path segments individually so substitutions cannot add separators.
		path := u.EscapedPath()
		query := u.RawQuery
		for key, value := range values {
			path = strings.ReplaceAll(path, url.PathEscape("{"+key+"}"), url.PathEscape(value))
			query = strings.ReplaceAll(query, "{"+key+"}", url.QueryEscape(value))
			query = strings.ReplaceAll(query, url.QueryEscape("{"+key+"}"), url.QueryEscape(value))
		}

		decoded, err := url.PathUnescape(path)
		if err != nil {
			return out, err
		}

		u.Path = decoded
		u.RawPath = path
		u.RawQuery = query
		out.Items = append(out.Items, domain.ObservabilityLink{Label: t.Label, Kind: t.Kind, URL: u.String()})
	}

	return out, nil
}

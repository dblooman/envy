package domain

import (
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type FrontendKey struct {
	Project  string `json:"project"`
	Frontend string `json:"frontend"`
	Revision string `json:"revision"`
}
type BindFrontendRequest struct {
	Composition string `json:"composition"`
	Repository  string `json:"repository"`
}
type PublishFrontendRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	URL             string `json:"url"`
}
type FrontendCheckRequest struct {
	ExpectedVersion       int64  `json:"expected_version"`
	CompositionGeneration int64  `json:"composition_generation"`
	Status                string `json:"status"`
	Message               string `json:"message"`
}
type FrontendCheck struct {
	CompositionGeneration int64     `json:"composition_generation"`
	Status                string    `json:"status"`
	Message               string    `json:"message"`
	ReportedAt            time.Time `json:"reported_at"`
}
type FrontendBinding struct {
	Project     string         `json:"project"`
	Frontend    string         `json:"frontend"`
	Revision    string         `json:"revision"`
	Composition string         `json:"composition"`
	Repository  string         `json:"repository"`
	Version     int64          `json:"version"`
	URL         string         `json:"url,omitempty"`
	Check       *FrontendCheck `json:"check,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}
type FrontendBindingView struct {
	Binding               FrontendBinding `json:"binding"`
	CompositionPhase      Phase           `json:"composition_phase"`
	CompositionGeneration int64           `json:"composition_generation"`
	ExpiresAt             time.Time       `json:"expires_at"`
	Ready                 bool            `json:"ready"`
	VerificationLevel     string          `json:"verification_level"`
	CheckState            string          `json:"check_state"`
}
type FrontendResolution struct {
	Project               string    `json:"project"`
	Frontend              string    `json:"frontend"`
	Revision              string    `json:"revision"`
	Composition           string    `json:"composition"`
	CompositionGeneration int64     `json:"composition_generation"`
	BindingVersion        int64     `json:"binding_version"`
	APIURL                string    `json:"api_url"`
	ExpiresAt             time.Time `json:"expires_at"`
	VerificationLevel     string    `json:"verification_level"`
}

var frontendRevision = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

func ValidateFrontendKey(k FrontendKey) error {
	if !ValidCatalogID(k.Project) || !ValidCatalogID(k.Frontend) || !frontendRevision.MatchString(k.Revision) {
		return Validation("project/frontend must be catalog identifiers and revision a full lowercase Git commit SHA (40 or 64 hex characters)")
	}
	return nil
}

// PublicFrontendURL validates a recorded link; the control plane never fetches it.
func PublicFrontendURL(raw string, localHTTP bool) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\\") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	return localHTTP && u.Scheme == "http" && (host == "localhost" || strings.HasSuffix(host, ".localhost") || (ip != nil && ip.IsLoopback()))
}

func FrontendCompositionAvailable(c Composition, now time.Time, ready bool) error {
	if c.DeletionRequested || c.Phase == PhaseDestroying || c.Phase == PhaseDestroyed || !c.ExpiresAt.After(now) {
		return &Error{Code: "gone", Message: "bound composition is expired or being destroyed; no fallback is available", Project: c.Project, Composition: c.ID}
	}
	if ready && (c.Phase != PhaseReady || c.Generation != c.ObservedGeneration || !c.Endpoints["public"].Ready || c.Endpoints["public"].URL == "") {
		return &Error{Code: "conflict", Message: "bound composition is not ready for a frontend build", Retryable: true, Project: c.Project, Composition: c.ID}
	}
	return nil
}

func ViewFrontend(b FrontendBinding, c Composition, now time.Time) FrontendBindingView {
	v := FrontendBindingView{Binding: b, CompositionPhase: c.Phase, CompositionGeneration: c.Generation, ExpiresAt: c.ExpiresAt, Ready: FrontendCompositionAvailable(c, now, true) == nil, VerificationLevel: c.VerificationLevel, CheckState: "not_reported"}
	if v.VerificationLevel == "" {
		v.VerificationLevel = "none"
	}
	if b.Check != nil {
		v.CheckState = "stale"
		if v.Ready && b.Check.CompositionGeneration == c.Generation {
			v.CheckState = "current"
		}
	}
	return v
}

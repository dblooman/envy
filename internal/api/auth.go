package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

type MachineCredential struct {
	Token       string `json:"token"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type AuthConfig struct {
	Mode               string
	SharedToken        string
	MachineCredentials []MachineCredential
	IdentityHeader     string
	EmailHeader        string
	ProxySecret        string
	TrustedProxies     []netip.Prefix
}

type Session struct {
	Principal    domain.Principal `json:"principal"`
	AuthMode     string           `json:"auth_mode"`
	Channel      string           `json:"channel"`
	Capabilities []string         `json:"capabilities"`
}

func normalizeAuth(cfg AuthConfig) AuthConfig {
	if cfg.Mode == "" {
		cfg.Mode = "token"
	}
	if cfg.IdentityHeader == "" {
		cfg.IdentityHeader = "X-Envy-User"
	}
	if cfg.EmailHeader == "" {
		cfg.EmailHeader = "X-Envy-Email"
	}
	return cfg
}

func bearer(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", false
	}
	value := strings.TrimPrefix(values[0], "Bearer ")
	return value, value != ""
}

func secureEqual(a, b string) bool {
	ah, bh := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return a != "" && b != "" && subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}

func proxyTrusted(remote string, prefixes []netip.Prefix) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, prefix := range prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func requestMetadata(r *http.Request, principal domain.Principal) context.Context {
	channel := domain.ValidChannel(r.Header.Get("X-Envy-Channel"))
	if principal.Kind == "human" && channel == "api" && r.Header.Get("Sec-Fetch-Site") != "" {
		channel = "web"
	}
	task := strings.TrimSpace(r.Header.Get("X-Envy-Task"))
	if len(task) > 200 {
		task = task[:200]
	}
	return domain.WithRequestIdentity(r.Context(), domain.RequestIdentity{Principal: principal, Channel: channel, Task: task})
}

func csrfAllowed(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || len(r.Cookies()) == 0 {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if scope := buildCredential(r, h.buildCredentials); scope != nil {
			principal := domain.Principal{Kind: "service", ID: "build:" + scope.Project + "/" + scope.Repository, DisplayName: "Build reporter"}
			next.ServeHTTP(w, r.WithContext(domain.WithRequestIdentity(context.WithValue(r.Context(), buildScopeKey{}, scope), domain.RequestIdentity{Principal: principal, Channel: "github"})))
			return
		}
		if token, ok := bearer(r); ok {
			for _, credential := range h.auth.MachineCredentials {
				if secureEqual(token, credential.Token) {
					p := domain.Principal{Kind: "service", ID: credential.ID, DisplayName: credential.DisplayName}
					next.ServeHTTP(w, r.WithContext(requestMetadata(r, p)))
					return
				}
			}
			if secureEqual(token, h.auth.SharedToken) {
				p := domain.Principal{Kind: "shared", ID: "shared-token", DisplayName: "Shared API credential"}
				next.ServeHTTP(w, r.WithContext(requestMetadata(r, p)))
				return
			}
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, &domain.Error{Code: "unauthorized", Message: "valid bearer credentials are required"})
			return
		}
		switch h.auth.Mode {
		case "none":
			p := domain.Principal{Kind: "anonymous", ID: "anonymous", DisplayName: "Anonymous"}
			next.ServeHTTP(w, r.WithContext(requestMetadata(r, p)))
		case "proxy":
			if !proxyTrusted(r.RemoteAddr, h.auth.TrustedProxies) || !secureEqual(r.Header.Get("X-Envy-Proxy-Secret"), h.auth.ProxySecret) {
				writeError(w, &domain.Error{Code: "unauthorized", Message: "request did not arrive through the trusted identity proxy"})
				return
			}
			ids := r.Header.Values(h.auth.IdentityHeader)
			emails := r.Header.Values(h.auth.EmailHeader)
			if len(ids) != 1 || strings.TrimSpace(ids[0]) == "" || len(ids[0]) > 200 || len(emails) > 1 {
				writeError(w, &domain.Error{Code: "unauthorized", Message: "trusted proxy identity is missing or ambiguous"})
				return
			}
			if !csrfAllowed(r) {
				writeError(w, &domain.Error{Code: "unauthorized", Message: "cross-origin browser mutation rejected"})
				return
			}
			p := domain.Principal{Kind: "human", ID: strings.TrimSpace(ids[0]), DisplayName: strings.TrimSpace(ids[0])}
			if len(emails) == 1 {
				p.Email = strings.TrimSpace(emails[0])
			}
			next.ServeHTTP(w, r.WithContext(requestMetadata(r, p)))
		default:
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, &domain.Error{Code: "unauthorized", Message: "valid bearer credentials are required"})
		}
	})
}

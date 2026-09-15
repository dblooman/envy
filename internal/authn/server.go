package authn

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/dblooman/envy/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"golang.org/x/oauth2"
)

const browserCookie = "envy_session"
const csrfCookie = "envy_csrf"

type RegisteredClient struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Redirects []string `json:"redirect_uris"`
}
type Config struct {
	Clients            []RegisteredClient
	Mode               string
	Origin             string
	Password           string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleDomains      []string
	GoogleEmails       []string
	// Issuer is set only by tests; production uses Google's issuer.
	GoogleIssuer string
}
type Identity struct {
	Principal domain.Principal `json:"principal"`
	Mode      string           `json:"mode"`
	Version   string           `json:"version"`
	Domain    string           `json:"domain,omitempty"`
	Verified  bool             `json:"verified"`
	Issued    time.Time        `json:"issued"`
}
type oauthSession struct {
	fosite.DefaultSession
	Identity Identity  `json:"identity"`
	Resource string    `json:"resource"`
	Until    time.Time `json:"until"`
}

func (s *oauthSession) Clone() fosite.Session {
	b, _ := json.Marshal(s)
	var v oauthSession
	_ = json.Unmarshal(b, &v)
	return &v
}
func (s *oauthSession) SetExpiresAt(t fosite.TokenType, v time.Time) {
	if t == fosite.RefreshToken && !s.Until.IsZero() && v.After(s.Until) {
		v = s.Until
	}
	s.DefaultSession.SetExpiresAt(t, v)
}

type authGeneration struct {
	Fingerprint string
	Epoch       string
}
type Server struct {
	epoch  string
	cfg    Config
	pool   *pgxpool.Pool
	key    []byte
	google *oidc.Provider
	oauth  oauth2.Config
}

func Random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func interactive(mode string) bool { return mode == "password" || mode == "google" }
func Validate(c Config) error {
	switch c.Mode {
	case "dev", "none", "proxy", "token", "password", "google":
	default:
		return fmt.Errorf("invalid authentication mode")
	}
	if c.Origin != "" && interactive(c.Mode) {
		u, e := url.Parse(c.Origin)
		if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()))) {
			return fmt.Errorf("external origin must be HTTPS, or HTTP on loopback, without a path")
		}
	}
	if interactive(c.Mode) && c.Origin == "" {
		return fmt.Errorf("ENVY_EXTERNAL_ORIGIN is required for password and Google modes")
	}
	if c.Mode == "google" && (c.GoogleClientID == "" || c.GoogleClientSecret == "" || len(c.GoogleDomains)+len(c.GoogleEmails) == 0) {
		return fmt.Errorf("Google mode requires client ID, client secret and allowed domains or emails")
	}
	for _, client := range c.Clients {
		if client.ID == "" || len(client.ID) > 200 || len(client.Redirects) == 0 {
			return fmt.Errorf("OAuth clients require ID and redirect URIs")
		}
		for _, uri := range client.Redirects {
			if !validRedirect(uri) {
				return fmt.Errorf("OAuth client redirect URI must be HTTPS or HTTP loopback")
			}
		}
	}
	return nil
}
func New(ctx context.Context, c Config, pool *pgxpool.Pool) (*Server, error) {
	c.Origin = strings.TrimRight(c.Origin, "/")
	if c.Password == "" {
		c.Password = "admin"
	}
	if err := Validate(c); err != nil {
		return nil, err
	}
	s := &Server{cfg: c, pool: pool}
	err := transaction(ctx, pool, func(db *records) error {
		var key string
		err := db.get(ctx, "secret", "oauth-hmac", &key)
		if errors.Is(err, fosite.ErrNotFound) {
			key = Random()
			err = db.put(ctx, "secret", "oauth-hmac", key, time.Now().AddDate(100, 0, 0))
		}
		if err != nil {
			return err
		}
		s.key = []byte(key)
		var generation authGeneration
		err = db.get(ctx, "configuration", "active", &generation)
		if err != nil && !errors.Is(err, fosite.ErrNotFound) {
			return err
		}
		if generation.Fingerprint != s.fingerprint() {
			generation = authGeneration{s.fingerprint(), Random()}
			if err = db.put(ctx, "configuration", "active", generation, time.Now().AddDate(100, 0, 0)); err != nil {
				return err
			}
		}
		s.epoch = generation.Epoch

		for _, client := range c.Clients {
			until := time.Now().AddDate(100, 0, 0)
			v := fosite.DefaultClient{ID: client.ID, Public: true, RedirectURIs: client.Redirects, GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, Scopes: []string{"envy", "offline_access"}, Audience: []string{c.Origin + "/v1", c.Origin + "/mcp"}}
			if err := db.put(ctx, "client", client.ID, v, until); err != nil {
				return err
			}
			if err := db.put(ctx, "client-name", client.ID, client.Name, until); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("initialize authentication storage: %w", err)
	}
	if c.Mode == "google" {
		issuer := c.GoogleIssuer
		if issuer == "" {
			issuer = "https://accounts.google.com"
		}
		discoveryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		p, err := oidc.NewProvider(discoveryCtx, issuer)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("discover Google identity provider: %w", err)
		}
		s.google = p
		s.oauth = oauth2.Config{ClientID: c.GoogleClientID, ClientSecret: c.GoogleClientSecret, Endpoint: p.Endpoint(), RedirectURL: c.Origin + "/auth/google/callback", Scopes: []string{oidc.ScopeOpenID, "email", "profile"}}
	}
	return s, nil
}
func (s *Server) version() string { return digest(s.epoch + ":" + s.fingerprint()) }
func (s *Server) fingerprint() string {
	if s.cfg.Mode == "password" {
		return digest("password:" + s.cfg.Password)
	}
	return digest(s.cfg.Mode + ":" + s.cfg.GoogleClientID)
}
func (s *Server) valid(ctx context.Context, db *records, i Identity) bool {
	if i.Mode != s.cfg.Mode || i.Version != s.version() {
		return false
	}
	var generation authGeneration
	if db.get(ctx, "configuration", "active", &generation) != nil || generation.Epoch != s.epoch || generation.Fingerprint != s.fingerprint() {
		return false
	}
	if i.Mode == "google" && !s.admitted(i.Principal.Email, i.Domain, i.Verified) {
		return false
	}
	var after time.Time
	err := db.get(ctx, "logout-all", i.Principal.ID, &after)
	return (errors.Is(err, fosite.ErrNotFound) || err == nil) && (after.IsZero() || i.Issued.After(after))
}
func (s *Server) admitted(email, domain string, verified bool) bool {
	if !verified {
		return false
	}
	for _, v := range s.cfg.GoogleEmails {
		if strings.EqualFold(strings.TrimSpace(v), email) {
			return true
		}
	}
	for _, v := range s.cfg.GoogleDomains {
		if domain != "" && strings.EqualFold(strings.TrimSpace(v), domain) {
			return true
		}
	}
	return false
}
func (s *Server) provider(db *records) fosite.OAuth2Provider {
	cfg := &fosite.Config{GlobalSecret: s.key, AccessTokenLifespan: 15 * time.Minute, RefreshTokenLifespan: 30 * 24 * time.Hour, AuthorizeCodeLifespan: time.Minute, EnforcePKCE: true, EnforcePKCEForPublicClients: true, EnablePKCEPlainChallengeMethod: false, RefreshTokenScopes: []string{}}
	return compose.Compose(cfg, &oauthStore{db, s}, compose.NewOAuth2HMACStrategy(cfg), compose.OAuth2AuthorizeExplicitFactory, compose.OAuth2RefreshTokenGrantFactory, compose.OAuth2TokenIntrospectionFactory, compose.OAuth2TokenRevocationFactory, compose.OAuth2PKCEFactory)
}
func (s *Server) secure() bool { return strings.HasPrefix(s.cfg.Origin, "https://") }
func (s *Server) cookieName(name string) string {
	if s.secure() {
		return "__Host-" + name
	}
	return name
}
func (s *Server) cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(name), Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: s.secure(), SameSite: http.SameSiteLaxMode})
}
func cookieValue(r *http.Request, name string) string {
	var value string
	for _, c := range r.Cookies() {
		if c.Name == name {
			if value != "" {
				return ""
			}
			value = c.Value
		}
	}
	return value
}
func equal(a, b string) bool {
	return a != "" && b != "" && subtle.ConstantTimeCompare([]byte(digest(a)), []byte(digest(b))) == 1
}
func (s *Server) csrf(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Values("Origin")
	if len(origin) != 1 || origin[0] != s.cfg.Origin {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return false
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		token = r.FormValue("csrf_token")
	}
	if !equal(token, cookieValue(r, s.cookieName(csrfCookie))) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return false
	}
	return true
}
func (s *Server) issue(ctx context.Context, db *records, w http.ResponseWriter, i Identity) error {
	i.Issued = time.Now().UTC()
	i.Mode = s.cfg.Mode
	i.Version = s.version()
	token := Random()
	if err := db.put(ctx, "identity", i.Principal.ID, i, time.Now().AddDate(100, 0, 0)); err != nil {
		return err
	}
	if err := db.put(ctx, "browser", digest(token), i, time.Now().Add(7*24*time.Hour)); err != nil {
		return err
	}
	s.cookie(w, browserCookie, token, 7*24*3600)
	return nil
}
func (s *Server) browser(ctx context.Context, db *records, r *http.Request) (Identity, error) {
	var i Identity
	token := cookieValue(r, s.cookieName(browserCookie))
	if token == "" {
		return i, fosite.ErrNotFound
	}
	if err := db.get(ctx, "browser", digest(token), &i); err != nil {
		return i, err
	}
	if !s.valid(ctx, db, i) {
		return i, fosite.ErrNotFound
	}
	return i, nil
}

// Authenticate never accepts browser cookies for the remote MCP resource.
func (s *Server) Authenticate(r *http.Request, resource string) (domain.Principal, error) {
	var p domain.Principal
	err := transaction(r.Context(), s.pool, func(db *records) error {
		if r.Header.Get("Authorization") != "" {
			h := r.Header.Values("Authorization")
			if len(h) != 1 || !strings.HasPrefix(h[0], "Bearer ") {
				return fosite.ErrRequestUnauthorized
			}
			tok := strings.TrimPrefix(h[0], "Bearer ")
			_, req, err := s.provider(db).IntrospectToken(r.Context(), tok, fosite.AccessToken, &oauthSession{}, "envy")
			if err != nil {
				return err
			}
			sess, ok := req.GetSession().(*oauthSession)
			if !ok || sess.Resource != s.cfg.Origin+resource {
				return fosite.ErrRequestUnauthorized
			}
			p = sess.Identity.Principal
			return nil
		}
		if resource != "/v1" {
			return fosite.ErrRequestUnauthorized
		}
		i, err := s.browser(r.Context(), db, r)
		if err != nil {
			return err
		}
		p = i.Principal
		return nil
	})
	return p, err
}
func (s *Server) BrowserMutationAllowed(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
		return true
	}
	return s.csrf(w, r)
}
func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
func safeReturn(v string) string {
	u, err := url.Parse(v)
	if err != nil || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.ContainsAny(v, "\\\r\n") || u.IsAbs() || u.Host != "" {
		return "/"
	}
	return v
}
func (s *Server) rate(ctx context.Context, db *records, key string, max int) bool {
	var n int
	err := db.get(ctx, "rate", key, &n)
	if err != nil && !errors.Is(err, fosite.ErrNotFound) {
		return false
	}
	if n >= max {
		return false
	}
	return db.put(ctx, "rate", key, n+1, time.Now().Truncate(time.Minute).Add(time.Minute)) == nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/config", func(w http.ResponseWriter, r *http.Request) {
		token := cookieValue(r, s.cookieName(csrfCookie))
		if token == "" {
			token = Random()
			s.cookie(w, csrfCookie, token, 7*24*3600)
		}
		jsonResponse(w, map[string]any{"mode": s.cfg.Mode, "csrf_token": token})
	})
	if interactive(s.cfg.Mode) {
		mux.HandleFunc("POST /auth/password", s.atomic(s.password))
		mux.HandleFunc("POST /auth/logout", s.atomic(s.logout))
		mux.HandleFunc("POST /auth/logout-all", s.atomic(s.logout))
		mux.HandleFunc("GET /auth/google/start", s.googleStart)
		mux.HandleFunc("GET /auth/google/callback", s.googleCallback)
		mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.metadata)
		mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.metadata)
		mux.HandleFunc("GET /.well-known/oauth-protected-resource/v1", s.metadata)
		mux.HandleFunc("POST /oauth/register", s.atomic(s.register))
		mux.HandleFunc("GET /oauth/authorize", s.atomic(s.authorize))
		mux.HandleFunc("POST /oauth/authorize", s.atomic(s.authorize))
		mux.HandleFunc("POST /oauth/token", s.atomic(s.token))
		mux.HandleFunc("POST /oauth/revoke", s.atomic(s.revoke))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		mux.ServeHTTP(w, r)
	})
}

type action func(http.ResponseWriter, *http.Request, *records) error

func (s *Server) atomic(fn action) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := httptest.NewRecorder()
		err := transaction(r.Context(), s.pool, func(db *records) error { return fn(out, r, db) })
		if err != nil {
			http.Error(w, "authentication storage unavailable", 503)
			return
		}
		maps.Copy(w.Header(), out.Header())
		w.WriteHeader(out.Code)
		_, _ = w.Write(out.Body.Bytes())
	}
}
func (s *Server) password(w http.ResponseWriter, r *http.Request, db *records) error {
	if s.cfg.Mode != "password" {
		http.NotFound(w, r)
		return nil
	}
	if !s.csrf(w, r) {
		return nil
	}
	if !s.rate(r.Context(), db, "password", 120) {
		http.Error(w, "try again later", 429)
		return nil
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "invalid login", 400)
		return nil
	}
	if !equal(in.Password, s.cfg.Password) || in.Username != "admin" {
		http.Error(w, "invalid username or password", 401)
		return nil
	}
	if err := s.issue(r.Context(), db, w, Identity{Principal: domain.Principal{Kind: "human", ID: "local:admin", DisplayName: "Admin"}}); err != nil {
		return err
	}
	jsonResponse(w, map[string]bool{"ok": true})
	return nil
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request, db *records) error {
	if !s.csrf(w, r) {
		return nil
	}
	if strings.HasSuffix(r.URL.Path, "-all") {
		i, err := s.browser(r.Context(), db, r)
		if err != nil {
			http.Error(w, "login required", 401)
			return nil
		}
		if err = db.put(r.Context(), "logout-all", i.Principal.ID, time.Now().UTC(), time.Now().Add(31*24*time.Hour)); err != nil {
			return err
		}
	}
	if err := db.del(r.Context(), "browser", digest(cookieValue(r, s.cookieName(browserCookie)))); err != nil {
		return err
	}
	s.cookie(w, browserCookie, "", -1)
	jsonResponse(w, map[string]bool{"ok": true})
	return nil
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Connect to Envy</title>
<style>
:root{font-family:system-ui,sans-serif;color:#0f172a;background:#f1f5f9;color-scheme:light dark}
body{min-height:100dvh;display:grid;place-items:center;margin:0;padding:24px;box-sizing:border-box}
main{box-sizing:border-box;width:100%;max-width:440px;background:#fff;border:1px solid #e2e8f0;border-radius:16px;padding:32px;overflow-wrap:anywhere}
.brand{font-size:14px;font-weight:700;color:#0f766e;letter-spacing:.04em}h1{font-size:24px;line-height:1.3;margin-top:18px}p{font-size:15px;line-height:1.6;color:#475569}
form{display:flex;gap:12px;margin-top:24px}button{font:inherit;border:1px solid #cbd5e1;border-radius:8px;padding:10px 16px;cursor:pointer;background:transparent;color:inherit}button[value=allow]{background:#0f766e;border-color:#0f766e;color:white}button:focus-visible{outline:3px solid #14b8a6;outline-offset:3px}
@media(prefers-color-scheme:dark){:root{background:#0f172a;color:#f8fafc}main{background:#1e293b;border-color:#334155}p{color:#cbd5e1}.brand{color:#5eead4}button{border-color:#64748b}}
</style></head><body><main><div class="brand">Envy</div><h1>Connect {{.Name}} to Envy?</h1><p>Signed in as {{.User}}.</p><p>This application will have full access to manage this Envy installation for up to 30 days.</p><form method="post" action="/oauth/authorize"><input type="hidden" name="pending" value="{{.Pending}}"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button name="decision" value="allow">Allow access</button><button name="decision" value="deny">Cancel</button></form></main></body></html>`))

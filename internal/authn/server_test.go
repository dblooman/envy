package authn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/persistence/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

func database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("ENVY_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("ENVY_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	boot, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}

	schema := fmt.Sprintf("auth_test_%d", time.Now().UnixNano())
	if _, err = boot.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}

	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}

	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		store.Close()
		_, _ = boot.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		boot.Close()
	})
	return store.AuthPool()
}

type browserTest struct {
	s       *Server
	cookies map[string]*http.Cookie
	csrf    string
}

func newBrowser(t *testing.T, s *Server) *browserTest {
	b := &browserTest{s: s, cookies: map[string]*http.Cookie{}}
	w := b.call("GET", "/auth/config", "", false)
	var v struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	b.csrf = v.CSRF
	return b
}

func (b *browserTest) call(method, path, body string, form bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://envy.test"+path, strings.NewReader(body))
	r.Header.Set("Origin", "https://envy.test")
	r.Header.Set("X-CSRF-Token", b.csrf)
	if form {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r.Header.Set("Content-Type", "application/json")
	}

	for _, c := range b.cookies {
		r.AddCookie(c)
	}

	w := httptest.NewRecorder()
	b.s.Handler().ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		b.cookies[c.Name] = c
	}

	return w
}

func (b *browserTest) login(t *testing.T) {
	t.Helper()
	w := b.call("POST", "/auth/password", `{"username":"admin","password":"admin"}`, false)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
}

func (b *browserTest) authenticate(token, resource string) error {
	r := httptest.NewRequest("GET", "https://envy.test"+resource, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	} else {
		for _, c := range b.cookies {
			r.AddCookie(c)
		}
	}

	_, err := b.s.Authenticate(r, resource)
	return err
}

func (b *browserTest) grant(t *testing.T, resource string) (string, url.Values) {
	t.Helper()
	w := b.call("POST", "/oauth/register", `{"client_name":"Test agent","redirect_uris":["http://127.0.0.1:5555/callback"],"token_endpoint_auth_method":"none"}`, false)
	var c map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &c)
	id, _ := c["client_id"].(string)
	if id == "" {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}

	verifier := oauth2.GenerateVerifier()
	q := url.Values{"client_id": {id}, "response_type": {"code"}, "redirect_uri": {"http://127.0.0.1:5555/callback"}, "scope": {"envy offline_access"}, "state": {Random()}, "resource": {"https://envy.test" + resource}, "code_challenge_method": {"S256"}, "code_challenge": {oauth2.S256ChallengeFromVerifier(verifier)}}
	w = b.call("GET", "/oauth/authorize?"+q.Encode(), "", false)
	matches := regexp.MustCompile(`name="pending" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(matches) != 2 {
		t.Fatalf("consent: %d %s %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}

	w = b.call("POST", "/oauth/authorize", url.Values{"pending": {matches[1]}, "decision": {"allow"}}.Encode(), true)
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil || u.Query().Get("code") == "" {
		t.Fatalf("authorize: %d %s %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}

	return id, url.Values{"client_id": {id}, "grant_type": {"authorization_code"}, "code": {u.Query().Get("code")}, "code_verifier": {verifier}, "redirect_uri": {"http://127.0.0.1:5555/callback"}, "resource": {"https://envy.test" + resource}}
}

func (b *browserTest) tokens(t *testing.T, q url.Values) map[string]any {
	t.Helper()
	w := b.call("POST", "/oauth/token", q.Encode(), true)
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}

	return v
}

func TestPasswordOAuthLifecycle(t *testing.T) {
	pool := database(t)
	s, err := New(context.Background(), Config{Mode: "password", Origin: "https://envy.test"}, pool)
	if err != nil {
		t.Fatal(err)
	}

	b := newBrowser(t, s)
	b.csrf = "wrong"
	if w := b.call("POST", "/auth/password", `{"username":"admin","password":"admin"}`, false); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}

	b = newBrowser(t, s)
	b.login(t)
	if err = b.authenticate("", "/v1"); err != nil {
		t.Fatal(err)
	}

	if err = b.authenticate("", "/mcp"); err == nil {
		t.Fatal("cookie accepted at MCP")
	}

	id, q := b.grant(t, "/mcp")
	v := b.tokens(t, q)
	access := v["access_token"].(string)
	refresh := v["refresh_token"].(string)
	if err = b.authenticate(access, "/mcp"); err != nil {
		t.Fatal(err)
	}

	if b.authenticate(access, "/v1") == nil {
		t.Fatal("MCP token accepted by REST")
	}

	s2, err := New(context.Background(), s.cfg, pool)
	if err != nil {
		t.Fatal(err)
	}

	b.s = s2
	if err = b.authenticate(access, "/mcp"); err != nil {
		t.Fatalf("restart: %v", err)
	}

	rq := url.Values{"client_id": {id}, "grant_type": {"refresh_token"}, "refresh_token": {refresh}, "resource": {"https://envy.test/mcp"}}
	rotated := b.tokens(t, rq)
	if w := b.call("POST", "/oauth/token", rq.Encode(), true); w.Code == 200 {
		t.Fatal("refresh replay accepted")
	}

	if b.authenticate(rotated["access_token"].(string), "/mcp") == nil {
		t.Fatal("refresh replay did not revoke grant")
	}

	_, q = b.grant(t, "/v1")
	v = b.tokens(t, q)
	if w := b.call("POST", "/oauth/token", q.Encode(), true); w.Code == 200 {
		t.Fatal("authorization code replay accepted")
	}

	if b.authenticate(v["access_token"].(string), "/v1") == nil {
		t.Fatal("code replay did not revoke grant")
	}

	_, q = b.grant(t, "/v1")
	v = b.tokens(t, q)
	if w := b.call("POST", "/auth/logout", `{}`, false); w.Code != 200 {
		t.Fatal(w.Body.String())
	}

	if b.authenticate("", "/v1") == nil {
		t.Fatal("logout retained browser")
	}

	if err = b.authenticate(v["access_token"].(string), "/v1"); err != nil {
		t.Fatal("browser logout revoked independent agent")
	}

	b.login(t)
	if w := b.call("POST", "/auth/logout-all", `{}`, false); w.Code != 200 {
		t.Fatal(w.Body.String())
	}

	if b.authenticate(v["access_token"].(string), "/v1") == nil {
		t.Fatal("logout-all retained agent")
	}
}

func TestOAuthPKCEExpiryAndRevocation(t *testing.T) {
	pool := database(t)
	s, _ := New(context.Background(), Config{Mode: "password", Origin: "https://envy.test"}, pool)
	b := newBrowser(t, s)
	b.login(t)
	_, q := b.grant(t, "/v1")
	q.Set("code_verifier", oauth2.GenerateVerifier())
	if w := b.call("POST", "/oauth/token", q.Encode(), true); w.Code == 200 {
		t.Fatal("wrong verifier accepted")
	}

	id, q := b.grant(t, "/v1")
	v := b.tokens(t, q)
	if w := b.call("POST", "/oauth/revoke", url.Values{"client_id": {id}, "token": {v["refresh_token"].(string)}, "token_type_hint": {"refresh_token"}}.Encode(), true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}

	if b.authenticate(v["access_token"].(string), "/v1") == nil {
		t.Fatal("revoke retained access token")
	}

	_, q = b.grant(t, "/v1")
	v = b.tokens(t, q)
	s.cfg.Password = "changed"
	if b.authenticate(v["access_token"].(string), "/v1") == nil || b.authenticate("", "/v1") == nil {
		t.Fatal("password change retained credentials")
	}

	s.cfg.Password = "admin"
	s.cfg.Mode = "google"
	if b.authenticate(v["access_token"].(string), "/v1") == nil {
		t.Fatal("mode change retained credentials")
	}
}

func TestConcurrentCodeExchange(t *testing.T) {
	s, _ := New(context.Background(), Config{Mode: "password", Origin: "https://envy.test"}, database(t))
	b := newBrowser(t, s)
	b.login(t)
	_, q := b.grant(t, "/v1")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() {
			r := httptest.NewRequest("POST", "https://envy.test/oauth/token", strings.NewReader(q.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			codes <- w.Code
		})
	}

	wg.Wait()
	close(codes)
	success := 0
	for c := range codes {
		if c == 200 {
			success++
		}
	}

	if success != 1 {
		t.Fatalf("%d successful exchanges", success)
	}
}

func TestGoogleAdmissionAndConfiguration(t *testing.T) {
	s := &Server{cfg: Config{GoogleDomains: []string{"example.com"}, GoogleEmails: []string{"allowed@gmail.com"}}}
	for _, tc := range []struct {
		email, hd      string
		verified, want bool
	}{{"user@example.com", "", true, false}, {"user@example.com", "example.com", true, true}, {"user@example.com", "example.com", false, false}, {"allowed@gmail.com", "", true, true}, {"other@gmail.com", "", true, false}} {
		if got := s.admitted(tc.email, tc.hd, tc.verified); got != tc.want {
			t.Fatalf("admission %v: %v", tc, got)
		}
	}

	for _, origin := range []string{"https://envy.test", "http://localhost:5173", "http://127.0.0.1:8081"} {
		if err := Validate(Config{Mode: "password", Origin: origin}); err != nil {
			t.Fatal(err)
		}
	}

	for _, origin := range []string{"", "http://envy.test", "https://user:pass@envy.test", "https://envy.test/path"} {
		if Validate(Config{Mode: "password", Origin: origin}) == nil {
			t.Fatal("invalid origin accepted", origin)
		}
	}
}

func TestExpiryAndCookies(t *testing.T) {
	pool := database(t)
	s, _ := New(context.Background(), Config{Mode: "password", Origin: "https://envy.test"}, pool)
	b := newBrowser(t, s)
	b.login(t)
	cookie := b.cookies[s.cookieName(browserCookie)]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 7*24*3600 {
		t.Fatalf("cookie attributes: %+v", cookie)
	}

	id, q := b.grant(t, "/v1")
	v := b.tokens(t, q)
	// Age only the access token; a valid refresh grant should still work.
	_, err := pool.Exec(context.Background(), `UPDATE envy_auth_records SET body=jsonb_set(body,'{request,session,expires_at,access_token}',to_jsonb((now()-interval '1 hour')::text)) WHERE kind='access'`)
	if err != nil {
		t.Fatal(err)
	}

	if b.authenticate(v["access_token"].(string), "/v1") == nil {
		t.Fatal("expired access token accepted")
	}

	rq := url.Values{"client_id": {id}, "grant_type": {"refresh_token"}, "refresh_token": {v["refresh_token"].(string)}, "resource": {"https://envy.test/v1"}}
	b.tokens(t, rq)
	_, err = pool.Exec(context.Background(), `UPDATE envy_auth_records SET expires_at=now()-interval '1 second' WHERE kind='browser'`)
	if err != nil {
		t.Fatal(err)
	}

	if b.authenticate("", "/v1") == nil {
		t.Fatal("expired browser session accepted")
	}

	session := &oauthSession{Until: time.Now().Add(time.Hour)}
	session.SetExpiresAt("refresh_token", time.Now().Add(30*24*time.Hour))
	if !session.GetExpiresAt("refresh_token").Equal(session.Until) {
		t.Fatal("refresh extended beyond absolute expiry")
	}
}

func TestModeChangesPermanentlyInvalidateSessions(t *testing.T) {
	pool := database(t)
	cfg := Config{Mode: "password", Origin: "https://envy.test"}
	s, _ := New(context.Background(), cfg, pool)
	b := newBrowser(t, s)
	b.login(t)
	_, q := b.grant(t, "/v1")
	tokens := b.tokens(t, q)
	if _, err := New(context.Background(), Config{Mode: "dev"}, pool); err != nil {
		t.Fatal(err)
	}

	if b.authenticate("", "/v1") == nil {
		t.Fatal("old replica accepted stale authentication config")
	}

	restarted, err := New(context.Background(), cfg, pool)
	if err != nil {
		t.Fatal(err)
	}

	b.s = restarted
	if b.authenticate("", "/v1") == nil || b.authenticate(tokens["access_token"].(string), "/v1") == nil {
		t.Fatal("restoring a mode resurrected old credentials")
	}
}

func TestRedirectAndConsentIsolation(t *testing.T) {
	s, _ := New(context.Background(), Config{Mode: "password", Origin: "https://envy.test"}, database(t))
	b := newBrowser(t, s)
	b.login(t)
	for _, uri := range []string{"javascript:alert(1)", "http://outside.test/callback", "https://client.test/callback#fragment"} {
		body, _ := json.Marshal(map[string]any{"client_name": "Bad redirect", "redirect_uris": []string{uri}})
		if w := b.call("POST", "/oauth/register", string(body), false); w.Code != 400 {
			t.Fatal("unsafe redirect registered", uri, w.Code)
		}
	}

	w := b.call("POST", "/oauth/register", `{"client_name":"Agent","redirect_uris":["https://client.test/callback"]}`, false)
	var registered struct {
		ID string `json:"client_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &registered)
	query := url.Values{"client_id": {registered.ID}, "redirect_uri": {"https://evil.test/callback"}, "response_type": {"code"}, "scope": {"envy"}, "state": {Random()}, "resource": {"https://envy.test/mcp"}, "code_challenge_method": {"S256"}, "code_challenge": {oauth2.S256ChallengeFromVerifier(oauth2.GenerateVerifier())}}
	w = b.call("GET", "/oauth/authorize?"+query.Encode(), "", false)
	if strings.HasPrefix(w.Header().Get("Location"), "https://evil.test") {
		t.Fatal("unregistered redirect followed")
	}

	if strings.Contains(w.Body.String(), `name="pending"`) {
		t.Fatal("unregistered redirect reached consent")
	}

	query.Set("redirect_uri", "https://client.test/callback")
	w = b.call("GET", "/oauth/authorize?"+query.Encode(), "", false)
	pending := regexp.MustCompile(`name="pending" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(pending) != 2 {
		t.Fatal("consent missing")
	}

	other := newBrowser(t, s)
	other.login(t)
	w = other.call("POST", "/oauth/authorize", url.Values{"pending": {pending[1]}, "decision": {"allow"}}.Encode(), true)
	if w.Code != 400 {
		t.Fatal("another browser approved consent", w.Code)
	}

	r := httptest.NewRequest("POST", "https://envy.test/auth/logout", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("X-CSRF-Token", b.csrf)
	for _, c := range b.cookies {
		r.AddCookie(c)
	}

	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin logout accepted")
	}
}

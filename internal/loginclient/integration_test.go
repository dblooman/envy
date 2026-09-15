package loginclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/api"
	"github.com/dblooman/envy/internal/authn"
	"github.com/dblooman/envy/internal/persistence/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

func TestLoginThroughEnvyServer(t *testing.T) {
	raw := os.Getenv("ENVY_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("ENVY_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	boot, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer boot.Close()
	schema := fmt.Sprintf("login_test_%d", time.Now().UnixNano())
	if _, err = boot.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer boot.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	auth, err := authn.New(ctx, authn.Config{Mode: "password", Origin: base}, store.AuthPool())
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = api.NewAuthenticationHandler(api.AuthConfig{Mode: "password", ExternalOrigin: base, Login: auth})
	server.Start()
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := browser.Get(base + "/auth/config")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.NewDecoder(response.Body).Decode(&cfg)
	response.Body.Close()
	req, _ := http.NewRequest("POST", base+"/auth/password", strings.NewReader(`{"username":"admin","password":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	req.Header.Set("X-CSRF-Token", cfg.CSRF)
	response, err = browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("password login failed", response.StatusCode)
	}
	// A supplied bad credential must never fall back to the browser session.
	req, _ = http.NewRequest("GET", base+"/v1/session", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	response, err = browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("invalid bearer fell back to browser")
	}
	approve := func(target string) string {
		t.Helper()
		response, err := browser.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		html, _ := io.ReadAll(response.Body)
		response.Body.Close()
		pending := regexp.MustCompile(`name="pending" value="([^"]+)"`).FindStringSubmatch(string(html))
		if len(pending) != 2 {
			t.Fatalf("missing consent form: %s", html)
		}
		form := url.Values{"pending": {pending[1]}, "decision": {"allow"}, "csrf_token": {cfg.CSRF}}
		req, _ := http.NewRequest("POST", base+"/oauth/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", base)
		response, err = browser.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 303 && response.StatusCode != 302 {
			t.Fatal("consent failed", response.StatusCode)
		}
		return response.Header.Get("Location")
	}
	manager, _ := New(base)
	manager.Directory = t.TempDir()
	manager.OpenBrowser = func(target string) error {
		response, err := http.Get(approve(target))
		if err == nil {
			response.Body.Close()
		}
		return err
	}
	if err = manager.Login(ctx, io.Discard); err != nil {
		t.Fatal(err)
	}
	response, err = manager.HTTPClient().Get(base + "/v1/session")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(data), "local:admin") {
		t.Fatalf("CLI identity: %d %s", response.StatusCode, data)
	}
	response, err = http.Get(base + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 || !strings.Contains(response.Header.Get("WWW-Authenticate"), "oauth-protected-resource/mcp") {
		t.Fatal("MCP discovery challenge missing")
	}
	var registered struct {
		ID string `json:"client_id"`
	}
	if err = manager.call(ctx, "/oauth/register", nil, map[string]any{"client_name": "Remote MCP", "redirect_uris": []string{"http://127.0.0.1:9999/callback"}}, &registered); err != nil {
		t.Fatal(err)
	}
	verifier := oauth2.GenerateVerifier()
	query := url.Values{"client_id": {registered.ID}, "response_type": {"code"}, "redirect_uri": {"http://127.0.0.1:9999/callback"}, "scope": {"envy"}, "state": {oauth2.GenerateVerifier()}, "code_challenge_method": {"S256"}, "code_challenge": {oauth2.S256ChallengeFromVerifier(verifier)}, "resource": {base + "/mcp"}}
	callback, _ := url.Parse(approve(base + "/oauth/authorize?" + query.Encode()))
	code := callback.Query().Get("code")
	if code == "" {
		t.Fatal("MCP code missing")
	}
	var token struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	err = manager.call(ctx, "/oauth/token", url.Values{"client_id": {registered.ID}, "grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {"http://127.0.0.1:9999/callback"}, "resource": {base + "/mcp"}}, nil, &token)
	if err != nil || token.Refresh == "" {
		t.Fatal("MCP token or automatic refresh missing", err)
	}
	remoteHTTP := &http.Client{Transport: integrationBearer(token.Access)}
	mcpClient := sdk.NewClient(&sdk.Implementation{Name: "integration", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: base + "/mcp", HTTPClient: remoteHTTP}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) == 0 {
		t.Fatal("authenticated MCP failed", err)
	}
	response, err = remoteHTTP.Get(base + "/v1/session")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("MCP token crossed resource boundary")
	}
	if err = manager.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Token(ctx); err != ErrLoginRequired {
		t.Fatal("CLI logout retained login", err)
	}
}

type integrationBearer string

func (t integrationBearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(t))
	return http.DefaultTransport.RoundTrip(r)
}

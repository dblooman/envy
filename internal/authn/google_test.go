package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestGoogleBrowserFlow(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "google-test"))
	if err != nil {
		t.Fatal(err)
	}
	var issuer, nonce, audience string
	verified := true
	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			jsonResponse(w, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			jsonResponse(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "google-test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			raw, e := jwt.Signed(signer).Claims(map[string]any{"iss": issuer, "sub": "stable-id", "aud": audience, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce, "email": "user@example.com", "email_verified": verified, "hd": "example.com", "name": "Example User"}).Serialize()
			if e != nil {
				t.Error(e)
			}
			jsonResponse(w, map[string]any{"id_token": raw, "access_token": "google-only-token", "token_type": "Bearer", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer google.Close()
	issuer = google.URL
	audience = "client"
	cfg := Config{Mode: "google", Origin: "https://envy.test", GoogleClientID: "client", GoogleClientSecret: "secret", GoogleDomains: []string{"example.com"}, GoogleIssuer: issuer}
	s, err := New(context.Background(), cfg, database(t))
	if err != nil {
		t.Fatal(err)
	}
	start := func(b *browserTest) string {
		t.Helper()
		w := b.call("GET", "/auth/google/start?return_to=%2Fcompositions%2Fabc", "", false)
		u, e := url.Parse(w.Header().Get("Location"))
		if e != nil || u.Query().Get("state") == "" {
			t.Fatal("Google redirect missing", w.Body.String())
		}
		nonce = u.Query().Get("nonce")
		if u.Query().Get("code_challenge_method") != "S256" {
			t.Fatal("missing Google PKCE")
		}
		return u.Query().Get("state")
	}
	b := newBrowser(t, s)
	state := start(b)
	w := b.call("GET", "/auth/google/callback?state="+state+"&code=google-code", "", false)
	if w.Header().Get("Location") != "/compositions/abc" {
		t.Fatalf("Google callback: %d %s %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	if err = b.authenticate("", "/v1"); err != nil {
		t.Fatal(err)
	}
	// The stable subject is independent of the display name/email.
	var identity Identity
	err = transaction(context.Background(), s.pool, func(db *records) error {
		return db.get(context.Background(), "browser", digest(b.cookies[s.cookieName(browserCookie)].Value), &identity)
	})
	if err != nil || identity.Principal.ID != "google:"+digest(issuer+"\x00stable-id") {
		t.Fatal("unstable Google identity", err)
	}
	_, q := b.grant(t, "/mcp")
	v := b.tokens(t, q)
	if v["access_token"] == "google-only-token" {
		t.Fatal("Google token passed through")
	}
	s.cfg.GoogleDomains = []string{"other.test"}
	if b.authenticate(v["access_token"].(string), "/mcp") == nil {
		t.Fatal("allowlist change retained access")
	}
	s.cfg.GoogleDomains = cfg.GoogleDomains
	replay := b.call("GET", "/auth/google/callback?state="+state+"&code=google-code", "", false)
	if replay.Header().Get("Location") != "/login?error=access_denied" {
		t.Fatal("Google state replay accepted")
	}
	for _, failure := range []string{"nonce", "audience", "verified", "browser"} {
		t.Run(failure, func(t *testing.T) {
			b := newBrowser(t, s)
			state := start(b)
			audience = "client"
			verified = true
			switch failure {
			case "nonce":
				nonce = "incorrect"
			case "audience":
				audience = "other"
			case "verified":
				verified = false
			case "browser":
				delete(b.cookies, s.cookieName("envy_google"))
			}
			w := b.call("GET", "/auth/google/callback?state="+state+"&code=google-code", "", false)
			if w.Header().Get("Location") != "/login?error=access_denied" {
				t.Fatal("invalid Google identity accepted", w.Body.String())
			}
			if b.authenticate("", "/v1") == nil {
				t.Fatal("invalid login created session")
			}
		})
	}
}

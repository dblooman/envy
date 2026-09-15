package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestAuthenticationModesAndSession(t *testing.T) {
	s := &fakeService{composition: domain.Composition{ID: "abc"}}
	none := NewConfiguredHandler(s, AuthConfig{Mode: "none"}, Installation{ID: "test"}, nil, nil)
	w := request(none, "GET", "/v1/session", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"anonymous"`) {
		t.Fatalf("none session: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "/v1/session", nil)
	r.Header.Set("Authorization", "Bearer invalid")
	w = httptest.NewRecorder()
	none.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("invalid bearer fell through to anonymous: %d", w.Code)
	}

	machine := NewConfiguredHandler(s, AuthConfig{Mode: "token", MachineCredentials: []MachineCredential{{Token: strings.Repeat("m", 32), ID: "agent-release", DisplayName: "Release agent"}}}, Installation{}, nil, nil)
	w = request(machine, "GET", "/v1/session", "", strings.Repeat("m", 32))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"agent-release"`) {
		t.Fatalf("machine session: %d %s", w.Code, w.Body.String())
	}

	proxy := NewConfiguredHandler(s, AuthConfig{Mode: "proxy", ProxySecret: strings.Repeat("p", 32), TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}, Installation{}, nil, nil)
	r = httptest.NewRequest("GET", "/v1/session", nil)
	r.Header.Set("X-Envy-Proxy-Secret", strings.Repeat("p", 32))
	r.Header.Set("X-Envy-User", "alice")
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"human"`) {
		t.Fatalf("proxy session: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/v1/session", nil)
	r.Header.Set("X-Envy-User", "alice")
	r.Header.Add("X-Envy-User", "mallory")
	r.Header.Set("X-Envy-Proxy-Secret", strings.Repeat("p", 32))
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("duplicate proxy identity accepted: %d", w.Code)
	}
	for name, value := range map[string]string{"X-Envy-User": "alice,mallory", "X-Envy-Proxy-Secret": strings.Repeat("p", 16) + "," + strings.Repeat("p", 16)} {
		r = httptest.NewRequest("GET", "/v1/session", nil)
		r.Header.Set("X-Envy-Proxy-Secret", strings.Repeat("p", 32))
		r.Header.Set("X-Envy-User", "alice")
		r.Header.Set(name, value)
		w = httptest.NewRecorder()
		proxy.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("joined %s accepted: %d", name, w.Code)
		}
	}
}

func TestAuthenticationOnlyHandlerProtectsAPIFallback(t *testing.T) {
	h := NewAuthenticationHandler(AuthConfig{Mode: "token", SharedToken: "secret"})
	if response := request(h, "GET", "/v1/session", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(h, "GET", "/v1/session", "", "secret"); response.Code != http.StatusNotFound {
		t.Fatalf("authenticated fallback status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProxyCookieMutationRequiresSameOrigin(t *testing.T) {
	s := &fakeService{composition: domain.Composition{ID: "abc"}}
	h := NewConfiguredHandler(s, AuthConfig{Mode: "proxy", ProxySecret: strings.Repeat("p", 32), TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}, Installation{}, nil, nil)
	r := httptest.NewRequest("DELETE", "http://envy.test/v1/compositions/abc", nil)
	r.Header.Set("X-Envy-Proxy-Secret", strings.Repeat("p", 32))
	r.Header.Set("X-Envy-User", "alice")
	r.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
	r.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("cross-origin mutation accepted: %d", w.Code)
	}
	r = httptest.NewRequest("DELETE", "http://envy.test/v1/compositions/abc", nil)
	r.Header.Set("X-Envy-Proxy-Secret", strings.Repeat("p", 32))
	r.Header.Set("X-Envy-User", "alice")
	r.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
	r.Header.Set("Origin", "http://envy.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("same-origin mutation rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestProxyCookieMutationUsesConfiguredExternalOrigin(t *testing.T) {
	s := &fakeService{composition: domain.Composition{ID: "abc"}}
	h := NewConfiguredHandler(s, AuthConfig{Mode: "proxy", ExternalOrigin: "https://envy.example.test", ProxySecret: strings.Repeat("p", 32), TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}, Installation{}, nil, nil)
	r := httptest.NewRequest("DELETE", "http://envy-envy.envy-system.svc/v1/compositions/abc", nil)
	r.Header.Set("X-Envy-Proxy-Secret", strings.Repeat("p", 32))
	r.Header.Set("X-Envy-User", "alice")
	r.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
	r.Header.Set("Origin", "https://envy.example.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("configured external origin rejected: %d %s", w.Code, w.Body.String())
	}
	r.Header.Set("Origin", "http://envy.example.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("wrong external scheme accepted: %d", w.Code)
	}
}

func TestMalformedAuthorizationNeverFallsThrough(t *testing.T) {
	for _, mode := range []string{"none", "proxy"} {
		for _, values := range [][]string{{""}, {"Basic abc"}, {"Bearer "}, {"Bearer a", "Bearer b"}, {"Bearer a,Bearer b"}} {
			h := NewConfiguredHandler(&fakeService{}, AuthConfig{Mode: mode, ProxySecret: strings.Repeat("p", 32), TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}, Installation{}, nil, nil)
			r := httptest.NewRequest("GET", "/v1/session", nil)
			r.Header["Authorization"] = values
			r.Header.Set("X-Envy-User", "alice")
			r.Header.Set("X-Envy-Proxy-Secret", strings.Repeat("p", 32))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("%s accepted %q: %d", mode, values, w.Code)
			}
		}
	}
}

func TestProxyMutationRequiresOriginWithoutCookies(t *testing.T) {
	h := NewConfiguredHandler(&fakeService{composition: domain.Composition{ID: "abc"}}, AuthConfig{Mode: "proxy", ExternalOrigin: "https://envy.test", ProxySecret: strings.Repeat("p", 32), TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}, Installation{}, nil, nil)
	for _, tc := range []struct {
		origin string
		code   int
	}{{"", 401}, {"https://evil.test", 401}, {"http://envy.test", 401}, {"https://envy.test/path", 401}, {"https://envy.test", 202}} {
		r := httptest.NewRequest("DELETE", "http://internal/v1/compositions/abc", nil)
		r.Header.Set("X-Envy-User", "alice")
		r.Header.Set("X-Envy-Proxy-Secret", strings.Repeat("p", 32))
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("origin %q: got %d want %d", tc.origin, w.Code, tc.code)
		}
	}
}

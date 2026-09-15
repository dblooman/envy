package loginclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshAcrossProcesses(t *testing.T) {
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/config" {
			io.WriteString(w, `{"mode":"password"}`)
			return
		}
		if r.URL.Path == "/oauth/token" {
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "old-refresh" {
				t.Error("replayed rotated token")
			}
			refreshes.Add(1)
			time.Sleep(100 * time.Millisecond)
			io.WriteString(w, `{"access_token":"fresh-access","refresh_token":"fresh-refresh","expires_in":900}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	m, _ := New(server.URL)
	m.Directory = t.TempDir()
	err := m.locked(context.Background(), func() error {
		return m.save(Credentials{ClientID: "client", Resource: server.URL + "/v1", AccessToken: "expired", RefreshToken: "old-refresh", Expires: time.Now().Add(-time.Hour)})
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCredentialProcessHelper$")
			cmd.Env = append(os.Environ(), "ENVY_AUTH_HELPER=1", "ENVY_HELPER_URL="+server.URL, "ENVY_HELPER_DIR="+m.Directory)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("helper: %v %s", err, out)
			}
		})
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("%d refreshes", refreshes.Load())
	}
	info, err := os.Stat(m.path())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential permissions", err)
	}
}
func TestCredentialProcessHelper(t *testing.T) {
	if os.Getenv("ENVY_AUTH_HELPER") != "1" {
		return
	}
	m, err := New(os.Getenv("ENVY_HELPER_URL"))
	if err != nil {
		t.Fatal(err)
	}
	m.Directory = os.Getenv("ENVY_HELPER_DIR")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token, err := m.Token(ctx)
	if err != nil || token != "fresh-access" {
		t.Fatalf("token: %v", err)
	}
}
func TestBrowserLoginAndLogout(t *testing.T) {
	var callback, challenge string
	revoked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/config":
			io.WriteString(w, `{"mode":"password"}`)
		case "/oauth/register":
			var v struct {
				Redirects []string `json:"redirect_uris"`
			}
			_ = json.NewDecoder(r.Body).Decode(&v)
			callback = v.Redirects[0]
			io.WriteString(w, `{"client_id":"client"}`)
		case "/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "single-use" || r.Form.Get("code_verifier") == "" || challenge == "" {
				t.Error("missing code or PKCE")
			}
			io.WriteString(w, `{"access_token":"access","refresh_token":"refresh","expires_in":900}`)
		case "/oauth/revoke":
			_ = r.ParseForm()
			revoked = r.Form.Get("token") == "refresh"
			io.WriteString(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m, _ := New(server.URL)
	m.Directory = t.TempDir()
	m.OpenBrowser = func(target string) error {
		u, _ := url.Parse(target)
		q := u.Query()
		challenge = q.Get("code_challenge")
		response, err := http.Get(callback + "?state=" + url.QueryEscape(q.Get("state")) + "&code=single-use")
		if err == nil {
			response.Body.Close()
		}
		return err
	}
	if err := m.Login(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	c, err := m.read()
	if err != nil || c.AccessToken != "access" {
		t.Fatal("credentials not saved", err)
	}
	if err = m.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("logout did not revoke")
	}
	if _, err = os.Stat(m.path()); !os.IsNotExist(err) {
		t.Fatal("credentials not removed")
	}
}
func TestDevAndCredentialOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/config" {
			io.WriteString(w, `{"mode":"dev"}`)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("dev request sent token")
		}
		io.WriteString(w, `{}`)
	}))
	defer server.Close()
	m, _ := New(server.URL)
	m.Directory = t.TempDir()
	if err := m.locked(context.Background(), func() error {
		return m.save(Credentials{ClientID: "old", Resource: server.URL + "/v1", RefreshToken: "old", AccessToken: "old", Expires: time.Now().Add(time.Hour)})
	}); err != nil {
		t.Fatal(err)
	}
	response, err := m.HTTPClient().Get(server.URL + "/v1/session")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	_, err = m.HTTPClient().Get("https://different.example/v1/session")
	if err == nil || !strings.Contains(err.Error(), "another origin") {
		t.Fatal("cross-origin credentials allowed")
	}
}

package loginclient

import (
	"context"
	"encoding/json"
	"errors"
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

type failingLoginWriter struct{ err error }

func (w failingLoginWriter) Write([]byte) (int, error) { return 0, w.err }

func TestBrowserLoginOutputFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"mode":"password"}`
		if r.URL.Path == "/oauth/register" {
			body = `{"client_id":"client"}`
		}

		if _, err := io.WriteString(w, body); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	m, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	opened := false
	m.OpenBrowser = func(context.Context, string) error {
		opened = true
		return nil
	}
	want := errors.New("output unavailable")
	if err := m.Login(t.Context(), failingLoginWriter{want}); !errors.Is(err, want) {
		t.Fatalf("expected output error, got %v", err)
	}

	if opened {
		t.Fatal("browser opened after failing to display login instructions")
	}
}

func TestOpenBrowserHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := openBrowser(ctx, "https://envy.test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestRefreshAcrossProcesses(t *testing.T) {
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/config" {
			if _, err := io.WriteString(w, `{"mode":"password"}`); err != nil {
				t.Error(err)
			}

			return
		}

		if r.URL.Path == "/oauth/token" {
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "old-refresh" {
				t.Error("replayed rotated token")
			}

			refreshes.Add(1)
			time.Sleep(100 * time.Millisecond)
			if _, err := io.WriteString(w, `{"access_token":"fresh-access","refresh_token":"fresh-refresh","expires_in":900}`); err != nil {
				t.Error(err)
			}

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
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCredentialProcessHelper$")
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
	if err != nil || info.Mode().Perm() != 0o600 {
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
			if _, err := io.WriteString(w, `{"mode":"password"}`); err != nil {
				t.Error(err)
			}
		case "/oauth/register":
			var v struct {
				Redirects []string `json:"redirect_uris"`
			}
			_ = json.NewDecoder(r.Body).Decode(&v)
			callback = v.Redirects[0]
			if _, err := io.WriteString(w, `{"client_id":"client"}`); err != nil {
				t.Error(err)
			}
		case "/oauth/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "single-use" || r.Form.Get("code_verifier") == "" || challenge == "" {
				t.Error("missing code or PKCE")
			}

			if _, err := io.WriteString(w, `{"access_token":"access","refresh_token":"refresh","expires_in":900}`); err != nil {
				t.Error(err)
			}
		case "/oauth/revoke":
			_ = r.ParseForm()
			revoked = r.Form.Get("token") == "refresh"
			if _, err := io.WriteString(w, `{}`); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m, _ := New(server.URL)
	m.Directory = t.TempDir()
	m.OpenBrowser = func(ctx context.Context, target string) error {
		u, _ := url.Parse(target)
		q := u.Query()
		challenge = q.Get("code_challenge")
		response, err := testHTTPGet(ctx, nil, callback+"?state="+url.QueryEscape(q.Get("state"))+"&code=single-use")
		if err == nil {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
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
			if _, err := io.WriteString(w, `{"mode":"dev"}`); err != nil {
				t.Error(err)
			}

			return
		}

		if r.Header.Get("Authorization") != "" {
			t.Error("dev request sent token")
		}

		if _, err := io.WriteString(w, `{}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	m, _ := New(server.URL)
	m.Directory = t.TempDir()
	if err := m.locked(context.Background(), func() error {
		return m.save(Credentials{ClientID: "old", Resource: server.URL + "/v1", RefreshToken: "old", AccessToken: "old", Expires: time.Now().Add(time.Hour)})
	}); err != nil {
		t.Fatal(err)
	}

	response, err := testHTTPGet(t.Context(), m.HTTPClient(), server.URL+"/v1/session")
	if err != nil {
		t.Fatal(err)
	}

	if err := response.Body.Close(); err != nil {
		t.Error(err)
	}

	response, err = testHTTPGet(t.Context(), m.HTTPClient(), "https://different.example/v1/session")
	if response != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}

	if err == nil || !strings.Contains(err.Error(), "another origin") {
		t.Fatal("cross-origin credentials allowed")
	}
}

func testHTTPGet(ctx context.Context, client *http.Client, target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}

	if client == nil {
		client = http.DefaultClient
	}

	return client.Do(req)
}

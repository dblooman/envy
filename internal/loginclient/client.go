// Package loginclient shares browser-issued credentials between CLI and stdio MCP.
package loginclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"golang.org/x/oauth2"
)

var ErrLoginRequired = errors.New("login required; run envy auth login --api-url <installation URL>")

type Credentials struct {
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Resource     string    `json:"resource"`
	Expires      time.Time `json:"expires"`
}
type Manager struct {
	Base        string
	Directory   string
	HTTP        *http.Client
	OpenBrowser func(string) error
}

func New(base string) (*Manager, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()))) {
		return nil, errors.New("login requires an HTTPS installation origin or HTTP loopback URL")
	}

	u.Host = strings.ToLower(u.Host)
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	return &Manager{Base: u.String(), Directory: filepath.Join(dir, "envy", "credentials"), HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, OpenBrowser: openBrowser}, nil
}

func (m *Manager) path() string {
	sum := sha256.Sum256([]byte(m.Base))
	return filepath.Join(m.Directory, hex.EncodeToString(sum[:])+".json")
}

func (m *Manager) locked(ctx context.Context, fn func() error) error {
	if err := os.MkdirAll(m.Directory, 0o700); err != nil {
		return err
	}

	if err := os.Chmod(m.Directory, 0o700); err != nil {
		return err
	}

	lock := flock.New(m.path() + ".lock")
	ok, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return err
	}

	if !ok {
		return ctx.Err()
	}

	defer lock.Unlock()
	return fn()
}

func (m *Manager) read() (Credentials, error) {
	var c Credentials
	info, err := os.Lstat(m.path())
	if errors.Is(err, os.ErrNotExist) {
		return c, ErrLoginRequired
	}

	if err != nil {
		return c, err
	}

	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return c, errors.New("credential file must be a regular file accessible only to its owner")
	}

	b, err := os.ReadFile(m.path())
	if err != nil {
		return c, err
	}

	if json.Unmarshal(b, &c) != nil || c.Resource != m.Base+"/v1" || c.ClientID == "" || c.RefreshToken == "" {
		return c, ErrLoginRequired
	}

	return c, nil
}

func (m *Manager) save(c Credentials) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(m.Directory, ".credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}

	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}

	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}

	if err = f.Close(); err != nil {
		return err
	}

	return os.Rename(f.Name(), m.path())
}

func (m *Manager) call(ctx context.Context, path string, form url.Values, body any, out any) error {
	var reader io.Reader
	method := "GET"
	content := ""
	if form != nil {
		reader = strings.NewReader(form.Encode())
		content = "application/x-www-form-urlencoded"
		method = "POST"
	} else if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}

		reader = bytes.NewReader(b)
		content = "application/json"
		method = "POST"
	}

	req, e := http.NewRequestWithContext(ctx, method, m.Base+path, reader)
	if e != nil {
		return e
	}

	if content != "" {
		req.Header.Set("Content-Type", content)
	}

	response, e := m.HTTP.Do(req)
	if e != nil {
		return errors.New("cannot reach authentication server")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if path == "/oauth/token" && response.StatusCode == 400 {
			return ErrLoginRequired
		}

		return fmt.Errorf("authentication request failed (HTTP %d)", response.StatusCode)
	}

	if out != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(out)
	}

	return nil
}

func (m *Manager) mode(ctx context.Context) (string, error) {
	var c struct {
		Mode string `json:"mode"`
	}
	err := m.call(ctx, "/auth/config", nil, nil, &c)
	return c.Mode, err
}

func (m *Manager) exchange(ctx context.Context, c Credentials, values url.Values) (Credentials, error) {
	values.Set("client_id", c.ClientID)
	values.Set("resource", c.Resource)
	var token struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires int64  `json:"expires_in"`
	}
	if err := m.call(ctx, "/oauth/token", values, nil, &token); err != nil {
		return c, err
	}

	if token.Access == "" || token.Refresh == "" || token.Expires <= 0 {
		return c, errors.New("invalid token response")
	}

	c.AccessToken = token.Access
	c.RefreshToken = token.Refresh
	c.Expires = time.Now().Add(time.Duration(token.Expires) * time.Second)
	return c, nil
}

func (m *Manager) Token(ctx context.Context) (string, error) {
	var token string
	err := m.locked(ctx, func() error {
		mode, err := m.mode(ctx)
		if err != nil {
			return err
		}

		if mode == "dev" || mode == "none" {
			return nil
		}

		c, err := m.read()
		if errors.Is(err, ErrLoginRequired) {
			mode, e := m.mode(ctx)
			if e != nil {
				return e
			}

			if mode == "dev" || mode == "none" {
				return nil
			}

			return ErrLoginRequired
		}

		if err != nil {
			return err
		}

		if time.Now().Add(time.Minute).After(c.Expires) {
			c, err = m.exchange(ctx, c, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.RefreshToken}})
			if err != nil {
				return err
			}

			if err = m.save(c); err != nil {
				return err
			}
		}

		token = c.AccessToken
		return nil
	})
	return token, err
}

// Transport re-reads the shared file under its process lock before every request,
// so one adapter cannot replay a refresh token rotated by another process.
type Transport struct {
	Manager *Manager
	Base    http.RoundTripper
}

func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme+"://"+req.URL.Host != t.Manager.Base {
		return nil, errors.New("refusing to send credentials to another origin")
	}

	token, err := t.Manager.Token(req.Context())
	if err != nil {
		return nil, err
	}

	r := req.Clone(req.Context())
	r.Header = r.Header.Clone()
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	} else {
		r.Header.Del("Authorization")
	}

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	response, err := base.RoundTrip(r)
	if err == nil && response.StatusCode == http.StatusUnauthorized {
		response.Body.Close()
		return nil, ErrLoginRequired
	}

	return response, err
}

func (m *Manager) HTTPClient() *http.Client {
	return &http.Client{Timeout: 120 * time.Second, Transport: Transport{Manager: m}}
}

func openBrowser(target string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", target)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		c = exec.Command("xdg-open", target)
	}

	if err := c.Start(); err != nil {
		return err
	}

	go func() { _ = c.Wait() }()
	return nil
}

func (m *Manager) Login(ctx context.Context, stderr io.Writer) error {
	mode, err := m.mode(ctx)
	if err != nil {
		return err
	}

	if mode == "dev" || mode == "none" {
		return nil
	}

	if mode != "google" && mode != "password" {
		return errors.New("this installation does not offer browser login; use its configured machine credentials")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	redirect := "http://" + listener.Addr().String() + "/callback"
	var registered struct {
		ID string `json:"client_id"`
	}
	if err = m.call(ctx, "/oauth/register", nil, map[string]any{"client_name": "Envy CLI and local MCP", "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none"}, &registered); err != nil {
		return err
	}

	if registered.ID == "" {
		return errors.New("invalid client registration")
	}

	state, verifier := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	callback := make(chan string, 1)
	var once sync.Once
	server := &http.Server{ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != "GET" || r.URL.Path != "/callback" || r.Host != listener.Addr().String() || r.URL.Query().Get("state") != state {
			http.Error(w, "invalid callback", 400)
			return
		}

		code := r.URL.Query().Get("code")
		once.Do(func() { callback <- code })
		if code == "" {
			http.Error(w, "Login cancelled. Return to your terminal.", 400)
			return
		}

		_, _ = io.WriteString(w, "Envy authorization received. You can close this tab and return to your terminal.")
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	query := url.Values{"client_id": {registered.ID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {"envy offline_access"}, "state": {state}, "code_challenge_method": {"S256"}, "code_challenge": {oauth2.S256ChallengeFromVerifier(verifier)}, "resource": {m.Base + "/v1"}}
	target := m.Base + "/oauth/authorize?" + query.Encode()
	fmt.Fprintln(stderr, "Complete login in your browser:", target)
	if err := m.OpenBrowser(target); err != nil {
		fmt.Fprintln(stderr, "Open the URL above to continue.")
	}

	var code string
	select {
	case code = <-callback:
	case <-ctx.Done():
		return ctx.Err()
	}

	if code == "" {
		return errors.New("authorization cancelled")
	}

	c, err := m.exchange(ctx, Credentials{ClientID: registered.ID, Resource: m.Base + "/v1"}, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}})
	if err != nil {
		return err
	}

	return m.locked(ctx, func() error { return m.save(c) })
}

func (m *Manager) Logout(ctx context.Context) error {
	return m.locked(ctx, func() error {
		c, err := m.read()
		if errors.Is(err, ErrLoginRequired) {
			return nil
		}

		if err != nil {
			return err
		}

		if err = m.call(ctx, "/oauth/revoke", url.Values{"client_id": {c.ClientID}, "token": {c.RefreshToken}, "token_type_hint": {"refresh_token"}}, nil, nil); err != nil {
			return err
		}

		return os.Remove(m.path())
	})
}

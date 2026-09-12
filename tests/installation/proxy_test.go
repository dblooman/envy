package installation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

// This test targets a real chart installation and PostgreSQL. TLS terminates at
// a local authenticated proxy fixture; it does not certify Istio or an IdP.
func TestHTTPSProxyInstallation(t *testing.T) {
	ns := os.Getenv("ENVY_INSTALLATION_TEST_NAMESPACE")
	if ns == "" {
		t.Skip("run make test-helm for an isolated installation")
	}
	data, err := os.ReadFile(os.Getenv("ENVY_INSTALLATION_TEST_CREDENTIALS"))
	if err != nil {
		t.Fatal(err)
	}
	var creds struct {
		Proxy   string `json:"proxy"`
		Machine string `json:"machine"`
		Session string `json:"session"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", "-n", ns, "port-forward", "service/"+ns+"-envy", ":8081", "--address=127.0.0.1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("port forward did not start")
	}
	var port int
	if _, err := fmt.Sscanf(scanner.Text(), "Forwarding from 127.0.0.1:%d", &port); err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, stdout)
	upstream, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(upstream)
		for _, h := range []string{"X-Envy-User", "X-Envy-Email", "X-Envy-Proxy-Secret", "Cookie"} {
			pr.Out.Header.Del(h)
		}
		// A fixture session stands in for an upstream IdP login. Never trust caller identity headers.
		if cookie, err := pr.In.Cookie("session"); err == nil && cookie.Value == creds.Session {
			pr.Out.Header.Set("X-Envy-User", "alice")
			pr.Out.Header.Set("X-Envy-Proxy-Secret", creds.Proxy)
		}
	}}
	server := httptest.NewTLSServer(proxy)
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	request := func(base, method, path, body string, headers http.Header, status int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header = headers.Clone()
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			t.Fatalf("%s %s: status %d, expected %d: %s", method, path, res.StatusCode, status, b)
		}
		return b
	}
	human := http.Header{"Cookie": {"session=" + creds.Session}, "Origin": {"https://envy.acceptance.test"}, "X-Envy-User": {"mallory"}, "X-Envy-Channel": {"web"}}
	machine := http.Header{"Authorization": {"Bearer " + creds.Machine}, "X-Envy-Channel": {"mcp"}}
	var session struct {
		Principal domain.Principal `json:"principal"`
	}
	if err := json.Unmarshal(request(server.URL, "GET", "/v1/session", "", human, 200), &session); err != nil {
		t.Fatal(err)
	}
	if session.Principal.ID != "alice" || session.Principal.Kind != "human" {
		t.Fatalf("unexpected proxy identity: %+v", session)
	}
	request(upstream.String(), "GET", "/v1/session", "", http.Header{"X-Envy-User": {"alice"}}, 401)
	request(server.URL, "GET", "/v1/session", "", http.Header{"X-Envy-User": {"alice"}, "X-Envy-Proxy-Secret": {creds.Proxy}}, 401)
	ambiguous := http.Header{"X-Envy-User": {"alice", "mallory"}, "X-Envy-Proxy-Secret": {creds.Proxy}}
	request(upstream.String(), "GET", "/v1/session", "", ambiguous, 401)
	for _, auth := range []string{"Bearer invalid", "Basic invalid", "Bearer "} {
		headers := human.Clone()
		headers.Set("Authorization", auth)
		request(server.URL, "GET", "/v1/session", "", headers, 401)
	}
	for _, origin := range []string{"", "https://hostile.test", "http://envy.acceptance.test"} {
		headers := human.Clone()
		headers.Del("Origin")
		if origin != "" {
			headers.Set("Origin", origin)
		}
		request(server.URL, "POST", "/v1/projects", `{"id":"rejected","name":"Rejected"}`, headers, 401)
	}
	request(server.URL, "POST", "/v1/projects", `{"id":"human","name":"Human"}`, human, 201)
	request(server.URL, "POST", "/v1/projects", `{"id":"machine","name":"Machine"}`, machine, 201)
	var page domain.ActivityPage
	if err := json.Unmarshal(request(server.URL, "GET", "/v1/activity?action=catalog.project.register", "", machine, 200), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected exactly two accepted mutations, got %d", len(page.Items))
	}
	for _, activity := range page.Items {
		want := domain.Principal{Kind: "human", ID: "alice"}
		channel := "web"
		if activity.ResourceID == "machine" {
			want = domain.Principal{Kind: "service", ID: "acceptance-agent"}
			channel = "mcp"
		}
		if activity.Actor.Kind != want.Kind || activity.Actor.ID != want.ID || activity.Channel != channel {
			t.Fatalf("wrong persisted attribution: %+v", activity)
		}
	}
}

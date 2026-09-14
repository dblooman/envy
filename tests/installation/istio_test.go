package installation

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise Envoy's real HTTPS listener, SDS certificate loading, SNI routing,
// exact-host routing, bearer authentication and route removal. The existing
// gateway is borrowed; only uniquely named fixture objects are owned here.
func TestIstioHTTPSInstallation(t *testing.T) {
	ns := os.Getenv("ENVY_INSTALLATION_TEST_NAMESPACE")
	if ns == "" {
		t.Skip("run make test-helm")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 480*time.Second)
	defer cancel()
	kubectl := func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		cmd.Stdin = bytes.NewReader(input)
		return cmd.CombinedOutput()
	}
	create := func(object any) {
		t.Helper()
		body, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		if output, err := kubectl(ctx, body, "create", "-f", "-"); err != nil {
			t.Fatalf("create fixture: %v: %s", err, output)
		}
	}
	// Cleanup has its own deadline so a test timeout cannot strand credentials.
	defer func() {
		clean, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		for _, obj := range []struct{ namespace, resource string }{{ns, "virtualservice/https-acceptance"}, {ns, "gateway/https-acceptance"}, {"istio-system", "secret/" + ns + "-tls"}} {
			if output, err := kubectl(clean, nil, "-n", obj.namespace, "delete", obj.resource, "--ignore-not-found=true"); err != nil {
				t.Errorf("cleanup: %v: %s", err, output)
			}
		}
	}()
	domain := ns + ".acceptance.test"
	host := "api." + domain
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, DNSNames: []string{"*." + domain}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	create(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]string{"name": ns + "-tls", "namespace": "istio-system"}, "type": "kubernetes.io/tls", "data": map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM}})
	create(map[string]any{"apiVersion": "networking.istio.io/v1", "kind": "Gateway", "metadata": map[string]string{"name": "https-acceptance", "namespace": ns}, "spec": map[string]any{"selector": map[string]string{"istio": "ingressgateway"}, "servers": []any{map[string]any{"port": map[string]any{"number": 443, "name": "https", "protocol": "HTTPS"}, "tls": map[string]string{"mode": "SIMPLE", "credentialName": ns + "-tls"}, "hosts": []string{"*." + domain}}}}})
	create(map[string]any{"apiVersion": "networking.istio.io/v1", "kind": "VirtualService", "metadata": map[string]string{"name": "https-acceptance", "namespace": ns}, "spec": map[string]any{"hosts": []string{host}, "gateways": []string{"https-acceptance"}, "http": []any{map[string]any{"route": []any{map[string]any{"destination": map[string]any{"host": ns + "-envy." + ns + ".svc.cluster.local", "port": map[string]int{"number": 8081}}}}}}}})
	var port int
	var stopForward func()
	startForward := func() {
		t.Helper()
		if stopForward != nil {
			stopForward()
		}
		cmd := exec.CommandContext(ctx, "kubectl", "-n", "istio-system", "port-forward", "service/istio-ingressgateway", ":443", "--address=127.0.0.1")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		stopForward = func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
		scanner := bufio.NewScanner(stdout)
		if !scanner.Scan() {
			t.Fatal("ingress port-forward did not start")
		}
		if _, err := fmt.Sscanf(scanner.Text(), "Forwarding from 127.0.0.1:%d", &port); err != nil {
			t.Fatal(err)
		}
		go io.Copy(io.Discard, stdout)
	}
	defer func() {
		if stopForward != nil {
			stopForward()
		}
	}()
	startForward()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("invalid fixture certificate")
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, fmt.Sprintf("127.0.0.1:%d", port))
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: dial}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	data, err := os.ReadFile(os.Getenv("ENVY_INSTALLATION_TEST_CREDENTIALS"))
	if err != nil {
		t.Fatal(err)
	}
	var creds struct {
		Machine string `json:"machine"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		t.Fatal(err)
	}
	probe := func(host, path, token string) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", "https://"+host+path, nil)
		if err != nil {
			return 0, nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer res.Body.Close()
		b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return res.StatusCode, b, err
	}
	eventually := func(host, path, token string, want int) []byte {
		t.Helper()
		deadline := time.Now().Add(35 * time.Second)
		for {
			status, b, err := probe(host, path, token)
			if err == nil && status == want {
				return b
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				t.Fatalf("HTTPS convergence %s%s: status %d want %d: %v", host, path, status, want, err)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(250 * time.Millisecond):
			}
			// kubectl exits if the target listener has not opened yet. Reconnect
			// during convergence rather than retaining a dead local socket.
			if err != nil {
				transport.CloseIdleConnections()
				startForward()
			}
		}
	}
	body := eventually(host, "/v1/session", creds.Machine, 200)
	var session struct {
		Principal struct {
			ID string `json:"id"`
		} `json:"principal"`
	}
	if err := json.Unmarshal(body, &session); err != nil || session.Principal.ID != "acceptance-agent" {
		t.Fatalf("incorrect ingress session: %s", body)
	}
	eventually(host, "/v1/session", "", 401)
	eventually(host, "/v1/session", "invalid", 401)
	body = eventually(host, "/", "", 200)
	if !strings.Contains(string(body), "<html") {
		t.Fatal("same-origin website missing")
	}
	eventually("unknown."+domain, "/v1/session", creds.Machine, 404)
	// SNI still selects the listener, but certificate validation must fail.
	bad := transport.Clone()
	bad.TLSClientConfig.RootCAs = x509.NewCertPool()
	defer bad.CloseIdleConnections()
	res, err := (&http.Client{Transport: bad, Timeout: 3 * time.Second}).Get("https://" + host + "/v1/session")
	if err == nil {
		res.Body.Close()
		t.Fatal("untrusted certificate was accepted")
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); !ok {
		t.Fatalf("expected certificate trust failure, got %v", err)
	}
	acceptHTTPSComposition(t, ctx, ns, domain, creds.Machine, certPEM, client)
	if output, err := kubectl(ctx, nil, "-n", ns, "delete", "virtualservice/https-acceptance"); err != nil {
		t.Fatalf("remove route: %v: %s", err, output)
	}
	eventually(host, "/v1/session", creds.Machine, 404)
}

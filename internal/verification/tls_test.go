package verification

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTLSProbeUsesPublicNameAndPrivateDialAddress(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" {
			t.Errorf("wrong Host/SNI: %s/%s", r.Host, r.TLS.ServerName)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	verifier, err := NewWithRoots(server.URL, "example.com", nil, roots)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, err := verifier.status(context.Background(), "example.com", "/"); err != nil || code != 200 {
		t.Fatalf("trusted probe: code=%d err=%v", code, err)
	}
	if _, _, err := verifier.status(context.Background(), "wrong.example", "/"); err == nil {
		t.Fatal("wrong certificate hostname accepted")
	}
	untrusted, err := New(server.URL, "example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := untrusted.status(context.Background(), "example.com", "/"); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}

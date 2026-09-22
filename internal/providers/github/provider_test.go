package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

func TestInstallationScopeJWTAndEscapedRevision(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	p, err := New("123", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(err)
	}

	sha := strings.Repeat("a", 40)
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		if r.Method == "POST" {
			if r.URL.Path != "/app/installations/42/access_tokens" {
				t.Errorf("wrong installation %s", r.URL.Path)
			}

			pieces := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
			if len(pieces) != 3 {
				t.Error("missing JWT")
				w.WriteHeader(401)
				return
			}

			sig, _ := base64.RawURLEncoding.DecodeString(pieces[2])
			hash := sha256.Sum256([]byte(pieces[0] + "." + pieces[1]))
			if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, hash[:], sig) != nil {
				t.Error("invalid JWT signature")
			}

			claims, _ := base64.RawURLEncoding.DecodeString(pieces[1])
			var payload struct {
				Iss string
				Exp int64
			}
			json.Unmarshal(claims, &payload)
			if payload.Iss != "123" || payload.Exp <= time.Now().Unix() {
				t.Error("invalid JWT claims")
			}

			var body struct {
				Repositories []string          `json:"repositories"`
				Permissions  map[string]string `json:"permissions"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Repositories) != 1 || body.Repositories[0] != "backend" || body.Permissions["contents"] != "read" {
				t.Error("token not repository scoped")
			}

			w.Write([]byte(`{"token":"installation-secret"}`))
			return
		}

		if r.Header.Get("Authorization") != "Bearer installation-secret" {
			t.Error("wrong credential")
		}

		if r.URL.Path == "/repos/acme/backend" {
			w.Write([]byte(`{"full_name":"acme/backend"}`))
			return
		}

		if strings.Contains(r.URL.Path, "missing") {
			w.WriteHeader(404)
			return
		}

		if r.URL.Path == "/repos/acme/backend/commits" {
			json.NewEncoder(w).Encode([]any{map[string]any{"sha": sha, "commit": map[string]string{"message": "history"}}})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{"sha": sha, "commit": map[string]string{"message": "commit"}})
	}))
	defer server.Close()
	p.baseURL = server.URL
	p.client = server.Client()
	repo := domain.SourceRepository{GitHubRepository: "acme/backend", InstallationID: 42}
	if err = p.Check(context.Background(), repo); err != nil {
		t.Fatal(err)
	}

	got, err := p.Resolve(context.Background(), repo, "feature/a#b")
	if err != nil || got.SHA != sha {
		t.Fatalf("resolve %+v %v", got, err)
	}

	if paths[len(paths)-1] != "/repos/acme/backend/commits/refs%2Fheads%2Ffeature%2Fa%23b" {
		t.Fatalf("ref not escaped: %v", paths)
	}

	if _, err = p.Resolve(context.Background(), repo, "missing"); err == nil {
		t.Fatal("missing commit accepted")
	}

	if _, err = p.Resolve(context.Background(), repo, sha); err != nil {
		t.Fatal(err)
	}

	if paths[len(paths)-1] != "/repos/acme/backend/commits/"+sha {
		t.Fatal("SHA resolved as branch")
	}

	commits, err := p.Commits(context.Background(), repo, "feature/a#b", 2)
	if err != nil || len(commits) != 1 {
		t.Fatal("history failed")
	}
}

func TestGitHubErrorsDoNotExposeCredentialsOrResponseBodies(t *testing.T) {
	p := &Provider{client: http.DefaultClient}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte("sensitive upstream detail"))
	}))
	defer server.Close()
	p.baseURL = server.URL
	var out any
	err := p.request(context.Background(), "GET", "/repos/x/y", "secret-token", nil, &out)
	if err == nil || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unsafe error: %v", err)
	}
}

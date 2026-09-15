package verification

import (
	"context"
	"github.com/dblooman/envy/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPVerificationAcceptsBusinessResponsesWithoutClaimingRouting(t *testing.T) {
	for _, code := range []int{200, 302, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			hosts := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hosts = append(hosts, r.Host)
				if r.URL.Path != "/products" {
					t.Error("probe path lost")
				}

				if r.Header.Get("Baggage") != "" {
					t.Error("verifier injected its own context")
				}

				if r.Host == "preview.test" {
					w.Header().Set("Location", "http://baseline.test/products")
					w.WriteHeader(code)
				}

				w.Write([]byte("ordinary application content"))
			}))
			defer server.Close()
			v, _ := New(server.URL, "baseline.test", nil)
			plan := domain.ResolvedPlan{Baseline: domain.Baseline{Endpoint: "http://baseline.test", Verification: domain.VerificationContract{Kind: "http", Path: "/products", ExpectedStatus: 200}}, Components: map[string]domain.Component{"pricing": {ID: "pricing"}}}
			result, err := v.Verify(context.Background(), "abc", "preview.test", map[string]string{"pricing": "observed-pod"}, plan)
			if (err == nil) != (code == 200) {
				t.Fatalf("HTTP %d: %v", code, err)
			}

			if len(result.Composition) != 0 || len(result.Baseline) != 0 {
				t.Fatal("HTTP check fabricated per-hop evidence")
			}

			if len(hosts) != 2 || hosts[0] != "baseline.test" || hosts[1] != "preview.test" {
				t.Fatal("host routing or no-redirect contract lost")
			}
		})
	}
}
func TestHTTPVerificationRejectsBaselineFailureAndMissingIdentity(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	v, _ := New(server.URL, "baseline.test", nil)
	plan := domain.ResolvedPlan{Baseline: domain.Baseline{Endpoint: "http://baseline.test", Verification: domain.VerificationContract{Kind: "http", Path: "/", ExpectedStatus: 200}}, Components: map[string]domain.Component{"pricing": {ID: "pricing"}}}
	if _, err := v.Verify(context.Background(), "abc", "preview.test", map[string]string{"pricing": ""}, plan); err == nil || calls != 0 {
		t.Fatal("missing identity reached ingress")
	}

	if _, err := v.Verify(context.Background(), "abc", "preview.test", map[string]string{"pricing": "pod"}, plan); err == nil || calls != 1 {
		t.Fatal("baseline failure ignored")
	}
}

func TestApplication404IsNotIngressWithdrawal(t *testing.T) {
	marker := "owned-composition"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if marker != "" {
			w.Header().Set(domain.PreviewRouteHeader, marker)
		}

		w.WriteHeader(404)
	}))
	defer server.Close()
	v, _ := New(server.URL, "baseline.test", nil)
	if err := v.Absent(context.Background(), "preview.test"); err == nil {
		t.Fatal("application 404 was mistaken for ingress withdrawal")
	}

	marker = ""
	if err := v.Absent(context.Background(), "preview.test"); err != nil {
		t.Fatal(err)
	}
}

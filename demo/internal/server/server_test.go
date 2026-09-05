package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dblooman/envy/demo/protocol"
)

func TestChainPropagatesRequestComposition(t *testing.T) {
	b := httptest.NewServer(Handler("service-b", "", "uid-b", "installed-override"))
	defer b.Close()
	a := httptest.NewServer(Handler("service-a", b.URL, "uid-a", "baseline"))
	defer a.Close()
	g := httptest.NewServer(Handler("gateway", a.URL, "uid-g", "baseline"))
	defer g.Close()
	r, _ := http.NewRequest(http.MethodGet, g.URL, nil)
	r.Header.Set("baggage", "tenant=test,composition=from-request;property=yes")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var result protocol.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Chain) != 3 {
		t.Fatalf("chain: %#v", result)
	}
	for i, name := range []string{"gateway", "service-a", "service-b"} {
		h := result.Chain[i]
		if h.Service != name || h.Composition != "from-request" || h.WorkloadID == "" {
			t.Fatalf("hop: %#v", h)
		}
	}
	if result.Chain[2].DeploymentComposition != "installed-override" {
		t.Fatal("request context overwrote workload identity")
	}
}

func TestDownstreamFailureCannotReturnSuccessfulChain(t *testing.T) {
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer b.Close()
	r := httptest.NewRecorder()
	Handler("service-a", b.URL, "a", "baseline").ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/", nil))
	if r.Code != http.StatusBadGateway {
		t.Fatalf("got %d", r.Code)
	}
}

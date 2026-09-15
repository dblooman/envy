package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type identityService struct{ fakeService }

func (s *identityService) Get(ctx context.Context, id string) (domain.Composition, error) {
	i := domain.RequestIdentityFromContext(ctx)
	return domain.Composition{ID: id, Name: i.Principal.ID, Phase: domain.Phase(i.Channel), Overrides: map[string]domain.ComponentOverride{}, Components: map[string]domain.ComponentObservation{}, Endpoints: map[string]domain.Endpoint{}, Conditions: []domain.Condition{}}, nil
}

type bearerTransport string

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(t))
	return http.DefaultTransport.RoundTrip(r)
}
func TestRemoteMCPIdentityIsolation(t *testing.T) {
	h := NewConfiguredHandler(&identityService{}, AuthConfig{Mode: "token", MachineCredentials: []MachineCredential{{ID: "alice", Token: "alice-token"}, {ID: "bob", Token: "bob-token"}}}, Installation{}, nil, nil)
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, name := range []string{"alice", "bob"} {
		wg.Go(func() {
			c := sdk.NewClient(&sdk.Implementation{Name: name, Version: "test"}, nil)
			session, err := c.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport(name + "-token")}}, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer session.Close()
			for range 3 {
				result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "get_composition", Arguments: map[string]any{"id": "abc"}})
				if err != nil {
					t.Error(err)
					return
				}
				if result.IsError {
					t.Errorf("tool error: %v", result)
					return
				}
				data := result.StructuredContent.(map[string]any)
				if data["name"] != name || data["phase"] != "mcp" {
					t.Errorf("identity leaked: %v", data)
				}
			}
		})
	}
	wg.Wait()
}
func TestRemoteMCPRejectsBuildCredentialsAndCrossOrigin(t *testing.T) {
	h := NewConfiguredHandler(&identityService{}, AuthConfig{Mode: "dev", ExternalOrigin: "https://envy.test"}, Installation{}, nil, []BuildCredential{{Token: "build-token", Project: "demo", Repository: "repo", Components: []string{"service"}}})
	r := httptest.NewRequest("POST", "https://envy.test/mcp", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer build-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("build token accepted", w.Code)
	}
	r = httptest.NewRequest("POST", "https://envy.test/mcp", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin request accepted", w.Code)
	}
}

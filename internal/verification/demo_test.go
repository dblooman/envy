package verification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dblooman/envy/demo/protocol"
	"github.com/dblooman/envy/internal/domain"
)

func TestVerificationRequiresActualOverrideAndSharedHops(t *testing.T) {
	for _, tt := range []struct {
		name    string
		mutate  func([]protocol.Hop)
		wantErr bool
	}{
		{"valid", func([]protocol.Hop) {}, false},
		{"wrong version source pod", func(h []protocol.Hop) { h[2].WorkloadID = "baseline-b" }, true},
		{"lost baggage", func(h []protocol.Hop) { h[1].Composition = "" }, true},
		{"duplicated gateway", func(h []protocol.Hop) { h[0].WorkloadID = "copy" }, true},
		{"fake deployment identity", func(h []protocol.Hop) { h[2].DeploymentComposition = "baseline" }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				chain := []protocol.Hop{{Service: "gateway", Version: "v3", WorkloadID: "g", DeploymentComposition: "baseline"}, {Service: "service-a", Version: "v4", WorkloadID: "a", DeploymentComposition: "baseline"}, {Service: "service-b", Version: "v1", WorkloadID: "baseline-b", DeploymentComposition: "baseline"}}
				if r.Host == "cmp.envy.localhost" {
					for i := range chain {
						chain[i].Composition = "id"
					}

					chain[2].Version = "v2"
					chain[2].WorkloadID = "override-b"
					chain[2].DeploymentComposition = "id"
					tt.mutate(chain)
				}

				_ = json.NewEncoder(w).Encode(protocol.Response{Chain: chain})
			}))
			defer s.Close()
			v, err := New(s.URL, "baseline.envy.localhost", s.Client())
			if err != nil {
				t.Fatal(err)
			}

			_, err = v.Verify(context.Background(), "id", "cmp.envy.localhost", map[string]string{"service-b": "override-b"}, domain.ResolvedPlan{Component: domain.Component{ID: "service-b"}, Baseline: domain.Baseline{Endpoint: "http://baseline.envy.localhost", Verification: domain.VerificationContract{Kind: "envy-chain", Chain: []string{"gateway", "service-a", "service-b"}}}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Verify error=%v wantError=%v", err, tt.wantErr)
			}
		})
	}
}

func TestAbsentRequiresNoRoute(t *testing.T) {
	for _, code := range []int{200, 404, 503} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			if code == 200 {
				_, _ = w.Write([]byte(`{"chain":[]}`))
			}
		}))
		v, _ := New(s.URL, "baseline.envy.localhost", s.Client())
		err := v.Absent(context.Background(), "old.envy.localhost")
		s.Close()
		if (err == nil) != (code == 404) {
			t.Fatalf("code=%d err=%v", code, err)
		}
	}
}

func TestRegisteredEntryAndMiddleOverrides(t *testing.T) {
	for _, index := range []int{0, 1} {
		t.Run([]string{"entry", "middle"}[index], func(t *testing.T) {
			names := []string{"front", "worker", "database-client"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				chain := make([]protocol.Hop, 3)
				for i, name := range names {
					chain[i] = protocol.Hop{Service: name, Version: "v1", WorkloadID: name, DeploymentComposition: "baseline"}
				}

				if r.Host == "preview.envy.localhost" {
					for i := range chain {
						chain[i].Composition = "id"
					}

					chain[index].WorkloadID = "override"
					chain[index].DeploymentComposition = "id"
					chain[index].Version = "v2"
				}

				json.NewEncoder(w).Encode(protocol.Response{Chain: chain})
			}))
			defer server.Close()
			v, _ := New(server.URL, "unused.envy.localhost", server.Client())
			plan := domain.ResolvedPlan{Component: domain.Component{ID: names[index]}, Baseline: domain.Baseline{Endpoint: "http://registered.envy.localhost", Verification: domain.VerificationContract{Kind: "envy-chain", Chain: names}}}
			if _, err := v.Verify(context.Background(), "id", "preview.envy.localhost", map[string]string{names[index]: "override"}, plan); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEveryOverrideRequiresItsOwnObservedPod(t *testing.T) {
	for _, bad := range []string{"", "service-a", "service-b", "missing"} {
		t.Run(bad, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				names := []string{"gateway", "service-a", "service-b"}
				chain := make([]protocol.Hop, 3)
				for i, name := range names {
					chain[i] = protocol.Hop{Service: name, Version: "v1", WorkloadID: name + "-baseline", DeploymentComposition: "baseline"}
					if r.Host == "preview.envy.localhost" {
						chain[i].Composition = "id"
						if i > 0 {
							chain[i].DeploymentComposition = "id"
							chain[i].WorkloadID = name + "-override"
							if name == bad {
								chain[i].WorkloadID = name + "-baseline"
							}
						}
					}
				}

				json.NewEncoder(w).Encode(protocol.Response{Chain: chain})
			}))
			defer server.Close()
			v, _ := New(server.URL, "baseline.envy.localhost", server.Client())
			plan := domain.ResolvedPlan{Components: map[string]domain.Component{"service-a": {ID: "service-a"}, "service-b": {ID: "service-b"}}, Baseline: domain.Baseline{Endpoint: "http://baseline.envy.localhost", Verification: domain.VerificationContract{Kind: "envy-chain", Chain: []string{"gateway", "service-a", "service-b"}}}}
			pods := map[string]string{"service-a": "service-a-override", "service-b": "service-b-override"}
			if bad == "missing" {
				delete(pods, "service-b")
			}

			_, err := v.Verify(context.Background(), "id", "preview.envy.localhost", pods, plan)
			if (err != nil) != (bad != "") {
				t.Fatalf("verification error: %v", err)
			}
		})
	}
}

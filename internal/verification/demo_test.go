package verification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dblooman/envy/demo/protocol"
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
			_, err = v.Verify(context.Background(), "id", "cmp.envy.localhost", "override-b")
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

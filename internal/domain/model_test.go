package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeAndDeletionAreNotPublic(t *testing.T) {
	c := Composition{ID: "example", DeletionRequested: true, Runtime: RuntimeState{OwnershipToken: "private-owner", RoutingActive: true, Workload: WorkloadRef{NamespaceUID: "private-namespace"}}}
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-owner", "private-namespace", "DeletionRequested", "Runtime", "RoutingActive"} {
		if strings.Contains(string(body), private) {
			t.Fatalf("private reconciler field escaped API: %s", body)
		}
	}
}
func TestNamespaceForID(t *testing.T) {
	if got := NamespaceForID("cmp_123"); got != "envy-cmp-123" {
		t.Fatalf("namespace=%s", got)
	}
}

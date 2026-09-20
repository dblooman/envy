package kubeapply

import "testing"

func TestRouteOrderAndInjectedContainers(t *testing.T) {
	a := map[string]any{"name": "preview", "match": "composition"}
	b := map[string]any{"name": "baseline"}
	if subset([]any{a, b}, []any{b, a}, "http") {
		t.Fatal("routing order drift ignored")
	}

	if subset([]any{a, b}, []any{a, b, map[string]any{"name": "unexpected"}}, "rules") {
		t.Fatal("unexpected route accepted")
	}

	if !subset([]any{a}, []any{a, b}, "containers") {
		t.Fatal("unowned injected container not preserved")
	}
}

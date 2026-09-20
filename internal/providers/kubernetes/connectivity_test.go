package kubernetes

import (
	"testing"

	"github.com/dblooman/envy/internal/domain"
)

func TestConnectivityFindings(t *testing.T) {
	b := domain.Baseline{Components: map[string]domain.BaselineBinding{"checkout": {ServiceHost: "checkout.staging.svc.example.internal"}}}
	for _, tc := range []struct {
		value, replacement string
		count              int
	}{
		{"checkout", "checkout.staging.svc.example.internal", 1},
		{"checkout:8080", "checkout.staging.svc.example.internal:8080", 1},
		{"http://checkout:8080/cart", "http://checkout.staging.svc.example.internal:8080/cart", 1},
		{"http://checkout:8080/cart?page=2", "http://checkout.staging.svc.example.internal:8080/cart?page=2", 1},
		{"unknown", "", 1},
		{"checkout.staging", "", 0},
		{"http://user:password@checkout", "", 0},
		{"http://checkout?token=secret", "", 0},
		{"{\"host\":\"checkout\"}", "", 0},
	} {
		got := connectivityFindings(b, "env/CHECKOUT_URL", tc.value)
		if len(got) != tc.count || (len(got) > 0 && got[0].Replacement != tc.replacement) {
			t.Fatalf("%q: %+v", tc.value, got)
		}
	}

	if len(connectivityFindings(b, "env/PASSWORD", "checkout")) != 0 {
		t.Fatal("credential exposed")
	}
}

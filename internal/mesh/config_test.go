package mesh

import "testing"

func TestProfiles(t *testing.T) {
	for _, name := range []string{"", "istio", "cilium", "linkerd"} {
		p, e := Resolve(name)
		if e != nil {
			t.Fatal(e)
		}
		_, e = p.IngressURL("")
		if (e == nil) != (p.Name == "istio") {
			t.Fatalf("unexpected ingress default for %s", name)
		}
	}
	for _, name := range []string{"gateway-api", "CILIUM", "typo"} {
		if _, e := Resolve(name); e == nil {
			t.Fatalf("accepted %s", name)
		}
	}
}

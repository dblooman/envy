package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallationSpecValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installation.json")
	data := `{"namespace":"envy-system","gateway":{"namespace":"istio-system","name":"preview"},"database_secret":{"name":"database","key":"url"},"preview_base_url":"https://envy.example.test","ingress_url":"https://ingress.example.test","baseline_host":"baseline.example.test","injection_labels":{"istio.io/rev":"production"},"ingress_selector":{"istio":"private-ingressgateway"}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	spec, err := readInstallationSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range evaluateInstallationSpec(spec) {
		if check.Status == "fail" {
			t.Fatalf("unexpected failed check: %+v", check)
		}
	}
	if err := os.WriteFile(path, []byte(`{"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readInstallationSpec(path); err == nil {
		t.Fatal("unknown installation field accepted")
	}
}

func TestInstallationReadinessRequiresCompleteEvidence(t *testing.T) {
	for _, tc := range []struct {
		statuses []string
		want     int
	}{
		{[]string{"pass"}, 0}, {[]string{"pass", "unknown"}, 2},
		{[]string{"unknown", "fail"}, 1}, {[]string{"fail", "unknown"}, 1},
	} {
		var checks []InstallationCheck
		for _, status := range tc.statuses {
			checks = append(checks, InstallationCheck{Status: status})
		}
		if got := installationExitCode(checks); got != tc.want {
			t.Fatalf("%v: %d want %d", tc.statuses, got, tc.want)
		}
	}
}

func TestNonIstioInstallationDoesNotRequireInjectionOrSelector(t *testing.T) {
	for _, name := range []string{"cilium", "linkerd"} {
		t.Run(name, func(t *testing.T) {
			var spec installationSpec
			spec.Mesh.Provider = name
			spec.Namespace = "envy-system"
			spec.Gateway.Name = "preview"
			spec.Gateway.Namespace = "staging"
			spec.DatabaseSecret.Name = "database"
			spec.DatabaseSecret.Key = "url"
			spec.PreviewBaseURL = "https://preview.example.test"
			spec.IngressURL = "https://ingress.example.test"
			spec.BaselineHost = "baseline.example.test"
			for _, c := range evaluateInstallationSpec(spec) {
				if c.Status == "fail" {
					t.Fatalf("unexpected prerequisite: %+v", c)
				}
			}
		})
	}
}

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallationSpecValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installation.json")
	data := `{"namespace":"envy-system","gateway":{"namespace":"istio-system","name":"preview"},"database_secret":{"name":"database","key":"url"},"preview_base_url":"https://envy.example.test","ingress_url":"https://ingress.example.test","baseline_host":"baseline.example.test"}`
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

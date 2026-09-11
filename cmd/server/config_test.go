package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServerConfigStrictAndEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"installation_id":"file-install","auth":{"mode":"none"},"limits":{"default_ttl":"4h"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadServerConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Mode != "none" || cfg.Limits.DefaultTTL != "4h" {
		t.Fatalf("cfg=%+v", cfg)
	}
	t.Setenv("ENVY_TEST_PRECEDENCE", "environment")
	if got := configured("ENVY_TEST_PRECEDENCE", "file", "default"); got != "environment" {
		t.Fatal(got)
	}
	if err = os.WriteFile(path, []byte(`{"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadServerConfig(path); err == nil {
		t.Fatal("unknown configuration field accepted")
	}
	if err = os.WriteFile(path, []byte(`{} trailing`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadServerConfig(path); err == nil {
		t.Fatal("trailing configuration data accepted")
	}
}

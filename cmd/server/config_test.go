package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServerConfigStrictAndEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"installation_id":"file-install","auth":{"mode":"none"},"limits":{"default_ttl":"4h"},"runtime":{"preview_base_url":"https://envy.example.test"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadServerConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Mode != "none" || cfg.Limits.DefaultTTL != "4h" || cfg.Runtime.PreviewBaseURL != "https://envy.example.test" {
		t.Fatalf("cfg=%+v", cfg)
	}
	if err := os.WriteFile(path, []byte(`{"installation_id":"mesh-install","mesh":{"provider":"gateway-api"},"gateway_api":{"gateway_class":"cilium"},"cilium":{"native_cec":true},"linkerd":{"inject_annotation":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	meshCfg, err := loadServerConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if meshCfg.Mesh.Provider != "gateway-api" || meshCfg.GatewayAPI.GatewayClass != "cilium" || !meshCfg.Cilium.NativeCEC || !meshCfg.Linkerd.InjectAnnotation {
		t.Fatalf("meshCfg=%+v", meshCfg)
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

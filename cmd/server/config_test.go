package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServerConfigStrictAndEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"installation_id":"file-install","auth":{"mode":"none"},"limits":{"default_ttl":"4h"},"runtime":{"preview_base_url":"https://envy.example.test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadServerConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Auth.Mode != "none" || cfg.Limits.DefaultTTL != "4h" || cfg.Runtime.PreviewBaseURL != "https://envy.example.test" {
		t.Fatalf("cfg=%+v", cfg)
	}

	if err := os.WriteFile(path, []byte(`{"installation_id":"mesh-install","mesh":{"provider":"gateway-api"},"gateway_api":{"gateway_class":"cilium"},"cilium":{"native_cec":true},"linkerd":{"inject_annotation":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadServerConfig(path); err == nil {
		t.Fatal("retired configuration accepted")
	}

	t.Setenv("ENVY_TEST_PRECEDENCE", "environment")
	if got := configured("ENVY_TEST_PRECEDENCE", "file", "default"); got != "environment" {
		t.Fatal(got)
	}

	if err = os.WriteFile(path, []byte(`{"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err = loadServerConfig(path); err == nil {
		t.Fatal("unknown configuration field accepted")
	}

	if err = os.WriteFile(path, []byte(`{} trailing`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err = loadServerConfig(path); err == nil {
		t.Fatal("trailing configuration data accepted")
	}
}

func TestPasswordConfiguration(t *testing.T) {
	var cfg serverFileConfig
	cfg.Auth.Mode = "password"
	cfg.Auth.ExternalOrigin = "http://127.0.0.1:8081"
	c, err := loginConfig(cfg)
	if err != nil || c.Password != "admin" {
		t.Fatalf("default password: %v", err)
	}

	path := filepath.Join(t.TempDir(), "password")
	if err = os.WriteFile(path, []byte("file-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg.Auth.AdminPasswordFile = path
	c, err = loginConfig(cfg)
	if err != nil || c.Password != "file-password" {
		t.Fatal("file password failed", err)
	}

	t.Setenv("ENVY_ADMIN_PASSWORD", "environment-password")
	c, err = loginConfig(cfg)
	if err != nil || c.Password != "environment-password" {
		t.Fatal("env did not replace file", err)
	}

	t.Setenv("ENVY_ADMIN_PASSWORD_FILE", path)
	if _, err = loginConfig(cfg); err == nil {
		t.Fatal("conflicting environment secrets accepted")
	}
}

func TestPasswordFileConfigurationConflict(t *testing.T) {
	var cfg serverFileConfig
	cfg.Auth.Mode = "password"
	cfg.Auth.ExternalOrigin = "https://envy.test"
	cfg.Auth.AdminPassword = "value"
	cfg.Auth.AdminPasswordFile = "file"
	if _, err := loginConfig(cfg); err == nil {
		t.Fatal("ambiguous file config accepted")
	}
}

func TestNamespacePolicyUsesEffectiveMeshProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mesh":{"provider":"istio"},"namespace_policy":{"cilium_ingress":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ENVY_MESH_PROVIDER", "cilium")
	if _, err := loadServerConfig(path); err != nil {
		t.Fatal("environment provider override rejected", err)
	}

	t.Setenv("ENVY_MESH_PROVIDER", "linkerd")
	if _, err := loadServerConfig(path); err == nil {
		t.Fatal("Cilium-only allowance accepted for Linkerd")
	}
}

func TestServerSettingsResolvePreviewPolicyBeforeProviderConstruction(t *testing.T) {
	t.Setenv("ENVY_CONFIG_FILE", "")
	t.Setenv("ENVY_AUTH_MODE", "dev")
	t.Setenv("ENVY_PREVIEW_CONTROLLER_NAMESPACE", "envy-system")
	t.Setenv("ENVY_PREVIEW_CONTROLLER_SERVICE_ACCOUNT", "envy-server")
	t.Setenv("ENVY_PREVIEW_DEPENDENCY_CLUSTER_ROLE", "envy-preview-dependencies")
	settings, cleanup, err := loadServerSettings()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	policy := settings.fileConfig.Preview
	if policy.ControllerNamespace != "envy-system" || policy.ControllerServiceAccount != "envy-server" || policy.DependencyClusterRole != "envy-preview-dependencies" {
		t.Fatalf("runtime factories would capture unresolved preview policy: %+v", policy)
	}
}

func TestServerSettingsRejectIncompletePreviewPolicy(t *testing.T) {
	t.Setenv("ENVY_CONFIG_FILE", "")
	t.Setenv("ENVY_PREVIEW_CONTROLLER_NAMESPACE", "envy-system")
	t.Setenv("ENVY_PREVIEW_CONTROLLER_SERVICE_ACCOUNT", "")
	t.Setenv("ENVY_PREVIEW_DEPENDENCY_CLUSTER_ROLE", "")
	_, _, err := loadServerSettings()
	if err == nil {
		t.Fatal("incomplete preview policy must fail before provider construction")
	}
}

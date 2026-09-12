package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

type serverFileConfig struct {
	InstallationID string `json:"installation_id"`
	ListenAddr     string `json:"listen_addr"`
	Kubeconfig     string `json:"kubeconfig"`
	WebDir         string `json:"web_dir"`
	Runtime        struct {
		PreviewBaseURL string `json:"preview_base_url"`
		IngressURL     string `json:"ingress_url"`
		BaselineHost   string `json:"baseline_host"`
	} `json:"runtime"`
	Istio struct {
		InjectionLabels map[string]string `json:"injection_labels"`
		IngressSelector map[string]string `json:"ingress_selector"`
	} `json:"istio"`
	Auth struct {
		Mode                   string `json:"mode"`
		APITokenFile           string `json:"api_token_file"`
		MachineCredentialsFile string `json:"machine_credentials_file"`
		ProxySecretFile        string `json:"proxy_secret_file"`
		IdentityHeader         string `json:"identity_header"`
		EmailHeader            string `json:"email_header"`
		TrustedProxyCIDRs      string `json:"trusted_proxy_cidrs"`
	} `json:"auth"`
	Limits struct {
		DefaultTTL      string `json:"default_ttl"`
		MaxTTL          string `json:"max_ttl"`
		MaxCompositions string `json:"max_compositions"`
		AuditRetention  string `json:"audit_retention"`
	} `json:"limits"`
	GitHub struct {
		AppID                string `json:"app_id"`
		PrivateKeyFile       string `json:"private_key_file"`
		BuildCredentialsFile string `json:"build_credentials_file"`
	} `json:"github"`
}

func loadServerConfig(path string) (serverFileConfig, error) {
	var cfg serverFileConfig
	if path == "" {
		return cfg, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("open ENVY_CONFIG_FILE: %w", err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode ENVY_CONFIG_FILE: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("ENVY_CONFIG_FILE must contain one JSON object")
	}
	return cfg, nil
}
func configured(key, fileValue, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	if fileValue != "" {
		return fileValue
	}
	return fallback
}

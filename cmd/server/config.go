package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dblooman/envy/internal/authn"
	"github.com/dblooman/envy/internal/mesh"
	kubeprovider "github.com/dblooman/envy/internal/providers/kubernetes"
	"io"
	"os"
	"strings"
)

type serverFileConfig struct {
	PubSubEnabled            bool                       `json:"pubsub_enabled"`
	Preview                  kubeprovider.PreviewPolicy `json:"preview"`
	ApprovedImagePullSecrets []string                   `json:"approved_image_pull_secrets"`
	InstallationID           string                     `json:"installation_id"`
	ListenAddr               string                     `json:"listen_addr"`
	Kubeconfig               string                     `json:"kubeconfig"`
	WebDir                   string                     `json:"web_dir"`
	Runtime                  struct {
		PreviewBaseURL string `json:"preview_base_url"`
		IngressCAFile  string `json:"ingress_ca_file"`
		IngressURL     string `json:"ingress_url"`
		BaselineHost   string `json:"baseline_host"`
	} `json:"runtime"`
	Mesh       mesh.Config `json:"mesh"`
	GatewayAPI struct {
		GatewayClass    string            `json:"gateway_class"`
		InjectionLabels map[string]string `json:"injection_labels"`
	} `json:"gateway_api"`
	Cilium struct {
		NativeCEC *bool `json:"native_cec"`
	} `json:"cilium"`
	Linkerd struct {
		InjectAnnotation *bool `json:"inject_annotation"`
	} `json:"linkerd"`
	Istio struct {
		InjectionLabels map[string]string `json:"injection_labels"`
		IngressSelector map[string]string `json:"ingress_selector"`
	} `json:"istio"`
	Auth struct {
		Clients                []authn.RegisteredClient `json:"oauth_clients"`
		Mode                   string                   `json:"mode"`
		AdminPassword          string                   `json:"admin_password"`
		AdminPasswordFile      string                   `json:"admin_password_file"`
		GoogleClientID         string                   `json:"google_client_id"`
		GoogleClientSecret     string                   `json:"google_client_secret"`
		GoogleClientSecretFile string                   `json:"google_client_secret_file"`
		GoogleDomains          string                   `json:"google_allowed_domains"`
		GoogleEmails           string                   `json:"google_allowed_emails"`
		APITokenFile           string                   `json:"api_token_file"`
		MachineCredentialsFile string                   `json:"machine_credentials_file"`
		ProxySecretFile        string                   `json:"proxy_secret_file"`
		IdentityHeader         string                   `json:"identity_header"`
		EmailHeader            string                   `json:"email_header"`
		TrustedProxyCIDRs      string                   `json:"trusted_proxy_cidrs"`
		ExternalOrigin         string                   `json:"external_origin"`
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

	defer func() { _ = f.Close() }()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode ENVY_CONFIG_FILE: %w", err)
	}

	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("ENVY_CONFIG_FILE must contain one JSON object")
	}

	if err := cfg.Preview.Validate(); err != nil {
		return cfg, err
	}

	if cfg.Cilium.NativeCEC != nil || cfg.Linkerd.InjectAnnotation != nil || len(cfg.GatewayAPI.InjectionLabels) > 0 {
		return cfg, fmt.Errorf("retired experimental mesh settings: remove cilium.native_cec, linkerd.inject_annotation and gateway_api.injection_labels; select istio, cilium or linkerd (drain experimental installations with the previous server first)")
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

// An environment value replaces both forms of a file-configured secret.
func configuredSecret(valueKey, fileKey, value, path, fallback string) (string, error) {
	ev, evSet := os.LookupEnv(valueKey)
	ep, epSet := os.LookupEnv(fileKey)
	if evSet && epSet {
		return "", fmt.Errorf("configure only one of %s and %s", valueKey, fileKey)
	}

	if evSet {
		if ev == "" {
			return "", fmt.Errorf("%s must not be empty", valueKey)
		}

		return ev, nil
	}

	if epSet {
		value = ""
		path = ep
		if path == "" {
			return "", fmt.Errorf("%s must not be empty", fileKey)
		}
	}

	if value != "" && path != "" {
		return "", fmt.Errorf("configure a secret value or file, not both")
	}

	if path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return "", fmt.Errorf("read %s: %w", fileKey, e)
		}

		value = strings.TrimRight(string(b), "\r\n")
		if value == "" {
			return "", fmt.Errorf("%s is empty", fileKey)
		}
	}

	if value == "" {
		value = fallback
	}

	return value, nil
}
func loginConfig(cfg serverFileConfig) (authn.Config, error) {
	c := authn.Config{Clients: cfg.Auth.Clients, Mode: configured("ENVY_AUTH_MODE", cfg.Auth.Mode, "token"), Origin: configured("ENVY_EXTERNAL_ORIGIN", cfg.Auth.ExternalOrigin, "")}
	var err error
	if c.Mode == "password" {
		c.Password, err = configuredSecret("ENVY_ADMIN_PASSWORD", "ENVY_ADMIN_PASSWORD_FILE", cfg.Auth.AdminPassword, cfg.Auth.AdminPasswordFile, "admin")
		if err != nil {
			return c, err
		}
	}

	if c.Mode == "google" {
		c.GoogleClientID = configured("ENVY_GOOGLE_CLIENT_ID", cfg.Auth.GoogleClientID, "")
		c.GoogleClientSecret, err = configuredSecret("ENVY_GOOGLE_CLIENT_SECRET", "ENVY_GOOGLE_CLIENT_SECRET_FILE", cfg.Auth.GoogleClientSecret, cfg.Auth.GoogleClientSecretFile, "")
		if err != nil {
			return c, err
		}

		c.GoogleDomains = splitList(configured("ENVY_GOOGLE_ALLOWED_DOMAINS", cfg.Auth.GoogleDomains, ""))
		c.GoogleEmails = splitList(configured("ENVY_GOOGLE_ALLOWED_EMAILS", cfg.Auth.GoogleEmails, ""))
	}

	return c, authn.Validate(c)
}
func splitList(s string) []string {
	var out []string
	for v := range strings.SplitSeq(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}

	return out
}

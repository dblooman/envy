// Command server runs Envy's HTTP control plane and its single active reconciler.
package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dblooman/envy/internal/api"
	"github.com/dblooman/envy/internal/application"
	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/persistence/postgres"
	githubprovider "github.com/dblooman/envy/internal/providers/github"
	istioprovider "github.com/dblooman/envy/internal/providers/istio"
	kubeprovider "github.com/dblooman/envy/internal/providers/kubernetes"
	registryprovider "github.com/dblooman/envy/internal/providers/registry"
	"github.com/dblooman/envy/internal/reconciler"
	"github.com/dblooman/envy/internal/verification"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var err error
	if len(os.Args) == 2 && os.Args[1] == "migrate" {
		err = migrate(ctx)
	} else {
		err = run(ctx)
	}
	if err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// migrate is deliberately a narrow entrypoint for the chart migration Job.
// Store.Migrate owns the PostgreSQL advisory lock, so overlapping Jobs and
// older startup paths cannot apply the same migration concurrently.
func migrate(ctx context.Context) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := postgres.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.Migrate(ctx)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return d, nil
}

func run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	fileConfig, err := loadServerConfig(os.Getenv("ENVY_CONFIG_FILE"))
	if err != nil {
		return err
	}
	token := os.Getenv("ENVY_API_TOKEN")
	if path := configured("ENVY_API_TOKEN_FILE", fileConfig.Auth.APITokenFile, ""); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read API token file: %w", err)
		}
		token = strings.TrimSpace(string(b))
	}
	authMode := configured("ENVY_AUTH_MODE", fileConfig.Auth.Mode, "token")
	if authMode != "token" && authMode != "none" && authMode != "proxy" {
		return fmt.Errorf("ENVY_AUTH_MODE must be token, none, or proxy")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	installation := configured("ENVY_INSTALLATION_ID", fileConfig.InstallationID, "envy-local")
	if os.Getenv("ENVY_DEFAULT_TTL") == "" && fileConfig.Limits.DefaultTTL != "" {
		if err = os.Setenv("ENVY_DEFAULT_TTL", fileConfig.Limits.DefaultTTL); err != nil {
			return err
		}
		defer os.Unsetenv("ENVY_DEFAULT_TTL")
	}
	if os.Getenv("ENVY_MAX_TTL") == "" && fileConfig.Limits.MaxTTL != "" {
		if err = os.Setenv("ENVY_MAX_TTL", fileConfig.Limits.MaxTTL); err != nil {
			return err
		}
		defer os.Unsetenv("ENVY_MAX_TTL")
	}
	interval, err := duration("ENVY_RECONCILE_INTERVAL", time.Second)
	if err != nil {
		return err
	}
	provision, err := duration("ENVY_PROVISION_TIMEOUT", 60*time.Second)
	if err != nil {
		return err
	}
	drain, err := duration("ENVY_DRAIN_TIMEOUT", 10*time.Second)
	if err != nil {
		return err
	}
	defaultTTL, err := duration("ENVY_DEFAULT_TTL", 8*time.Hour)
	if err != nil {
		return err
	}
	maxTTL, err := duration("ENVY_MAX_TTL", 24*time.Hour)
	if err != nil {
		return err
	}
	if defaultTTL > maxTTL {
		return fmt.Errorf("default TTL exceeds maximum TTL")
	}
	maxCompositions, err := strconv.Atoi(configured("ENVY_MAX_COMPOSITIONS", fileConfig.Limits.MaxCompositions, "20"))
	if err != nil || maxCompositions < 1 {
		return fmt.Errorf("ENVY_MAX_COMPOSITIONS must be positive")
	}
	startup, done := context.WithTimeout(ctx, 30*time.Second)
	store, err := postgres.Open(startup, dbURL)
	if err != nil {
		done()
		return err
	}
	defer store.Close()
	if env("ENVY_MIGRATE_ON_START", "true") == "true" {
		if err = store.Migrate(startup); err != nil {
			done()
			return err
		}
	}
	auditRetention := configured("ENVY_AUDIT_RETENTION", fileConfig.Limits.AuditRetention, "retained")
	if auditRetention != "retained" {
		retention, e := time.ParseDuration(auditRetention)
		if e != nil || retention <= 0 {
			done()
			return fmt.Errorf("ENVY_AUDIT_RETENTION must be retained or a positive duration")
		}
		if err = store.PruneActivity(startup, time.Now().UTC().Add(-retention)); err != nil {
			done()
			return err
		}
	}
	if env("ENVY_SEED_DEMO", "false") == "true" {
		if err = store.SeedDemo(startup); err != nil {
			done()
			return err
		}
	}
	done()
	var kubeConfig *rest.Config
	if path := configured("KUBECONFIG", fileConfig.Kubeconfig, ""); path != "" {
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", path)
	} else {
		kubeConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return fmt.Errorf("load explicit Kubernetes configuration: %w", err)
	}
	kubeConfig.Timeout = 10 * time.Second
	kube, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	istio, err := istioclient.NewForConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("create Istio client: %w", err)
	}
	ingressURL := configured("ENVY_INGRESS_URL", fileConfig.Runtime.IngressURL, "http://istio-ingressgateway.istio-system.svc.cluster.local")
	baselineHost := configured("ENVY_BASELINE_HOST", fileConfig.Runtime.BaselineHost, "baseline.envy.localhost")
	previewBaseURL := configured("ENVY_PREVIEW_BASE_URL", fileConfig.Runtime.PreviewBaseURL, "http://envy.localhost:8080")
	var roots *x509.CertPool
	if path := configured("ENVY_INGRESS_CA_FILE", fileConfig.Runtime.IngressCAFile, ""); path != "" {
		pem, e := os.ReadFile(path)
		if e != nil {
			return fmt.Errorf("read ingress CA: %w", e)
		}
		roots, e = x509.SystemCertPool()
		if e != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return fmt.Errorf("ingress CA file contains no certificates")
		}
	}
	verifier, err := verification.NewWithRoots(ingressURL, baselineHost, nil, roots)
	if err != nil {
		return err
	}
	var sourceControl domain.SourceControl
	if appID, path := configured("ENVY_GITHUB_APP_ID", fileConfig.GitHub.AppID, ""), configured("ENVY_GITHUB_APP_PRIVATE_KEY_FILE", fileConfig.GitHub.PrivateKeyFile, ""); appID != "" || path != "" {
		key, e := os.ReadFile(path)
		if e != nil {
			return fmt.Errorf("read GitHub App private key: %w", e)
		}
		sourceControl, e = githubprovider.New(appID, key)
		if e != nil {
			return e
		}
	}
	var buildCredentials []api.BuildCredential
	if path := configured("ENVY_BUILD_CREDENTIALS_FILE", fileConfig.GitHub.BuildCredentialsFile, ""); path != "" {
		data, e := os.ReadFile(path)
		if e != nil {
			return fmt.Errorf("read build credentials: %w", e)
		}
		if json.Unmarshal(data, &buildCredentials) != nil {
			return fmt.Errorf("invalid build credentials JSON")
		}
		seen := map[string]bool{}
		for _, c := range buildCredentials {
			if len(c.Token) < 32 || c.Token == token || seen[c.Token] || !domain.ValidCatalogID(c.Project) || !domain.ValidCatalogID(c.Repository) || len(c.Components) == 0 {
				return fmt.Errorf("build credentials require unique tokens of at least 32 characters, project, repository, and components")
			}
			seen[c.Token] = true
			for _, component := range c.Components {
				if !domain.ValidCatalogID(component) {
					return fmt.Errorf("invalid CI component scope")
				}
			}
		}
	}
	var machineCredentials []api.MachineCredential
	if path := configured("ENVY_MACHINE_CREDENTIALS_FILE", fileConfig.Auth.MachineCredentialsFile, ""); path != "" {
		data, e := os.ReadFile(path)
		if e != nil {
			return fmt.Errorf("read machine credentials: %w", e)
		}
		if json.Unmarshal(data, &machineCredentials) != nil {
			return fmt.Errorf("invalid machine credentials JSON")
		}
		seen := map[string]bool{}
		for _, c := range machineCredentials {
			if len(c.Token) < 32 || strings.TrimSpace(c.ID) == "" || len(c.ID) > 200 || seen[c.Token] || c.Token == token {
				return fmt.Errorf("machine credentials require unique tokens of at least 32 characters and IDs")
			}
			seen[c.Token] = true
		}
	}
	if authMode == "token" && strings.TrimSpace(token) == "" && len(machineCredentials) == 0 {
		return fmt.Errorf("token mode requires a shared token or named machine credentials")
	}
	proxySecret := os.Getenv("ENVY_PROXY_SECRET")
	if path := configured("ENVY_PROXY_SECRET_FILE", fileConfig.Auth.ProxySecretFile, ""); path != "" {
		data, e := os.ReadFile(path)
		if e != nil {
			return fmt.Errorf("read proxy secret: %w", e)
		}
		proxySecret = strings.TrimSpace(string(data))
	}
	var trustedProxies []netip.Prefix
	for _, raw := range strings.Split(configured("ENVY_TRUSTED_PROXY_CIDRS", fileConfig.Auth.TrustedProxyCIDRs, "127.0.0.0/8,::1/128"), ",") {
		prefix, e := netip.ParsePrefix(strings.TrimSpace(raw))
		if e != nil {
			return fmt.Errorf("invalid ENVY_TRUSTED_PROXY_CIDRS")
		}
		trustedProxies = append(trustedProxies, prefix)
	}
	if authMode == "proxy" && (len(proxySecret) < 32 || len(trustedProxies) == 0) {
		return fmt.Errorf("proxy mode requires ENVY_PROXY_SECRET(_FILE) of at least 32 characters and trusted proxy CIDRs")
	}
	service := application.New(store, application.Config{ApprovedImagePullSecrets: fileConfig.ApprovedImagePullSecrets, SourceControl: sourceControl, ImageRegistry: registryprovider.Provider{}, CatalogValidator: application.BaselineChecks{kubeprovider.NewWithInjection(kube, installation, nil, fileConfig.Istio.InjectionLabels), istioprovider.NewWithIngressSelector(istio, installation, nil, fileConfig.Istio.IngressSelector), verifier}, Logs: kubeprovider.NewLogReader(kube, installation), DefaultTTL: defaultTTL, MaxTTL: maxTTL, MaxCompositions: maxCompositions, PreviewBaseURL: previewBaseURL})
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	auth := api.AuthConfig{Mode: authMode, SharedToken: token, MachineCredentials: machineCredentials, IdentityHeader: configured("ENVY_PROXY_IDENTITY_HEADER", fileConfig.Auth.IdentityHeader, "X-Envy-User"), EmailHeader: configured("ENVY_PROXY_EMAIL_HEADER", fileConfig.Auth.EmailHeader, "X-Envy-Email"), ExternalOrigin: configured("ENVY_EXTERNAL_ORIGIN", fileConfig.Auth.ExternalOrigin, ""), ProxySecret: proxySecret, TrustedProxies: trustedProxies}
	installationInfo := api.Installation{ID: installation, Version: "0.3.0", AuthMode: authMode, DefaultTTL: defaultTTL.String(), MaxTTL: maxTTL.String(), MaxCompositions: maxCompositions, AuditRetention: auditRetention, WebDir: configured("ENVY_WEB_DIR", fileConfig.WebDir, "")}
	server := &http.Server{Addr: configured("ENVY_LISTEN_ADDR", fileConfig.ListenAddr, ":8081"), Handler: api.NewConfiguredHandler(service, auth, installationInfo, store.Ping, buildCredentials), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		lead(ctx, store, kube, istio, verifier, installation, fileConfig.Istio.InjectionLabels, fileConfig.Istio.IngressSelector, fileConfig.ApprovedImagePullSecrets, reconciler.Config{Interval: interval, ProvisionTimeout: provision, DrainTimeout: drain})
	}()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	slog.Info("control plane listening", "address", server.Addr, "installation", installation)
	select {
	case <-ctx.Done():
	case err = <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	cancel()
	shutdown, finish := context.WithTimeout(context.Background(), 15*time.Second)
	defer finish()
	if shutdownErr := server.Shutdown(shutdown); shutdownErr != nil {
		slog.Warn("HTTP shutdown incomplete", "error", shutdownErr)
	}
	<-workerDone
	return err
}

func lead(ctx context.Context, store *postgres.Store, kube kubernetes.Interface, istio istioclient.Interface, verifier *verification.Demo, installation string, injectionLabels, ingressSelector map[string]string, approvedPullSecrets []string, cfg reconciler.Config) {
	for ctx.Err() == nil {
		acquire, cancel := context.WithTimeout(ctx, 5*time.Second)
		lease, err := store.AcquireLease(acquire)
		cancel()
		if err == nil {
			slog.Info("reconciler leadership acquired")
			runCtx, stop := context.WithCancel(ctx)
			guard := func(callCtx context.Context) error {
				check, done := context.WithTimeout(callCtx, 3*time.Second)
				defer done()
				if err := lease.Check(check); err != nil {
					stop()
					return err
				}
				return runCtx.Err()
			}
			monitorDone := make(chan struct{})
			go func() {
				defer close(monitorDone)
				timer := time.NewTicker(time.Second)
				defer timer.Stop()
				for {
					select {
					case <-runCtx.Done():
						return
					case <-timer.C:
						if guard(runCtx) != nil {
							return
						}
					}
				}
			}()
			worker := reconciler.New(store, kubeprovider.NewWithInjection(kube, installation, guard, injectionLabels).WithApprovedPullSecrets(approvedPullSecrets), istioprovider.NewWithIngressSelector(istio, installation, guard, ingressSelector), verifier, guard, slog.Default(), cfg)
			err = worker.Run(runCtx)
			stop()
			<-monitorDone
			closeCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
			closeErr := lease.Close(closeCtx)
			done()
			if closeErr != nil {
				slog.Warn("release leadership", "error", closeErr)
			}
			if ctx.Err() == nil {
				slog.Warn("reconciler restarting", "error", err)
			}
		} else if !errors.Is(err, domain.ErrNotLeader) && ctx.Err() == nil {
			slog.Warn("reconciler lease unavailable", "error", err)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

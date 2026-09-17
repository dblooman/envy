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
	"github.com/dblooman/envy/internal/authn"
	"github.com/dblooman/envy/internal/buildinfo"
	"github.com/dblooman/envy/internal/domain"
	"github.com/dblooman/envy/internal/mesh"
	"github.com/dblooman/envy/internal/persistence/postgres"
	ciliumprovider "github.com/dblooman/envy/internal/providers/cilium"
	githubprovider "github.com/dblooman/envy/internal/providers/github"
	istioprovider "github.com/dblooman/envy/internal/providers/istio"
	kubeprovider "github.com/dblooman/envy/internal/providers/kubernetes"
	linkerdprovider "github.com/dblooman/envy/internal/providers/linkerd"
	pubsubprovider "github.com/dblooman/envy/internal/providers/pubsub"
	registryprovider "github.com/dblooman/envy/internal/providers/registry"
	"github.com/dblooman/envy/internal/reconciler"
	"github.com/dblooman/envy/internal/verification"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	istioclient "istio.io/client-go/pkg/clientset/versioned"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
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

type serverSettings struct {
	fileConfig      serverFileConfig
	profile         mesh.Profile
	ingressURL      string
	token           string
	loginConfig     authn.Config
	installation    string
	interval        time.Duration
	provision       time.Duration
	drain           time.Duration
	defaultTTL      time.Duration
	maxTTL          time.Duration
	maxCompositions int
}

func loadServerSettings() (serverSettings, func(), error) {
	var settings serverSettings
	config, err := loadServerConfig(os.Getenv("ENVY_CONFIG_FILE"))
	if err != nil {
		return settings, nil, err
	}

	profile, err := mesh.Resolve(configured("ENVY_MESH_PROVIDER", config.Mesh.Provider, ""))
	if err != nil {
		return settings, nil, err
	}

	for _, key := range []string{"ENVY_CILIUM_NATIVE_CEC", "ENVY_LINKERD_INJECT"} {
		if _, set := os.LookupEnv(key); set {
			return settings, nil, fmt.Errorf("%s is retired; select mesh.provider=cilium or linkerd", key)
		}
	}

	ingressURL, err := profile.IngressURL(configured("ENVY_INGRESS_URL", config.Runtime.IngressURL, ""))
	if err != nil {
		return settings, nil, err
	}

	token, err := loadToken(config)
	if err != nil {
		return settings, nil, err
	}

	loginCfg, err := loginConfig(config)
	if err != nil {
		return settings, nil, err
	}

	cleanup, err := applyTTLLimits(config)
	if err != nil {
		return settings, nil, err
	}

	settings = serverSettings{
		fileConfig: config, profile: profile, ingressURL: ingressURL, token: token, loginConfig: loginCfg,
		installation: configured("ENVY_INSTALLATION_ID", config.InstallationID, "envy-local"),
	}
	for _, item := range []struct {
		key         string
		fallback    time.Duration
		destination *time.Duration
	}{
		{"ENVY_RECONCILE_INTERVAL", time.Second, &settings.interval},
		{"ENVY_PROVISION_TIMEOUT", 60 * time.Second, &settings.provision},
		{"ENVY_DRAIN_TIMEOUT", 10 * time.Second, &settings.drain},
		{"ENVY_DEFAULT_TTL", 8 * time.Hour, &settings.defaultTTL},
		{"ENVY_MAX_TTL", 24 * time.Hour, &settings.maxTTL},
	} {
		*item.destination, err = duration(item.key, item.fallback)
		if err != nil {
			cleanup()
			return serverSettings{}, nil, err
		}
	}

	if settings.defaultTTL > settings.maxTTL {
		cleanup()
		return serverSettings{}, nil, fmt.Errorf("default TTL exceeds maximum TTL")
	}

	settings.maxCompositions, err = strconv.Atoi(configured("ENVY_MAX_COMPOSITIONS", config.Limits.MaxCompositions, "20"))
	if err != nil || settings.maxCompositions < 1 {
		cleanup()
		return serverSettings{}, nil, fmt.Errorf("ENVY_MAX_COMPOSITIONS must be positive")
	}

	return settings, cleanup, nil
}

func loadToken(config serverFileConfig) (string, error) {
	token := os.Getenv("ENVY_API_TOKEN")
	path := configured("ENVY_API_TOKEN_FILE", config.Auth.APITokenFile, "")
	if path == "" {
		return token, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read API token file: %w", err)
	}

	return strings.TrimSpace(string(b)), nil
}

func applyTTLLimits(config serverFileConfig) (func(), error) {
	var keys []string
	for _, item := range []struct{ key, value string }{
		{"ENVY_DEFAULT_TTL", config.Limits.DefaultTTL},
		{"ENVY_MAX_TTL", config.Limits.MaxTTL},
	} {
		if os.Getenv(item.key) == "" && item.value != "" {
			if err := os.Setenv(item.key, item.value); err != nil {
				for _, key := range keys {
					_ = os.Unsetenv(key)
				}

				return nil, err
			}

			keys = append(keys, item.key)
		}
	}

	return func() {
		for _, key := range keys {
			_ = os.Unsetenv(key)
		}
	}, nil
}

func loadAPIAuthConfig(config serverFileConfig, mode, sharedToken string) (api.AuthConfig, error) {
	machineCredentials, err := loadMachineCredentials(config, sharedToken)
	if err != nil {
		return api.AuthConfig{}, err
	}

	if mode == "token" && strings.TrimSpace(sharedToken) == "" && len(machineCredentials) == 0 {
		return api.AuthConfig{}, fmt.Errorf("token mode requires a shared token or named machine credentials")
	}

	proxySecret, err := loadProxySecret(config)
	if err != nil {
		return api.AuthConfig{}, err
	}

	trustedProxies, err := loadTrustedProxies(config)
	if err != nil {
		return api.AuthConfig{}, err
	}

	if mode == "proxy" && (len(proxySecret) < 32 || len(trustedProxies) == 0) {
		return api.AuthConfig{}, fmt.Errorf("proxy mode requires ENVY_PROXY_SECRET(_FILE) of at least 32 characters and trusted proxy CIDRs")
	}

	return api.AuthConfig{
		Mode: mode, SharedToken: sharedToken, MachineCredentials: machineCredentials,
		IdentityHeader: configured("ENVY_PROXY_IDENTITY_HEADER", config.Auth.IdentityHeader, "X-Envy-User"),
		EmailHeader:    configured("ENVY_PROXY_EMAIL_HEADER", config.Auth.EmailHeader, "X-Envy-Email"),
		ExternalOrigin: configured("ENVY_EXTERNAL_ORIGIN", config.Auth.ExternalOrigin, ""),
		ProxySecret:    proxySecret, TrustedProxies: trustedProxies,
	}, nil
}

func loadMachineCredentials(config serverFileConfig, sharedToken string) ([]api.MachineCredential, error) {
	path := configured("ENVY_MACHINE_CREDENTIALS_FILE", config.Auth.MachineCredentialsFile, "")
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read machine credentials: %w", err)
	}

	var credentials []api.MachineCredential
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, fmt.Errorf("invalid machine credentials JSON")
	}

	seen := map[string]bool{}
	for _, credential := range credentials {
		if len(credential.Token) < 32 || strings.TrimSpace(credential.ID) == "" || len(credential.ID) > 200 || seen[credential.Token] || credential.Token == sharedToken {
			return nil, fmt.Errorf("machine credentials require unique tokens of at least 32 characters and IDs")
		}

		seen[credential.Token] = true
	}

	return credentials, nil
}

func loadBuildCredentials(config serverFileConfig, sharedToken string) ([]api.BuildCredential, error) {
	path := configured("ENVY_BUILD_CREDENTIALS_FILE", config.GitHub.BuildCredentialsFile, "")
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read build credentials: %w", err)
	}

	var credentials []api.BuildCredential
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, fmt.Errorf("invalid build credentials JSON")
	}

	seen := map[string]bool{}
	for _, credential := range credentials {
		if len(credential.Token) < 32 || credential.Token == sharedToken || seen[credential.Token] || !domain.ValidCatalogID(credential.Project) || !domain.ValidCatalogID(credential.Repository) || len(credential.Components) == 0 {
			return nil, fmt.Errorf("build credentials require unique tokens of at least 32 characters, project, repository, and components")
		}

		seen[credential.Token] = true
		for _, component := range credential.Components {
			if !domain.ValidCatalogID(component) {
				return nil, fmt.Errorf("invalid CI component scope")
			}
		}
	}

	return credentials, nil
}

func newVerifier(config serverFileConfig, ingressURL, baselineHost string) (*verification.Demo, error) {
	roots, err := loadIngressRoots(config)
	if err != nil {
		return nil, err
	}

	return verification.NewWithRoots(ingressURL, baselineHost, nil, roots)
}

func loadIngressRoots(config serverFileConfig) (*x509.CertPool, error) {
	path := configured("ENVY_INGRESS_CA_FILE", config.Runtime.IngressCAFile, "")
	if path == "" {
		return nil, nil
	}

	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ingress CA: %w", err)
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}

	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("ingress CA file contains no certificates")
	}

	return roots, nil
}

func loadSourceControl(config serverFileConfig) (domain.SourceControl, error) {
	appID := configured("ENVY_GITHUB_APP_ID", config.GitHub.AppID, "")
	path := configured("ENVY_GITHUB_APP_PRIVATE_KEY_FILE", config.GitHub.PrivateKeyFile, "")
	if appID == "" && path == "" {
		return nil, nil
	}

	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App private key: %w", err)
	}

	return githubprovider.New(appID, key)
}

func configurePreviewPolicy(config *serverFileConfig) error {
	config.Preview.ControllerNamespace = configured("ENVY_PREVIEW_CONTROLLER_NAMESPACE", config.Preview.ControllerNamespace, "")
	config.Preview.ControllerServiceAccount = configured("ENVY_PREVIEW_CONTROLLER_SERVICE_ACCOUNT", config.Preview.ControllerServiceAccount, "")
	config.Preview.DependencyClusterRole = configured("ENVY_PREVIEW_DEPENDENCY_CLUSTER_ROLE", config.Preview.DependencyClusterRole, "")
	return config.Preview.Validate()
}

func newMessagingProvider(ctx context.Context, config serverFileConfig, installation string) (domain.MessagingProvider, error) {
	enabled, err := strconv.ParseBool(configured("ENVY_PUBSUB_ENABLED", strconv.FormatBool(config.PubSubEnabled), "false"))
	if err != nil {
		return nil, fmt.Errorf("ENVY_PUBSUB_ENABLED must be true or false")
	}

	if !enabled {
		return nil, nil
	}

	return pubsubprovider.New(ctx, installation, nil)
}

func loadProxySecret(config serverFileConfig) (string, error) {
	secret := os.Getenv("ENVY_PROXY_SECRET")
	path := configured("ENVY_PROXY_SECRET_FILE", config.Auth.ProxySecretFile, "")
	if path == "" {
		return secret, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read proxy secret: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

func loadTrustedProxies(config serverFileConfig) ([]netip.Prefix, error) {
	values := configured("ENVY_TRUSTED_PROXY_CIDRS", config.Auth.TrustedProxyCIDRs, "127.0.0.0/8,::1/128")
	var prefixes []netip.Prefix
	for raw := range strings.SplitSeq(values, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid ENVY_TRUSTED_PROXY_CIDRS")
		}

		prefixes = append(prefixes, prefix)
	}

	return prefixes, nil
}

func run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	settings, cleanup, err := loadServerSettings()
	if err != nil {
		return err
	}
	defer cleanup()
	fileConfig, profile := settings.fileConfig, settings.profile
	ingressURL, token, loginCfg := settings.ingressURL, settings.token, settings.loginConfig
	authMode := settings.loginConfig.Mode

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	installation := settings.installation
	interval, provision, drain := settings.interval, settings.provision, settings.drain
	defaultTTL, maxTTL, maxCompositions := settings.defaultTTL, settings.maxTTL, settings.maxCompositions
	store, auditRetention, err := initializeStore(ctx, dbURL, installation, profile.Name, fileConfig)
	if err != nil {
		return err
	}
	defer store.Close()
	kubeConfig, kube, err := newKubernetesClient(fileConfig)
	if err != nil {
		return err
	}

	providers, err := newProviders(kubeConfig, kube, fileConfig, profile, installation)
	if err != nil {
		return err
	}

	routeValidator, kubeValidator := providers.routeValidator, providers.kubeValidator
	runtimeFactory, routeFactory := providers.runtimeFactory, providers.routeFactory
	baselineHost := configured("ENVY_BASELINE_HOST", fileConfig.Runtime.BaselineHost, "baseline.envy.localhost")
	previewBaseURL := configured("ENVY_PREVIEW_BASE_URL", fileConfig.Runtime.PreviewBaseURL, "http://envy.localhost:8080")
	verifier, err := newVerifier(fileConfig, ingressURL, baselineHost)
	if err != nil {
		return err
	}

	sourceControl, err := loadSourceControl(fileConfig)
	if err != nil {
		return err
	}

	buildCredentials, err := loadBuildCredentials(fileConfig, token)
	if err != nil {
		return err
	}

	auth, err := loadAPIAuthConfig(fileConfig, authMode, token)
	if err != nil {
		return err
	}

	if err := configurePreviewPolicy(&fileConfig); err != nil {
		return err
	}

	// Deployment-derived templates currently support platform Istio injection.
	var previewDiscoverer domain.PreviewDiscoverer
	if profile.Name == "istio" {
		previewDiscoverer = kubeprovider.NewWithInjection(kube, installation, nil, fileConfig.Istio.InjectionLabels).WithPreviewPolicy(fileConfig.Preview)
	}

	messaging, err := newMessagingProvider(ctx, fileConfig, installation)
	if err != nil {
		return err
	}

	webhookSecret := ""
	if path := configured("ENVY_GITHUB_WEBHOOK_SECRET_FILE", fileConfig.GitHub.WebhookSecretFile, ""); path != "" {
		data, e := os.ReadFile(path)
		if e != nil {
			return fmt.Errorf("read GitHub webhook secret: %w", e)
		}

		webhookSecret = strings.TrimSpace(string(data))
		if len(webhookSecret) < 32 {
			return fmt.Errorf("GitHub webhook secret must contain at least 32 characters")
		}
	}

	service := application.New(store, application.Config{GitHubWebhookSecret: webhookSecret, Messaging: messaging, Installation: installation, PreviewDiscoverer: previewDiscoverer, ApprovedImagePullSecrets: fileConfig.ApprovedImagePullSecrets, SourceControl: sourceControl, ImageRegistry: registryprovider.Provider{}, CatalogValidator: application.BaselineChecks{kubeValidator, routeValidator, verifier}, Logs: kubeprovider.NewLogReader(kube, installation), DefaultTTL: defaultTTL, MaxTTL: maxTTL, MaxCompositions: maxCompositions, PreviewBaseURL: previewBaseURL})
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	login, err := authn.New(ctx, loginCfg, store.AuthPool())
	if err != nil {
		return err
	}

	auth.Login = login
	installationInfo := api.Installation{ID: installation, Version: buildinfo.Version, AuthMode: authMode, DefaultTTL: defaultTTL.String(), MaxTTL: maxTTL.String(), MaxCompositions: maxCompositions, AuditRetention: auditRetention, WebDir: configured("ENVY_WEB_DIR", fileConfig.WebDir, "")}
	server := &http.Server{Addr: configured("ENVY_LISTEN_ADDR", fileConfig.ListenAddr, ":8081"), Handler: api.NewConfiguredHandler(service, auth, installationInfo, store.Ping, buildCredentials), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	return serve(ctx, cancel, server, installation, func() {
		lead(ctx, store, runtimeFactory, routeFactory, verifier, reconciler.Config{Messaging: messaging, Interval: interval, ProvisionTimeout: provision, DrainTimeout: drain}, service.RunGitHub)
	})
}

func serve(ctx context.Context, cancel context.CancelFunc, server *http.Server, installation string, worker func()) error {
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker()
	}()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	slog.Info("control plane listening", "address", server.Addr, "installation", installation)
	var err error
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

type (
	runtimeFactoryFunc func(guard func(context.Context) error) reconciler.Runtime
	routeFactoryFunc   func(guard func(context.Context) error) domain.RoutingProvider
)

type providerSet struct {
	routeValidator domain.CatalogValidator
	kubeValidator  domain.CatalogValidator
	runtimeFactory runtimeFactoryFunc
	routeFactory   routeFactoryFunc
}

func initializeStore(ctx context.Context, dbURL, installation, meshName string, config serverFileConfig) (*postgres.Store, string, error) {
	startup, done := context.WithTimeout(ctx, 30*time.Second)
	defer done()
	store, err := postgres.Open(startup, dbURL)
	if err != nil {
		return nil, "", err
	}

	if env("ENVY_MIGRATE_ON_START", "true") == "true" {
		if err := store.Migrate(startup); err != nil {
			store.Close()
			return nil, "", err
		}
	}

	if err := store.BindInstallation(startup, installation, meshName); err != nil {
		store.Close()
		return nil, "", err
	}

	auditRetention := configured("ENVY_AUDIT_RETENTION", config.Limits.AuditRetention, "retained")
	if auditRetention != "retained" {
		retention, err := time.ParseDuration(auditRetention)
		if err != nil || retention <= 0 {
			store.Close()
			return nil, "", fmt.Errorf("ENVY_AUDIT_RETENTION must be retained or a positive duration")
		}

		if err := store.PruneActivity(startup, time.Now().UTC().Add(-retention)); err != nil {
			store.Close()
			return nil, "", err
		}
	}

	if env("ENVY_SEED_DEMO", "false") == "true" {
		if err := store.SeedDemo(startup); err != nil {
			store.Close()
			return nil, "", err
		}
	}

	return store, auditRetention, nil
}

func newKubernetesClient(config serverFileConfig) (*rest.Config, kubernetes.Interface, error) {
	kubeConfig, err := loadKubernetesConfig(config)
	if err != nil {
		return nil, nil, err
	}

	kube, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	return kubeConfig, kube, nil
}

func newProviders(kubeConfig *rest.Config, kube kubernetes.Interface, config serverFileConfig, profile mesh.Profile, installation string) (providerSet, error) {
	// Each closure constructs a fresh provider bound to the reconciler's lease guard.
	set := providerSet{}
	switch profile.Name {
	case "cilium", "linkerd":
		return newGatewayProviders(kubeConfig, kube, config, profile, installation)
	default:
		istio, err := istioclient.NewForConfig(kubeConfig)
		if err != nil {
			return set, fmt.Errorf("create Istio client: %w", err)
		}

		set.routeValidator = istioprovider.NewWithIngressSelector(istio, installation, nil, config.Istio.IngressSelector)
		set.kubeValidator = kubeprovider.NewWithInjection(kube, installation, nil, config.Istio.InjectionLabels)
		set.runtimeFactory = func(guard func(context.Context) error) reconciler.Runtime {
			return kubeprovider.NewWithInjection(kube, installation, guard, config.Istio.InjectionLabels).WithApprovedPullSecrets(config.ApprovedImagePullSecrets).WithPreviewPolicy(config.Preview)
		}
		set.routeFactory = func(guard func(context.Context) error) domain.RoutingProvider {
			return istioprovider.NewWithIngressSelector(istio, installation, guard, config.Istio.IngressSelector)
		}
		return set, nil
	}
}

func loadKubernetesConfig(config serverFileConfig) (*rest.Config, error) {
	var (
		kubeConfig *rest.Config
		err        error
	)
	if path := configured("KUBECONFIG", config.Kubeconfig, ""); path != "" {
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", path)
	} else {
		kubeConfig, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, fmt.Errorf("load explicit Kubernetes configuration: %w", err)
	}

	kubeConfig.Timeout = 10 * time.Second
	return kubeConfig, nil
}

func newGatewayProviders(kubeConfig *rest.Config, kube kubernetes.Interface, config serverFileConfig, profile mesh.Profile, installation string) (providerSet, error) {
	set := providerSet{}
	gwClient, err := gatewayclient.NewForConfig(kubeConfig)
	if err != nil {
		return set, err
	}

	dyn, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return set, err
	}

	gwClass := configured("ENVY_GATEWAY_CLASS", config.GatewayAPI.GatewayClass, profile.GatewayClass)
	makeRuntime := func(guard func(context.Context) error) *kubeprovider.Provider {
		p := kubeprovider.NewWithInjection(kube, installation, guard, map[string]string{}).WithMesh(profile.Name)
		return p.WithApprovedPullSecrets(config.ApprovedImagePullSecrets).WithPreviewPolicy(config.Preview)
	}
	set.kubeValidator = makeRuntime(nil)
	set.runtimeFactory = func(guard func(context.Context) error) reconciler.Runtime { return makeRuntime(guard) }
	if profile.Name == "cilium" {
		set.routeValidator = ciliumprovider.New(gwClient, kube, installation, nil, gwClass).WithEndpointClient(dyn)
		set.routeFactory = func(guard func(context.Context) error) domain.RoutingProvider {
			return ciliumprovider.New(gwClient, kube, installation, guard, gwClass).WithEndpointClient(dyn)
		}
	} else {
		set.routeValidator = linkerdprovider.New(gwClient, kube, dyn, installation, nil, gwClass)
		set.routeFactory = func(guard func(context.Context) error) domain.RoutingProvider {
			return linkerdprovider.New(gwClient, kube, dyn, installation, guard, gwClass)
		}
	}

	return set, nil
}

func lead(ctx context.Context, store *postgres.Store, runtimeFactory runtimeFactoryFunc, routeFactory routeFactoryFunc, verifier *verification.Demo, cfg reconciler.Config, extra ...func(context.Context, func(context.Context) error)) {
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
			go monitorLeadership(runCtx, guard, monitorDone)
			workerConfig := cfg
			if provider, ok := cfg.Messaging.(*pubsubprovider.Provider); ok {
				workerConfig.Messaging = provider.WithGuard(guard)
			}

			worker := reconciler.New(store, runtimeFactory(guard), routeFactory(guard), verifier, guard, slog.Default(), workerConfig)
			extraDone := make(chan struct{})
			go func() {
				defer close(extraDone)
				for _, run := range extra {
					run(runCtx, guard)
				}
			}()
			err = worker.Run(runCtx)
			stop()
			<-extraDone
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

func monitorLeadership(ctx context.Context, guard func(context.Context) error, done chan<- struct{}) {
	defer close(done)
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if guard(ctx) != nil {
				return
			}
		}
	}
}

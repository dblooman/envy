// Command server runs Envy's HTTP control plane and its single active reconciler.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	istioprovider "github.com/dblooman/envy/internal/providers/istio"
	kubeprovider "github.com/dblooman/envy/internal/providers/kubernetes"
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
	if err := run(ctx); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
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
	token := os.Getenv("ENVY_API_TOKEN")
	if path := os.Getenv("ENVY_API_TOKEN_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read API token file: %w", err)
		}
		token = strings.TrimSpace(string(b))
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("ENVY_API_TOKEN_FILE or ENVY_API_TOKEN is required")
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	installation := env("ENVY_INSTALLATION_ID", "envy-local")
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
	maxCompositions, err := strconv.Atoi(env("ENVY_MAX_COMPOSITIONS", "20"))
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
	if err = store.Migrate(startup); err != nil {
		done()
		return err
	}
	if env("ENVY_SEED_DEMO", "false") == "true" {
		if err = store.SeedDemo(startup); err != nil {
			done()
			return err
		}
	}
	done()
	var kubeConfig *rest.Config
	if path := os.Getenv("KUBECONFIG"); path != "" {
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
	verifier, err := verification.New(env("ENVY_INGRESS_URL", "http://istio-ingressgateway.istio-system.svc.cluster.local"), env("ENVY_BASELINE_HOST", "baseline.envy.localhost"), nil)
	if err != nil {
		return err
	}
	service := application.New(store, application.Config{Logs: kubeprovider.NewLogReader(kube, installation), DefaultTTL: defaultTTL, MaxTTL: maxTTL, MaxCompositions: maxCompositions, PreviewBaseURL: env("ENVY_PREVIEW_BASE_URL", "http://envy.localhost:8080")})
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	server := &http.Server{Addr: env("ENVY_LISTEN_ADDR", ":8081"), Handler: api.NewHandler(service, token, store.Ping), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		lead(ctx, store, kube, istio, verifier, installation, reconciler.Config{Interval: interval, ProvisionTimeout: provision, DrainTimeout: drain})
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

func lead(ctx context.Context, store *postgres.Store, kube kubernetes.Interface, istio istioclient.Interface, verifier *verification.Demo, installation string, cfg reconciler.Config) {
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
			worker := reconciler.New(store, kubeprovider.New(kube, installation, guard), istioprovider.New(istio, installation, guard), verifier, guard, slog.Default(), cfg)
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

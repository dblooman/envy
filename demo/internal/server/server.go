package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dblooman/envy/demo/protocol"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
)

// Version is set at build time. No request or deployment setting can override it.
var Version = "v1"

func Handler(service, downstream, workload, deploymentComposition string) http.Handler {
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	client := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithPropagators(prop)), Timeout: 5 * time.Second}
	mux := http.NewServeMux()
	for _, path := range []string{"GET /healthz", "GET /readyz"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	}

	mux.Handle("GET /{$}", otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := protocol.Response{Chain: []protocol.Hop{{Service: service, Version: Version, Composition: baggage.FromContext(r.Context()).Member("composition").Value(), WorkloadID: workload, DeploymentComposition: deploymentComposition}}}
		if downstream != "" {
			req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, downstream, nil)
			if err != nil {
				http.Error(w, "invalid downstream", http.StatusBadGateway)
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				slog.Warn("downstream unavailable", "service", service, "error", err)
				http.Error(w, "downstream unavailable", http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				http.Error(w, "downstream unhealthy", http.StatusBadGateway)
				return
			}

			var child protocol.Response
			if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&child); err != nil {
				http.Error(w, "invalid downstream response", http.StatusBadGateway)
				return
			}

			result.Chain = append(result.Chain, child.Chain...)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}), service, otelhttp.WithPropagators(prop)))
	return mux
}

func Run(service string) error {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	workload := os.Getenv("POD_UID")
	if workload == "" {
		workload, _ = os.Hostname()
	}

	composition := os.Getenv("ENVY_COMPOSITION_ID")
	if composition == "" {
		composition = "baseline"
	}

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	s := &http.Server{Addr: addr, Handler: Handler(service, os.Getenv("DOWNSTREAM_URL"), workload, composition), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 6 * time.Second, WriteTimeout: 8 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = s.Shutdown(shutdown)
	}()
	slog.Info("demo listening", "service", service, "version", Version, "address", addr)
	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve %s: %w", service, err)
	}

	return nil
}

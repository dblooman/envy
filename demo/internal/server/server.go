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
	"strings"
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

	offer := func() *protocol.Offer {
		if service != "service-a" {
			return nil
		}
		if Version == "v1" {
			return &protocol.Offer{
				Name:        "Starter analytics",
				Price:       "$49 / month",
				Description: "A clear daily snapshot for a growing product team.",
				Service:     service,
				Version:     Version,
			}
		}
		return &protocol.Offer{
			Name:        "Growth analytics",
			Price:       "$79 / month",
			Description: "Adds conversion cohorts and a weekly experiment review.",
			Service:     service,
			Version:     Version,
		}
	}

	respond := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := protocol.Response{Chain: []protocol.Hop{{Service: service, Version: Version, Composition: baggage.FromContext(r.Context()).Member("composition").Value(), WorkloadID: workload, DeploymentComposition: deploymentComposition}}}
		result.Offer = offer()
		if downstream != "" {
			req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(downstream, "/")+"/api/offer", nil)
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
			if child.Offer != nil {
				result.Offer = child.Offer
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}), service, otelhttp.WithPropagators(prop))
	mux.Handle("GET /api/offer", respond)
	if service != "gateway" {
		mux.Handle("GET /{$}", respond)
		return mux
	}
	mux.Handle("GET /{$}", respond)
	mux.HandleFunc("GET /app", storefront)
	return mux
}

func storefront(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Signal Market</title>
  <style>
    :root { color-scheme: dark; font-family: Georgia, "Times New Roman", serif; background: #101817; color: #f4ead8; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: radial-gradient(circle at top right, #315850, transparent 42%), #101817; }
    main { width: min(920px, calc(100% - 2rem)); padding: 4rem 0; }
    .eyebrow { color: #b5d7bd; font: 700 .75rem/1.2 ui-monospace, monospace; letter-spacing: .16em; text-transform: uppercase; }
    h1 { max-width: 11ch; margin: .5rem 0 2rem; font-size: clamp(3rem, 9vw, 6.5rem); line-height: .88; letter-spacing: -.06em; }
    .offer { min-height: 236px; padding: 2rem; border: 1px solid #6a8d82; border-radius: 1.25rem; background: rgba(16, 24, 23, .78); box-shadow: 14px 14px 0 #d6a746; }
    .offer h2 { margin: .75rem 0; font-size: clamp(2rem, 5vw, 4rem); line-height: .95; }
    .offer p { max-width: 52ch; color: #d1d6ca; font-family: ui-sans-serif, system-ui, sans-serif; font-size: 1.05rem; }
    .price { color: #f2c659; font: 700 1.3rem/1 ui-monospace, monospace; }
    .meta { margin-top: 2rem; color: #a6bab2; font: .8rem/1.5 ui-monospace, monospace; }
    .error { color: #ffb4a8; }
  </style>
</head>
<body>
  <main>
    <div class="eyebrow">Envy preview routing demo</div>
    <h1>Make the middle count.</h1>
    <section class="offer" aria-live="polite" id="offer"><div class="eyebrow">Loading offer</div></section>
  </main>
  <script>
    const offer = document.querySelector("#offer");
    fetch("/api/offer").then(response => {
      if (!response.ok) throw new Error("Offer service unavailable");
      return response.json();
    }).then(data => {
      offer.innerHTML = '<div class="eyebrow">Selected by ' + data.offer.service + ' · ' + data.offer.version + '</div>' +
        '<h2>' + data.offer.name + '</h2><div class="price">' + data.offer.price + '</div>' +
        '<p>' + data.offer.description + '</p><div class="meta">' +
        data.chain.map(hop => hop.service + ' ' + hop.version + ' [' + hop.deployment_composition + ']').join(' → ') + '</div>';
    }).catch(error => {
      offer.innerHTML = '<p class="error">' + error.message + '</p>';
    });
  </script>
</body>
</html>`)
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

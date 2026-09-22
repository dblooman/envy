// Shop is an ordinary HTTP application. Envy does not parse its response schema.
package main

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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
)

var version = "v1"

func main() {
	role := os.Getenv("SHOP_ROLE")
	if role != "storefront" && role != "pricing" {
		slog.Error("SHOP_ROLE must be storefront or pricing")
		os.Exit(1)
	}

	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	client := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithPropagators(prop)), Timeout: 5 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/products", http.StatusFound) })
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.Handle("GET /products", otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Diagnostic headers are application-owned. Only the external acceptance
		// test interprets them; the control plane checks HTTP status alone.
		w.Header().Set("X-Shop-"+role+"-Workload", os.Getenv("POD_UID"))
		w.Header().Set("X-Shop-"+role+"-Context", baggage.FromContext(r.Context()).Member("composition").Value())
		w.Header().Set("Content-Type", "application/json")
		if role == "pricing" {
			price := 1200
			if version == "v2" {
				price = 990
			}

			_ = json.NewEncoder(w).Encode(map[string]any{"sku": "tea-001", "name": "Breakfast tea", "price_minor": price, "currency": "GBP", "release": version})
			return
		}

		req, err := http.NewRequestWithContext(r.Context(), "GET", os.Getenv("DOWNSTREAM_URL"), nil)
		if err != nil {
			http.Error(w, "pricing URL invalid", 502)
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "pricing unavailable", 502)
			return
		}
		defer resp.Body.Close()
		for _, header := range []string{"X-Shop-Pricing-Workload", "X-Shop-Pricing-Context"} {
			w.Header().Set(header, resp.Header.Get(header))
		}

		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 64<<10))
	}), "shop-"+role, otelhttp.WithPropagators(prop)))
	server := &http.Server{Addr: ":8080", Handler: browserCORS(mux), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 6 * time.Second, WriteTimeout: 8 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		c, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = server.Shutdown(c)
	}()
	slog.Info("shop listening", "role", role, "version", version)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// The example accepts one local demo origin. Configure the application's own
// origin policy for a real hosted frontend; this is not authentication.
func browserCORS(next http.Handler) http.Handler {
	allowed := os.Getenv("SHOP_ALLOWED_ORIGIN")
	if allowed == "" {
		allowed = "http://localhost:4174"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		if r.Header.Get("Origin") == allowed {
			w.Header().Set("Access-Control-Allow-Origin", allowed)
			w.Header().Set("Access-Control-Expose-Headers", "X-Shop-Storefront-Context, X-Shop-Pricing-Context, X-Shop-Storefront-Workload, X-Shop-Pricing-Workload")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") == "GET" && r.Header.Get("Access-Control-Request-Headers") == "" {
				w.Header().Set("Access-Control-Allow-Methods", "GET")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

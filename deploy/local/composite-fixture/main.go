// The composite acceptance helper has no cloud dependencies. One immutable
// image supplies an ordinary init, two restartable inits, a regular proxy,
// and a shared HTTP dependency.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const work = "/fixture-work"

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "check-dependency" {
		response, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://127.0.0.1:8083/readyz")
		if err != nil {
			return fmt.Errorf("dependency request failed: %w", err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
		if err != nil || response.StatusCode != http.StatusOK || string(body) != "synthetic-shared-dependency" {
			return fmt.Errorf("dependency response is unavailable or unexpected")
		}
		log.Print("shared dependency connected")
		return nil
	}
	if len(os.Args) != 2 {
		return fmt.Errorf("expected init, native, proxy, fail-native, or repair-native")
	}

	mode := os.Args[1]
	marker := filepath.Join(work, "fail-native")
	if mode == "fail-native" {
		return os.WriteFile(marker, []byte("synthetic failure"), 0o600)
	}

	if mode == "repair-native" {
		return os.Remove(marker)
	}

	// Different per-container references are required for successful startup.
	// Values are deliberately never logged.
	if os.Getenv("FIXTURE_CONFIG") != mode+"-config" || os.Getenv("FIXTURE_SECRET") != "synthetic-opaque-"+mode+"-payload" {
		return fmt.Errorf("%s fixture dependency references are missing or incorrect", mode)
	}

	if mode == "upstream" {
		return serve(mode, marker)
	}
	if mode == "init" {
		if err := os.WriteFile(filepath.Join(work, "initialized"), []byte("ready"), 0o600); err != nil {
			return err
		}

		log.Print("bootstrap completed")
		return nil
	}

	if mode != "native" && mode != "proxy" && mode != "dependency" {
		return fmt.Errorf("unsupported fixture mode")
	}

	if _, err := os.Stat(filepath.Join(work, "initialized")); err != nil {
		return fmt.Errorf("bootstrap did not complete: %w", err)
	}

	return serve(mode, marker)
}

func serve(mode, marker string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	mux := http.NewServeMux()
	addr := ":8082"
	if mode == "upstream" {
		addr = ":8084"
		mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "synthetic-shared-dependency") })
	} else if mode == "dependency" {
		addr = ":8083"
		target, err := url.Parse(os.Getenv("FIXTURE_UPSTREAM"))
		if err != nil || target.Host == "" || target.Scheme != "http" {
			return fmt.Errorf("invalid synthetic upstream")
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.Transport = &http.Transport{ResponseHeaderTimeout: 2 * time.Second}
		mux.Handle("/", proxy)
		// Process startup must not wait for outbound mesh connectivity: a mesh
		// proxy injected as a regular container may not have started yet.
		mux.HandleFunc("GET /startupz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	} else if mode == "proxy" {
		addr = ":8080"
		target, _ := url.Parse("http://127.0.0.1:8081")
		mux.Handle("/", httputil.NewSingleHostReverseProxy(target))
		mux.HandleFunc("GET /proxy-ready", dependenciesReady)
	} else {
		mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		// The marker survives native-sidecar restarts so diagnostics can observe
		// CrashLoopBackOff. It is confined to this disposable Pod's emptyDir.
		go watchFailure(ctx, marker)
	}

	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer done()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("%s fixture listening", mode)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func watchFailure(ctx context.Context, marker string) {
	ticker := time.NewTicker(200 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			if _, err := os.Stat(marker); err == nil {
				ticker.Stop()
				log.Print("native-helper synthetic failure")
				os.Exit(17)
			}
		}
	}
}

func dependenciesReady(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: time.Second}
	for _, endpoint := range []string{"http://127.0.0.1:8081/readyz", "http://127.0.0.1:8082/readyz", "http://127.0.0.1:8083/readyz"} {
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
		if err != nil {
			http.Error(w, "invalid fixture dependency", http.StatusInternalServerError)
			return
		}

		response, err := client.Do(request)
		if err != nil {
			http.Error(w, "fixture dependency unavailable", http.StatusServiceUnavailable)
			return
		}

		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			http.Error(w, "fixture dependency unready", http.StatusServiceUnavailable)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
)

// The composite fixture uses a read-only record lookup to exercise an
// application-to-sidecar-to-shared-dependency path. It is never enabled in the
// ordinary demo baseline unless the fixture supplies SIMULATED_DATABASE_URL.
type accessRecord struct {
	Subject  string   `json:"subject"`
	Resource string   `json:"resource"`
	Actions  []string `json:"actions"`
	Source   string   `json:"source"`
}

type accessResult struct {
	Subject               string   `json:"subject"`
	Resource              string   `json:"resource"`
	Actions               []string `json:"actions"`
	Source                string   `json:"source"`
	Release               string   `json:"release"`
	WorkloadID            string   `json:"workload_id"`
	RequestComposition    string   `json:"request_composition"`
	DeploymentComposition string   `json:"deployment_composition"`
}

func recordHandler(service, downstream, databaseURL, workload, deploymentComposition string, client *http.Client, prop propagation.TextMapPropagator) http.Handler {
	return otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if service != "service-b" {
			if downstream == "" {
				http.NotFound(w, r)
				return
			}

			forwardRecord(w, r, client, downstream)
			return
		}

		if databaseURL == "" {
			http.NotFound(w, r)
			return
		}

		record, err := fetchRecord(r.Context(), client, databaseURL, r.URL.EscapedPath())
		if err != nil {
			http.Error(w, "synthetic database unavailable", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessResult{
			Subject:               record.Subject,
			Resource:              record.Resource,
			Actions:               record.Actions,
			Source:                record.Source,
			Release:               Version,
			WorkloadID:            workload,
			RequestComposition:    baggage.FromContext(r.Context()).Member("composition").Value(),
			DeploymentComposition: deploymentComposition,
		})
	}), "record-"+service, otelhttp.WithPropagators(prop))
}

func forwardRecord(w http.ResponseWriter, r *http.Request, client *http.Client, downstream string) {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(downstream, "/")+r.URL.EscapedPath(), nil)
	if err != nil {
		http.Error(w, "invalid downstream", http.StatusBadGateway)
		return
	}

	response, err := client.Do(request)
	if err != nil {
		http.Error(w, "downstream unavailable", http.StatusBadGateway)
		return
	}

	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		http.Error(w, "downstream record unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, io.LimitReader(response.Body, 16<<10))
}

func fetchRecord(ctx context.Context, client *http.Client, databaseURL, path string) (accessRecord, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(databaseURL, "/")+path, nil)
	if err != nil {
		return accessRecord{}, err
	}

	response, err := client.Do(request)
	if err != nil {
		return accessRecord{}, err
	}

	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return accessRecord{}, fmt.Errorf("synthetic database status %d", response.StatusCode)
	}

	var record accessRecord
	if err = json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&record); err != nil {
		return accessRecord{}, err
	}

	if record.Subject == "" || record.Resource == "" || len(record.Actions) == 0 || record.Source == "" {
		return accessRecord{}, fmt.Errorf("incomplete synthetic database record")
	}

	return record, nil
}

func recordDatabaseReady(ctx context.Context, client *http.Client, databaseURL string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(databaseURL, "/")+"/readyz", nil)
	if err != nil {
		return false
	}

	response, err := client.Do(request)
	if err != nil {
		return false
	}

	defer func() { _ = response.Body.Close() }()
	return response.StatusCode == http.StatusOK
}

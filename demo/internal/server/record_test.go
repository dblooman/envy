package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const sampleRecordPath = "/api/v1/subjects/sample-user/resources/sample-space/access"

func TestCompositeRecordTraversesApplicationAndSyntheticDatabase(t *testing.T) {
	var available atomic.Bool
	available.Store(true)
	database := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.URL.Path != sampleRecordPath || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subject":"sample-user","resource":"sample-space","actions":["read"],"source":"synthetic-database"}`))
	}))
	defer database.Close()
	t.Setenv("SIMULATED_DATABASE_URL", database.URL)

	b := httptest.NewServer(Handler("service-b", "", "pod-b", "preview-b"))
	defer b.Close()
	a := httptest.NewServer(Handler("service-a", b.URL, "pod-a", "baseline"))
	defer a.Close()
	g := httptest.NewServer(Handler("gateway", a.URL, "pod-g", "baseline"))
	defer g.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, g.URL+sampleRecordPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("baggage", "composition=preview-b")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("record status=%d", response.StatusCode)
	}

	var record accessResult
	if err := json.NewDecoder(response.Body).Decode(&record); err != nil {
		t.Fatal(err)
	}

	if record.Subject != "sample-user" || record.Resource != "sample-space" || len(record.Actions) != 1 || record.Actions[0] != "read" || record.Source != "synthetic-database" || record.Release != Version || record.WorkloadID != "pod-b" || record.RequestComposition != "preview-b" || record.DeploymentComposition != "preview-b" {
		t.Fatalf("unexpected synthetic record: %+v", record)
	}

	available.Store(false)
	ready, err := http.Get(b.URL + "/readyz") //nolint:noctx // Local test request.
	if err != nil {
		t.Fatal(err)
	}

	_ = ready.Body.Close()
	if ready.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("database outage left application ready: %d", ready.StatusCode)
	}

	failed, err := http.Get(g.URL + sampleRecordPath) //nolint:noctx // Local test request.
	if err != nil {
		t.Fatal(err)
	}

	_ = failed.Body.Close()
	if failed.StatusCode == http.StatusOK {
		t.Fatal("database outage returned a successful record")
	}
}

func TestRecordEndpointRequiresFixtureDatabase(t *testing.T) {
	t.Setenv("SIMULATED_DATABASE_URL", "")
	response := httptest.NewRecorder()
	Handler("service-b", "", "pod-b", "baseline").ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, sampleRecordPath, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unconfigured record endpoint status=%d", response.Code)
	}
}

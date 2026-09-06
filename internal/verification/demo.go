// Package verification checks observed request behavior separately from provider
// configuration acceptance. The initial checker understands the demo contract.
package verification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dblooman/envy/demo/protocol"
)

type Result struct {
	Baseline    []protocol.Hop
	Composition []protocol.Hop
}

type Demo struct {
	ingressURL   string
	baselineHost string
	client       *http.Client
}

func New(ingressURL, baselineHost string, client *http.Client) (*Demo, error) {
	u, err := url.Parse(ingressURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, fmt.Errorf("invalid ingress URL")
	}
	if baselineHost == "" || strings.ContainsAny(baselineHost, "/\r\n") {
		return nil, fmt.Errorf("invalid baseline hostname")
	}
	if client == nil {
		client = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Demo{ingressURL: strings.TrimRight(ingressURL, "/") + "/", baselineHost: baselineHost, client: client}, nil
}

func (v *Demo) request(ctx context.Context, host string) (int, []protocol.Hop, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.ingressURL, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("construct verification request: %w", err)
	}
	req.Host = host
	resp, err := v.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("preview ingress unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, nil, nil
	}
	var body protocol.Response
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 65537))
	if err := decoder.Decode(&body); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("decode demo verification response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return resp.StatusCode, nil, fmt.Errorf("demo verification response has trailing content")
	}
	return resp.StatusCode, body.Chain, nil
}

func validate(chain []protocol.Hop, id string) error {
	names := []string{"gateway", "service-a", "service-b"}
	if len(chain) != len(names) {
		return fmt.Errorf("expected three demo hops, got %d", len(chain))
	}
	for i, h := range chain {
		if h.Service != names[i] || h.Composition != id || h.WorkloadID == "" || h.Version == "" {
			return fmt.Errorf("unexpected identity or context at %s", names[i])
		}
		deployment := "baseline"
		if i == 2 && id != "" {
			deployment = id
		}
		if h.DeploymentComposition != deployment {
			return fmt.Errorf("unexpected deployment identity at %s", names[i])
		}
	}
	return nil
}

func (v *Demo) Verify(ctx context.Context, id, host, workloadID string) (Result, error) {
	if id == "" || host == "" || workloadID == "" {
		return Result{}, fmt.Errorf("composition and observed workload identities are required")
	}
	code, baseline, err := v.request(ctx, v.baselineHost)
	if err != nil {
		return Result{}, err
	}
	if code != 200 {
		return Result{}, fmt.Errorf("baseline ingress returned HTTP %d", code)
	}
	if err = validate(baseline, ""); err != nil {
		return Result{}, fmt.Errorf("baseline: %w", err)
	}
	code, chain, err := v.request(ctx, host)
	if err != nil {
		return Result{}, err
	}
	if code != 200 {
		return Result{}, fmt.Errorf("composition ingress returned HTTP %d", code)
	}
	if err = validate(chain, id); err != nil {
		return Result{}, fmt.Errorf("composition: %w", err)
	}
	for i := range 2 {
		if chain[i].WorkloadID != baseline[i].WorkloadID || chain[i].Version != baseline[i].Version {
			return Result{}, fmt.Errorf("inherited %s did not use the observed baseline", chain[i].Service)
		}
	}
	if chain[2].WorkloadID != workloadID {
		return Result{}, fmt.Errorf("service-b did not reach the observed override pod")
	}
	return Result{Baseline: baseline, Composition: chain}, nil
}

// Absent verifies ingress withdrawal, not the health of the old destination.
func (v *Demo) Absent(ctx context.Context, host string) error {
	code, _, err := v.request(ctx, host)
	if err != nil {
		return err
	}
	if code != http.StatusNotFound {
		return fmt.Errorf("hostname withdrawal not observed: HTTP %d", code)
	}
	return nil
}

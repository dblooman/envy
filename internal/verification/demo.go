// Package verification checks observed request behavior separately from provider
// configuration acceptance. Registered contracts distinguish full chain routing
// evidence from ordinary HTTP reachability.
package verification

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/dblooman/envy/demo/protocol"
	"github.com/dblooman/envy/internal/domain"
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
	return NewWithRoots(ingressURL, baselineHost, client, nil)
}

func NewWithRoots(ingressURL, baselineHost string, client *http.Client, roots *x509.CertPool) (*Demo, error) {
	u, err := url.Parse(ingressURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, fmt.Errorf("invalid ingress URL")
	}
	if baselineHost == "" || strings.ContainsAny(baselineHost, "/\r\n") {
		return nil, fmt.Errorf("invalid baseline hostname")
	}
	if client == nil {
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
		if u.Scheme == "https" {
			address := u.Host
			if u.Port() == "" {
				address = net.JoinHostPort(u.Hostname(), "443")
			}
			transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
			}
		}
		client = &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Demo{ingressURL: strings.TrimRight(ingressURL, "/") + "/", baselineHost: baselineHost, client: client}, nil
}

func (v *Demo) request(ctx context.Context, host string) (int, []protocol.Hop, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.ingressURL, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("construct verification request: %w", err)
	}
	req.Host = host
	if req.URL.Scheme == "https" {
		req.URL.Host = host
	}
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

func validate(chain []protocol.Hop, id string, names []string, overrides map[string]string) error {
	if len(chain) != len(names) {
		return fmt.Errorf("expected %d registered hops, got %d", len(names), len(chain))
	}
	for i, h := range chain {
		if h.Service != names[i] || h.Composition != id || h.WorkloadID == "" || h.Version == "" {
			return fmt.Errorf("unexpected identity or context at %s", names[i])
		}
		deployment := "baseline"
		if _, overridden := overrides[h.Service]; overridden && id != "" {
			deployment = id
		}
		if h.DeploymentComposition != deployment {
			return fmt.Errorf("unexpected deployment identity at %s", names[i])
		}
	}
	return nil
}

func (v *Demo) Verify(ctx context.Context, id, host string, workloads map[string]string, plan domain.ResolvedPlan) (Result, error) {
	if plan.Baseline.Verification.Kind == "http" {
		return v.verifyHTTP(ctx, id, host, workloads, plan)
	}
	profiles := plan.Profiles()
	if plan.Baseline.Verification.Kind != "envy-chain" || len(plan.Baseline.Verification.Chain) == 0 || len(workloads) != len(profiles) {
		return Result{}, fmt.Errorf("invalid registered verification contract")
	}
	if id == "" || host == "" {
		return Result{}, fmt.Errorf("composition identity and hostname are required")
	}
	for component := range profiles {
		if !slices.Contains(plan.Baseline.Verification.Chain, component) || workloads[component] == "" {
			return Result{}, fmt.Errorf("observed workload identity required for %s", component)
		}
	}
	code, baseline, err := v.request(ctx, baselineHost(plan.Baseline))
	if err != nil {
		return Result{}, err
	}
	if code != 200 {
		return Result{}, fmt.Errorf("baseline ingress returned HTTP %d", code)
	}
	if err = validate(baseline, "", plan.Baseline.Verification.Chain, workloads); err != nil {
		return Result{}, fmt.Errorf("baseline: %w", err)
	}
	code, chain, err := v.request(ctx, host)
	if err != nil {
		return Result{}, err
	}
	if code != 200 {
		return Result{}, fmt.Errorf("composition ingress returned HTTP %d", code)
	}
	if err = validate(chain, id, plan.Baseline.Verification.Chain, workloads); err != nil {
		return Result{}, fmt.Errorf("composition: %w", err)
	}
	for i := range chain {
		if workloadID, overridden := workloads[chain[i].Service]; overridden {
			if chain[i].WorkloadID != workloadID {
				return Result{}, fmt.Errorf("%s did not reach the observed override pod", chain[i].Service)
			}
			continue
		}
		if chain[i].WorkloadID != baseline[i].WorkloadID || chain[i].Version != baseline[i].Version {
			return Result{}, fmt.Errorf("inherited %s did not use the observed baseline", chain[i].Service)
		}
	}
	return Result{Baseline: baseline, Composition: chain}, nil
}

// Absent verifies ingress withdrawal, not the health of the old destination.
func (v *Demo) Absent(ctx context.Context, host string) error {
	code, routeID, err := v.status(ctx, host, "/")
	if err != nil {
		return err
	}
	if code != http.StatusNotFound || routeID != "" {
		return fmt.Errorf("hostname withdrawal not observed: HTTP %d", code)
	}
	return nil
}

func baselineHost(b domain.Baseline) string {
	u, err := url.Parse(b.Endpoint)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
func (v *Demo) ValidateBaseline(ctx context.Context, b domain.Baseline, _ map[string]domain.Component) error {
	if b.Verification.Kind == "http" {
		if err := v.checkHTTP(ctx, baselineHost(b), b.Verification); err != nil {
			return domain.Validation("baseline HTTP check failed: " + err.Error())
		}
		return nil
	}
	code, chain, err := v.request(ctx, baselineHost(b))
	if err != nil {
		return err
	}
	if code != 200 {
		return domain.Validation(fmt.Sprintf("baseline ingress returned HTTP %d", code))
	}
	if err := validate(chain, "", b.Verification.Chain, nil); err != nil {
		return domain.Validation("baseline propagation check failed: " + err.Error())
	}
	return nil
}

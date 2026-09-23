package verification

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dblooman/envy/internal/domain"
)

// status checks only HTTP reachability; it deliberately ignores application data.
func (v *Demo) status(ctx context.Context, host, path string) (int, string, error) {
	code, route, _, err := v.statusWithID(ctx, host, path)
	return code, route, err
}

func (v *Demo) statusWithID(ctx context.Context, host, path string) (int, string, string, error) {
	u, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
		return 0, "", "", fmt.Errorf("invalid verification path")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(v.ingressURL, "/")+path, nil)
	if err != nil {
		return 0, "", "", err
	}

	req.Host = host
	if req.URL.Scheme == "https" {
		req.URL.Host = host
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return 0, "", "", fmt.Errorf("ingress request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, resp.Header.Get(domain.PreviewRouteHeader), observedRequestID(resp.Header.Get("X-Request-ID")), nil
}

func (v *Demo) checkHTTP(ctx context.Context, host string, contract domain.VerificationContract) error {
	if contract.Path == "" || contract.ExpectedStatus < 200 || contract.ExpectedStatus > 299 {
		return fmt.Errorf("invalid HTTP verification contract")
	}

	code, _, err := v.status(ctx, host, contract.Path)
	if err != nil {
		return err
	}

	if code != contract.ExpectedStatus {
		return fmt.Errorf("ingress %s at %s returned HTTP %d; expected %d", host, contract.Path, code, contract.ExpectedStatus)
	}

	return nil
}

func (v *Demo) verifyHTTP(ctx context.Context, id, host string, workloads map[string]string, plan domain.ResolvedPlan) (Result, error) {
	if plan.Baseline.Verification.ExpectedStatus < 200 || plan.Baseline.Verification.ExpectedStatus > 299 {
		return Result{}, fmt.Errorf("invalid HTTP verification contract")
	}

	if id == "" || host == "" || len(workloads) != len(plan.Profiles()) || len(workloads) == 0 {
		return Result{}, fmt.Errorf("HTTP verification requires observed workloads and composition identity")
	}

	for component := range plan.Profiles() {
		if workloads[component] == "" {
			return Result{}, fmt.Errorf("observed workload identity required for %s", component)
		}
	}

	result := Result{}
	for _, target := range []struct{ name, host string }{{"baseline", baselineHost(plan.Baseline)}, {"preview", host}} {
		code, _, requestID, err := v.statusWithID(ctx, target.host, plan.Baseline.Verification.Path)
		result.Probes = append(result.Probes, domain.VerificationProbe{Target: target.name, ExpectedStatus: plan.Baseline.Verification.ExpectedStatus, ObservedStatus: code, RequestID: requestID})
		if err != nil {
			return result, fmt.Errorf("%s: %w", target.name, err)
		}

		if code != plan.Baseline.Verification.ExpectedStatus {
			return result, fmt.Errorf("%s returned HTTP %d; expected %d", target.name, code, plan.Baseline.Verification.ExpectedStatus)
		}
	}
	return result, nil
}

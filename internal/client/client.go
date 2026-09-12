// Package client is the private HTTP client shared by Envy adapters.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
	channel string
	task    string
}

// New constructs an authenticated client. Redirects are refused so credentials
// cannot be carried to a different API endpoint by a server redirect.
func New(baseURL, token string, httpClient *http.Client) (*Client, error) {
	return NewWithIdentity(baseURL, token, httpClient, "api", "")
}
func NewWithIdentity(baseURL, token string, httpClient *http.Client, channel, task string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("API URL must be an http(s) URL without credentials, query, or fragment")
	}
	if token == "" {
		return nil, errors.New("API token is required")
	}
	if strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("API token must be one line")
	}
	hc := http.Client{Timeout: 120 * time.Second}
	if httpClient != nil {
		hc = *httpClient
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &hc, channel: domain.ValidChannel(channel), task: strings.TrimSpace(task)}, nil
}

func (c *Client) Create(ctx context.Context, request domain.CreateRequest, key string) (domain.Composition, error) {
	var result domain.Composition
	err := c.request(ctx, http.MethodPost, "/v1/compositions", request, key, &result)
	return result, err
}

func (c *Client) Get(ctx context.Context, id string) (domain.Composition, error) {
	var result domain.Composition
	path, err := compositionPath(id)
	if err != nil {
		return result, err
	}
	err = c.request(ctx, http.MethodGet, path, nil, "", &result)
	return result, err
}

func (c *Client) Destroy(ctx context.Context, id string) (domain.Composition, error) {
	var result domain.Composition
	path, err := compositionPath(id)
	if err != nil {
		return result, err
	}
	err = c.request(ctx, http.MethodDelete, path, nil, "", &result)
	return result, err
}

type Endpoint struct {
	URL   string `json:"url"`
	Ready bool   `json:"ready"`
}

type EndpointsResponse struct {
	ID        string              `json:"id"`
	Endpoints map[string]Endpoint `json:"endpoints"`
}

func (c *Client) Endpoints(ctx context.Context, id string) (EndpointsResponse, error) {
	var result EndpointsResponse
	path, err := compositionPath(id)
	if err != nil {
		return result, err
	}
	err = c.request(ctx, http.MethodGet, path+"/endpoints", nil, "", &result)
	return result, err
}

// Wait returns the latest observed composition at its timeout. Cancellation of
// the caller's context returns an error and never destroys a composition.
func (c *Client) Wait(ctx context.Context, id string, timeout time.Duration) (domain.Composition, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if timeout < 0 || timeout > 60*time.Second {
		return domain.Composition{}, &domain.Error{Code: "validation_error", Message: "wait timeout must be positive and at most 60 seconds"}
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var latest domain.Composition
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		observed, err := c.Get(waitCtx, id)
		if err != nil {
			if ctx.Err() != nil {
				return latest, ctx.Err()
			}
			if waitCtx.Err() != nil && latest.ID != "" {
				return latest, nil
			}
			return latest, err
		}
		latest = observed
		switch string(latest.Phase) {
		case "ready", "failed", "destroyed":
			return latest, nil
		}
		select {
		case <-ctx.Done():
			return latest, ctx.Err()
		case <-waitCtx.Done():
			return latest, nil
		case <-ticker.C:
		}
	}
}

func compositionPath(id string) (string, error) {
	if id == "" || len(id) > 128 || strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}) >= 0 {
		return "", &domain.Error{Code: "validation_error", Message: "invalid composition ID"}
	}
	return "/v1/compositions/" + id, nil
}

func (c *Client) request(ctx context.Context, method, path string, input any, key string, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode API request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	r, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("construct API request: %w", err)
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-Envy-Channel", c.channel)
	if c.task != "" {
		r.Header.Set("X-Envy-Task", c.task)
	}
	if input != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	response, err := c.http.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &domain.Error{Code: "unavailable", Message: "could not reach Envy API", Retryable: true}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return &domain.Error{Code: "unavailable", Message: "invalid or oversized API response", Retryable: true}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope struct {
			Error *domain.Error `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error != nil && envelope.Error.Code != "" {
			return envelope.Error
		}
		return &domain.Error{Code: "unavailable", Message: fmt.Sprintf("Envy API returned HTTP %d", response.StatusCode), Retryable: response.StatusCode >= 500}
	}
	if err := json.Unmarshal(data, output); err != nil {
		return &domain.Error{Code: "unavailable", Message: "Envy API returned invalid JSON", Retryable: true}
	}
	return nil
}

func (c *Client) Update(ctx context.Context, id string, request domain.UpdateRequest) (domain.Composition, error) {
	var result domain.Composition
	path, err := compositionPath(id)
	if err != nil {
		return result, err
	}
	err = c.request(ctx, http.MethodPatch, path, request, request.IdempotencyKey, &result)
	return result, err
}

type CompositionsPage struct {
	Items      []domain.Composition `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

func (c *Client) List(ctx context.Context, project, after string, limit int) (CompositionsPage, error) {
	var result CompositionsPage
	if limit < 1 || limit > 100 {
		return result, domain.Validation("limit must be between 1 and 100")
	}
	query := url.Values{"project": {project}, "after": {after}, "limit": {fmt.Sprint(limit)}}
	err := c.request(ctx, http.MethodGet, "/v1/compositions?"+query.Encode(), nil, "", &result)
	return result, err
}

func (c *Client) Logs(ctx context.Context, id, component string, options domain.LogOptions) (domain.ComponentLogs, error) {
	var result domain.ComponentLogs
	path, err := compositionPath(id)
	if err != nil {
		return result, err
	}
	if _, err = compositionPath(component); err != nil {
		return result, domain.Validation("invalid component ID")
	}
	options, err = domain.NormalizeLogOptions(options)
	if err != nil {
		return result, err
	}
	q := url.Values{"tail_lines": {fmt.Sprint(options.TailLines)}, "max_bytes": {fmt.Sprint(options.MaxBytes)}, "previous": {fmt.Sprint(options.Previous)}}
	if options.SinceSeconds > 0 {
		q.Set("since_seconds", fmt.Sprint(options.SinceSeconds))
	}
	err = c.request(ctx, http.MethodGet, path+"/components/"+component+"/logs?"+q.Encode(), nil, "", &result)
	return result, err
}
func (c *Client) Events(ctx context.Context, id, after string, limit int) (domain.EventsPage, error) {
	var page domain.EventsPage
	path, err := compositionPath(id)
	if err != nil {
		return page, err
	}
	if _, err = domain.EventCursor(after); err != nil {
		return page, err
	}
	if limit < 1 || limit > 100 {
		return page, domain.Validation("limit must be between 1 and 100")
	}
	q := url.Values{"after": {after}, "limit": {fmt.Sprint(limit)}}
	err = c.request(ctx, http.MethodGet, path+"/events?"+q.Encode(), nil, "", &page)
	return page, err
}

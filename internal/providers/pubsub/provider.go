// Package pubsub manages composition-owned pull subscriptions. It never reads message payloads.
package pubsub

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"golang.org/x/oauth2/google"
)

type Provider struct {
	client                 *http.Client
	endpoint, installation string
	guard                  func(context.Context) error
}

func New(ctx context.Context, installation string, guard func(context.Context) error) (*Provider, error) {
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/pubsub")
	if err != nil {
		return nil, fmt.Errorf("Pub/Sub application default credentials: %w", err)
	}

	client.Timeout = 15 * time.Second
	return &Provider{client: client, endpoint: "https://pubsub.googleapis.com/v1/", installation: installation, guard: guard}, nil
}

// WithGuard returns a copy bound to the current controller leadership lease.
func (p *Provider) WithGuard(guard func(context.Context) error) *Provider {
	copy := *p
	copy.guard = guard
	return &copy
}

type subscription struct {
	Detached     bool              `json:"detached,omitempty"`
	Transforms   []map[string]any  `json:"messageTransforms,omitempty"`
	BigTable     map[string]any    `json:"bigtableConfig,omitempty"`
	Name         string            `json:"name"`
	Topic        string            `json:"topic"`
	Filter       string            `json:"filter"`
	Labels       map[string]string `json:"labels"`
	Retention    string            `json:"messageRetentionDuration"`
	Expiration   map[string]string `json:"expirationPolicy"`
	Push         map[string]any    `json:"pushConfig,omitempty"`
	BigQuery     map[string]any    `json:"bigqueryConfig,omitempty"`
	CloudStorage map[string]any    `json:"cloudStorageConfig,omitempty"`
	DeadLetter   map[string]any    `json:"deadLetterPolicy,omitempty"`
}
type apiError int

func (e apiError) Error() string { return fmt.Sprintf("Pub/Sub API returned HTTP %d", e) }
func (p *Provider) call(ctx context.Context, method, path string, body, out any) error {
	if method != "GET" {
		if p.guard == nil {
			return errors.New("Pub/Sub mutations require a leadership guard")
		}

		if err := p.guard(ctx); err != nil {
			return err
		}
	}

	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	// Escape literal resource-name characters without changing separators or API query parameters.
	pathParts := strings.SplitN(path, "?", 2)
	segments := strings.Split(pathParts[0], "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}

	escapedPath := strings.Join(segments, "/")
	if len(pathParts) == 2 {
		escapedPath += "?" + pathParts[1]
	}

	req, err := http.NewRequestWithContext(ctx, method, p.endpoint+escapedPath, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("Pub/Sub request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(resp.StatusCode)
	}

	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
	}

	return nil
}

func (p *Provider) get(ctx context.Context, name string) (subscription, error) {
	var s subscription
	err := p.call(ctx, "GET", name, nil, &s)
	return s, err
}

func label(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:20])
}

func (p *Provider) labels(spec domain.MessagingSpec) map[string]string {
	return map[string]string{"envy-installation": label(p.installation), "envy-composition": label(spec.CompositionID), "envy-owner": label(spec.OwnershipToken)}
}

func (p *Provider) owned(got subscription, spec domain.MessagingSpec) bool {
	if spec.CompositionID == "" || spec.OwnershipToken == "" {
		return false
	}

	for k, v := range p.labels(spec) {
		if got.Labels[k] != v {
			return false
		}
	}

	return true
}

func matches(got subscription, want domain.MessageSubscription) bool {
	return !got.Detached && len(got.Transforms) == 0 && len(got.BigTable) == 0 && got.Name == want.Name && got.Topic == want.Topic && got.Filter == want.Filter && got.Retention == want.Retention && got.Expiration != nil && len(got.Expiration) == 0 && len(got.Push) == 0 && len(got.BigQuery) == 0 && len(got.CloudStorage) == 0 && len(got.DeadLetter) == 0
}

var isolatedFilter = regexp.MustCompile(`^attributes\.envy_composition = "[a-f0-9]{24}"$`)

func previewFilter(filter, business string) bool {
	if business != "" {
		suffix := ") AND (" + business + ")"
		if !strings.HasPrefix(filter, "(") || !strings.HasSuffix(filter, suffix) {
			return false
		}

		filter = strings.TrimSuffix(strings.TrimPrefix(filter, "("), suffix)
	}

	return isolatedFilter.MatchString(filter)
}

func (p *Provider) Validate(ctx context.Context, b domain.Baseline) (err error) {
	defer func() {
		if err == nil {
			return
		}

		if _, ok := errors.AsType[*domain.Error](err); ok {
			return
		}

		message := "cannot inspect Pub/Sub resources; check controller credentials, resource existence and topic/subscription permissions"
		if status, ok := errors.AsType[apiError](err); ok {
			message = fmt.Sprintf("%s (HTTP %d)", message, status)
		}

		err = &domain.Error{Code: "unavailable", Message: message, Retryable: true}
	}()
	if err := domain.ValidateMessaging(b); err != nil {
		return err
	}

	for _, topic := range b.PubSub {
		if err := p.validateTopic(ctx, topic); err != nil {
			return err
		}
	}

	return nil
}

func (p *Provider) validateTopic(ctx context.Context, topic domain.PubSubTopic) error {
	var metadata struct {
		Name       string           `json:"name"`
		Transforms []map[string]any `json:"messageTransforms"`
	}
	if err := p.call(ctx, "GET", topic.Topic, nil, &metadata); err != nil {
		return fmt.Errorf("inspect topic %s: %w", topic.Topic, err)
	}

	if len(metadata.Transforms) > 0 {
		return domain.Validation("Pub/Sub topic message transforms are not supported for isolation: " + topic.Topic)
	}

	if err := p.validateBaselineConsumers(ctx, topic); err != nil {
		return err
	}

	return p.validateTopicInventory(ctx, topic)
}

func (p *Provider) validateBaselineConsumers(ctx context.Context, topic domain.PubSubTopic) error {
	for _, consumer := range topic.Consumers {
		got, err := p.get(ctx, consumer.Subscription)
		if err != nil {
			return fmt.Errorf("inspect baseline subscription %s: %w", consumer.Subscription, err)
		}

		if got.Detached || len(got.Transforms) > 0 || got.Topic != topic.Topic || got.Filter != domain.MessageFilter(domain.BaselineMessageFilter, consumer.Filter) {
			return domain.Validation(fmt.Sprintf("baseline subscription %s must be attached, use the registered exclusion filter and have no message transforms", consumer.Subscription))
		}
	}

	return nil
}

func (p *Provider) validateTopicInventory(ctx context.Context, topic domain.PubSubTopic) error {
	for token := ""; ; {
		var page struct {
			Subscriptions []string `json:"subscriptions"`
			Next          string   `json:"nextPageToken"`
		}
		path := topic.Topic + "/subscriptions?pageSize=1000"
		if token != "" {
			path += "&pageToken=" + url.QueryEscape(token)
		}

		if err := p.call(ctx, "GET", path, nil, &page); err != nil {
			return fmt.Errorf("inventory topic subscriptions: %w", err)
		}

		for _, name := range page.Subscriptions {
			if err := p.validateInventoriedSubscription(ctx, topic, name); err != nil {
				return err
			}
		}

		if page.Next == "" {
			return nil
		}

		token = page.Next
	}
}

func (p *Provider) validateInventoriedSubscription(ctx context.Context, topic domain.PubSubTopic, name string) error {
	got, err := p.get(ctx, name)
	if err != nil {
		return err
	}

	protected := false
	for _, consumer := range topic.Consumers {
		if got.Filter == domain.MessageFilter(domain.BaselineMessageFilter, consumer.Filter) {
			protected = true
		}

		if got.Labels["envy-installation"] == label(p.installation) && got.Labels["envy-owner"] != "" && previewFilter(got.Filter, consumer.Filter) {
			protected = true
		}
	}

	if !protected || len(got.Transforms) > 0 {
		return domain.Validation(fmt.Sprintf("unprotected or unsupported subscription %s on %s; migrate its filter and remove message transforms before enabling isolation", name, topic.Topic))
	}

	return nil
}

func (p *Provider) Ensure(ctx context.Context, spec domain.MessagingSpec) ([]domain.MessageSubscription, error) {
	if spec.CompositionID == "" || spec.OwnershipToken == "" {
		return nil, errors.New("Pub/Sub requires composition ownership")
	}

	out := append([]domain.MessageSubscription(nil), spec.Subscriptions...)
	for i, want := range spec.Subscriptions {
		ensured, err := p.ensureSubscription(ctx, spec, want)
		if err != nil {
			return out, err
		}

		out[i] = ensured
	}

	return out, nil
}

func (p *Provider) ensureSubscription(ctx context.Context, spec domain.MessagingSpec, want domain.MessageSubscription) (domain.MessageSubscription, error) {
	ensured := want
	ensured.Ready = false
	got, err := p.get(ctx, want.Name)
	if errors.Is(err, apiError(http.StatusNotFound)) {
		// Ready is durable evidence that a previously observed subscription disappeared.
		ensured.BacklogMayBeLost = want.BacklogMayBeLost || want.Ready
		got, err = p.createSubscription(ctx, spec, want)
	}

	if err != nil {
		return ensured, err
	}

	if !p.owned(got, spec) {
		return ensured, fmt.Errorf("subscription ownership conflict: %s", want.Name)
	}

	instance := got.Labels["envy-instance"]
	if instance == "" {
		return ensured, fmt.Errorf("subscription has no Envy instance identity: %s", want.Name)
	}

	ensured.Instance = instance
	if want.Instance != "" && want.Instance != instance {
		ensured.BacklogMayBeLost = true
	}

	if !matches(got, want) {
		return ensured, fmt.Errorf("subscription configuration drift: %s (no baseline fallback)", want.Name)
	}

	ensured.Ready = true
	return ensured, nil
}

func (p *Provider) createSubscription(ctx context.Context, spec domain.MessagingSpec, want domain.MessageSubscription) (subscription, error) {
	instance := make([]byte, 16)
	if _, err := rand.Read(instance); err != nil {
		return subscription{}, fmt.Errorf("generate subscription instance: %w", err)
	}

	labels := p.labels(spec)
	labels["envy-instance"] = hex.EncodeToString(instance)
	desired := subscription{Name: want.Name, Topic: want.Topic, Filter: want.Filter, Labels: labels, Retention: want.Retention, Expiration: map[string]string{}}
	if err := p.call(ctx, "PUT", want.Name, desired, nil); err != nil && !errors.Is(err, apiError(http.StatusConflict)) {
		return subscription{}, err
	}

	return p.get(ctx, want.Name)
}

func (p *Provider) Delete(ctx context.Context, spec domain.MessagingSpec) (bool, error) {
	for _, want := range spec.Subscriptions {
		got, err := p.get(ctx, want.Name)
		if errors.Is(err, apiError(http.StatusNotFound)) {
			continue
		}

		if err != nil {
			return false, err
		}

		if !p.owned(got, spec) {
			return false, fmt.Errorf("subscription ownership conflict: %s", want.Name)
		}

		if err = p.call(ctx, "DELETE", want.Name, nil, nil); err != nil && !errors.Is(err, apiError(http.StatusNotFound)) {
			return false, err
		}

		_, err = p.get(ctx, want.Name)
		if err == nil {
			return false, nil
		}

		if !errors.Is(err, apiError(http.StatusNotFound)) {
			return false, err
		}
	}

	return true, nil
}

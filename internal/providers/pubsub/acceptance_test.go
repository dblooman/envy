package pubsub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"go.opentelemetry.io/otel/baggage"
)

// Test-only application fixture. Production applications implement this documented
// boundary themselves; no application SDK or payload access is part of Envy.
func fixtureAttributes(inbound *string, deployment string, isolated bool) (map[string]string, error) {
	id, mode := deployment, isolated
	if inbound != nil {
		bag, err := baggage.Parse(*inbound)
		if err != nil {
			return nil, err
		}

		id = bag.Member("composition").Value()
		flag := bag.Member("envy_message_isolation").Value()
		if id != "" && flag != "true" && flag != "false" {
			return nil, fmt.Errorf("composition context lacks isolation decision")
		}

		if id == "" && (flag == "true" || isolated) {
			return nil, fmt.Errorf("isolated request lost composition context")
		}

		mode = flag == "true"
	}

	if mode && id == "" {
		return nil, fmt.Errorf("isolated background job lacks composition")
	}

	attributes := map[string]string{"kind": "order"}
	if mode {
		attributes["envy_composition"] = id
	}

	return attributes, nil
}

func TestApplicationContextContract(t *testing.T) {
	for _, tc := range []struct {
		name, inbound, deployment  string
		request, isolated, invalid bool
		want                       string
	}{
		{name: "inherited publisher", inbound: "composition=preview,envy_message_isolation=true", request: true, want: "preview"},
		{name: "request precedes deployment", inbound: "composition=another,envy_message_isolation=true", deployment: "preview", request: true, isolated: true, want: "another"},
		{name: "background", deployment: "preview", isolated: true, want: "preview"},
		{name: "off", inbound: "composition=preview,envy_message_isolation=false", request: true},
		{name: "baseline", request: true},
		{name: "lost mode", inbound: "composition=preview", request: true, invalid: true},
		{name: "lost identity", inbound: "envy_message_isolation=true", request: true, invalid: true},
		{name: "lost request context", deployment: "preview", request: true, isolated: true, invalid: true},
		{name: "missing deployment", isolated: true, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var inbound *string
			if tc.request {
				inbound = &tc.inbound
			}

			attrs, err := fixtureAttributes(inbound, tc.deployment, tc.isolated)
			if (err != nil) != tc.invalid {
				t.Fatal(err)
			}

			if err == nil && attrs["envy_composition"] != tc.want {
				t.Fatal(attrs)
			}
		})
	}

	// Consumer restores context, then an inherited HTTP publisher emits the same identity.
	inbound := "composition=preview,envy_message_isolation=true"
	attrs, err := fixtureAttributes(&inbound, "", false)
	restored := "composition=" + attrs["envy_composition"] + ",envy_message_isolation=true"
	next, err2 := fixtureAttributes(&restored, "", false)
	if err != nil || err2 != nil || next["envy_composition"] != "preview" {
		t.Fatal("consumer to HTTP to publisher lost context")
	}
}

// Opt in explicitly. The cloud suite only touches uniquely named resources it
// creates in ENVY_PUBSUB_TEST_PROJECT; it never discovers a default gcloud project.
func TestBrokerAcceptance(t *testing.T) {
	endpoint := os.Getenv("ENVY_PUBSUB_TEST_EMULATOR")
	project := os.Getenv("ENVY_PUBSUB_TEST_PROJECT")
	if endpoint == "" && project == "" {
		t.Skip("set ENVY_PUBSUB_TEST_EMULATOR or an explicit disposable ENVY_PUBSUB_TEST_PROJECT")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	guard := func(context.Context) error { return nil }
	var p *Provider
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
			t.Fatal("emulator must be an explicit loopback HTTP URL")
		}

		project = "envy-pubsub-test"
		p = &Provider{client: &http.Client{Timeout: 10 * time.Second}, endpoint: strings.TrimRight(endpoint, "/") + "/v1/", installation: "acceptance", guard: guard}
	} else {
		var err error
		p, err = New(ctx, "acceptance", guard)
		if err != nil {
			t.Fatal(err)
		}
	}

	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}

	id := hex.EncodeToString(random)
	prefix := "projects/" + project
	topic := prefix + "/topics/envy-acceptance-" + id
	baselineName := prefix + "/subscriptions/envy-acceptance-" + id
	cleanup := func(method, path string) {
		c, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := p.call(c, method, path, nil, nil); err != nil && !errors.Is(err, apiError(404)) {
			t.Errorf("cleanup %s: %v", path, err)
		}
	}
	// Register cleanup before provisioning to cover interrupted and partial creates.
	t.Cleanup(func() { cleanup("DELETE", topic) })
	if err := p.call(ctx, "PUT", topic, map[string]string{"name": topic}, nil); err != nil {
		t.Fatal(err)
	}

	b := domain.Baseline{Components: map[string]domain.BaselineBinding{"publisher": {}, "worker": {}}, PubSub: map[string]domain.PubSubTopic{"orders": {Topic: topic, Publishers: []string{"publisher"}, Consumers: map[string]domain.PubSubConsumer{"billing": {Subscription: baselineName, Component: "worker", SubscriptionEnv: "SUB", Filter: `attributes.kind = "order"`}}}}}
	t.Cleanup(func() { cleanup("DELETE", baselineName) })
	if err := p.call(ctx, "PUT", baselineName, subscription{Name: baselineName, Topic: topic, Filter: domain.MessageFilter(domain.BaselineMessageFilter, `attributes.kind = "order"`), Expiration: map[string]string{}, Retention: "604800s"}, nil); err != nil {
		t.Fatal(err)
	}

	if err := p.Validate(ctx, b); err != nil {
		t.Fatal(err)
	}

	specs := []domain.MessagingSpec{}
	for _, preview := range []string{id, strings.Repeat("b", 24)} {
		spec := domain.MessagingSpec{CompositionID: preview, OwnershipToken: "test-" + id, Subscriptions: domain.ResolveMessaging(b, "acceptance", preview, time.Now().Add(time.Hour))}
		// id participates in topic but deterministic subscription name also needs run identity.
		spec.Subscriptions = domain.ResolveMessaging(b, "acceptance-"+id, preview, time.Now().Add(time.Hour))
		name := spec.Subscriptions[0].Name
		t.Cleanup(func() { cleanup("DELETE", name) })
		observed, err := p.Ensure(ctx, spec)
		if err != nil {
			t.Fatal(err)
		}

		spec.Subscriptions = observed
		specs = append(specs, spec)
	}

	publish := func(data string, attrs map[string]string) error {
		return p.call(ctx, "POST", topic+":publish", map[string]any{"messages": []any{map[string]any{"data": base64.StdEncoding.EncodeToString([]byte(data)), "attributes": attrs}}}, nil)
	}
	for _, preview := range []string{"", specs[0].CompositionID, specs[1].CompositionID} {
		var inbound *string
		if preview != "" {
			v := "composition=" + preview + ",envy_message_isolation=true"
			inbound = &v
		}

		attrs, err := fixtureAttributes(inbound, "", false)
		if err != nil {
			t.Fatal(err)
		}

		if err := publish("message-"+preview, attrs); err != nil {
			t.Fatal(err)
		}
	}

	// Wrong business type must be excluded even with a matching preview identity.
	if err := publish("filtered-business", map[string]string{"kind": "other", "envy_composition": id}); err != nil {
		t.Fatal(err)
	}

	type received struct {
		Ack     string `json:"ackId"`
		Message struct {
			Data       string            `json:"data"`
			Attributes map[string]string `json:"attributes"`
		} `json:"message"`
	}
	pull := func(name string) []received {
		t.Helper()
		var response struct {
			Messages []received `json:"receivedMessages"`
		}
		c, stop := context.WithTimeout(ctx, 4*time.Second)
		defer stop()
		err := p.call(c, "POST", name+":pull", map[string]any{"maxMessages": 10, "returnImmediately": true}, &response)
		if err != nil {
			if c.Err() != nil {
				return nil
			}

			t.Fatal(err)
		}

		return response.Messages
	}
	for i, name := range []string{baselineName, specs[0].Subscriptions[0].Name, specs[1].Subscriptions[0].Name} {
		expected := ""
		if i > 0 {
			expected = specs[i-1].CompositionID
		}

		var messages []received
		deadline := time.Now().Add(15 * time.Second)
		for len(messages) == 0 && time.Now().Before(deadline) {
			messages = pull(name)
		}

		if len(messages) != 1 {
			t.Fatalf("filter support required: %s received %d messages, expected one", name, len(messages))
		}

		msg := messages[0]
		payload, _ := base64.StdEncoding.DecodeString(msg.Message.Data)
		if msg.Message.Attributes["envy_composition"] != expected || string(payload) != "message-"+expected {
			t.Fatalf("cross-preview or baseline pollution: %s: %s", name, payload)
		}

		if i == 1 {
			// Inspection does not ack. Restart/attach keeps the same subscription and message.
			if _, err := p.Ensure(ctx, specs[0]); err != nil {
				t.Fatal(err)
			}

			if err := p.call(ctx, "POST", name+":modifyAckDeadline", map[string]any{"ackIds": []string{msg.Ack}, "ackDeadlineSeconds": 0}, nil); err != nil {
				t.Fatal(err)
			}

			messages = nil
			deadline = time.Now().Add(15 * time.Second)
			for len(messages) == 0 && time.Now().Before(deadline) {
				messages = pull(name)
			}

			if len(messages) != 1 || messages[0].Message.Data != msg.Message.Data {
				t.Fatal("consumer attached after inspection lost backlog")
			}

			msg = messages[0]
		}

		if err := p.call(ctx, "POST", name+":acknowledge", map[string]any{"ackIds": []string{msg.Ack}}, nil); err != nil {
			t.Fatal(err)
		}
	}

	if err := p.Validate(ctx, b); err != nil {
		t.Fatal(err)
	}

	// Attributes cannot bypass a schema enforced by the shared topic.
	schema := prefix + "/schemas/envy-acceptance-" + id
	schemaTopic := topic + "-schema"
	t.Cleanup(func() { cleanup("DELETE", schema) })
	if err := p.call(ctx, "POST", prefix+"/schemas?schemaId=envy-acceptance-"+id, map[string]string{"type": "AVRO", "definition": `{"type":"record","name":"Order","fields":[{"name":"amount","type":"int"}]}`}, nil); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { cleanup("DELETE", schemaTopic) })
	if err := p.call(ctx, "PUT", schemaTopic, map[string]any{"name": schemaTopic, "schemaSettings": map[string]string{"schema": schema, "encoding": "JSON"}}, nil); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		data  string
		valid bool
	}{{`{"amount":1}`, true}, {`{"amount":"incompatible"}`, false}} {
		err := p.call(ctx, "POST", schemaTopic+":publish", map[string]any{"messages": []any{map[string]any{"data": base64.StdEncoding.EncodeToString([]byte(test.data)), "attributes": map[string]string{"envy_composition": id}}}}, nil)
		if test.valid && err != nil {
			t.Fatalf("valid schema publish: %v", err)
		}

		if !test.valid && !errors.Is(err, apiError(http.StatusBadRequest)) {
			t.Fatalf("schema boundary not enforced: %v", err)
		}
	}

	if endpoint == "" {
		name := specs[0].Subscriptions[0].Name
		var permissions struct {
			Permissions []string `json:"permissions"`
		}
		if err := p.call(ctx, "POST", name+":testIamPermissions", map[string]any{"permissions": []string{"pubsub.subscriptions.get", "pubsub.subscriptions.delete", "pubsub.subscriptions.consume"}}, &permissions); err != nil || len(permissions.Permissions) != 3 {
			t.Fatalf("acceptance caller lacks required subscription permissions: %v", err)
		}

		t.Log("Real GCP filters, retention/expiration configuration, IAM and capture/attach verified")
	} else {
		t.Log("Pinned emulator filter support and capture/attach verified; IAM and retention need real GCP")
	}

	for _, spec := range specs {
		if absent, err := p.Delete(ctx, spec); err != nil || !absent {
			t.Fatalf("owned cleanup: %v", err)
		}
	}
}

func TestConsumerHTTPPublisherPropagation(t *testing.T) {
	publisher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inbound := r.Header.Get("baggage")
		attributes, err := fixtureAttributes(&inbound, "", false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_ = json.NewEncoder(w).Encode(attributes)
	}))
	defer publisher.Close()
	// The consumer restores routing context from the delivered Pub/Sub attribute,
	// then calls an inherited publisher over HTTP rather than deploying that publisher.
	delivered := map[string]string{"envy_composition": "preview-from-message"}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, publisher.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("baggage", "composition="+delivered["envy_composition"]+",envy_message_isolation=true")
	response, err := publisher.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()
	var attributes map[string]string
	if err = json.NewDecoder(response.Body).Decode(&attributes); err != nil || attributes["envy_composition"] != delivered["envy_composition"] {
		t.Fatal("consumer HTTP continuation lost routing context")
	}
}

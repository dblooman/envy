package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dblooman/envy/internal/domain"
)

type broker struct {
	topicTransforms []map[string]any
	subscriptions   map[string]subscription
	writes          int
	deny            bool
}

func (b *broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if b.deny {
		w.WriteHeader(403)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	if strings.Contains(path, "/topics/") {
		if strings.HasSuffix(path, "/subscriptions") {
			names := []string{}
			topic := strings.TrimSuffix(path, "/subscriptions")
			for name, s := range b.subscriptions {
				if s.Topic == topic {
					names = append(names, name)
				}
			}

			_ = json.NewEncoder(w).Encode(map[string]any{"subscriptions": names})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"name": path, "messageTransforms": b.topicTransforms})
		}

		return
	}

	switch r.Method {
	case "GET":
		s, ok := b.subscriptions[path]
		if !ok {
			w.WriteHeader(404)
			return
		}

		_ = json.NewEncoder(w).Encode(s)
	case "PUT":
		if _, ok := b.subscriptions[path]; ok {
			w.WriteHeader(409)
			return
		}

		var s subscription
		_ = json.NewDecoder(r.Body).Decode(&s)
		b.subscriptions[path] = s
		b.writes++
		_ = json.NewEncoder(w).Encode(s)
	case "DELETE":
		delete(b.subscriptions, path)
		b.writes++
		_, _ = w.Write([]byte(`{}`))
	default:
		w.WriteHeader(405)
	}
}

func fixture(t *testing.T) (*Provider, *broker, domain.Baseline, domain.MessagingSpec) {
	t.Helper()
	b := domain.Baseline{Components: map[string]domain.BaselineBinding{"publisher": {}, "worker": {}}, PubSub: map[string]domain.PubSubTopic{"orders": {Topic: "projects/test-project/topics/orders", Publishers: []string{"publisher"}, Consumers: map[string]domain.PubSubConsumer{"billing": {Subscription: "projects/test-project/subscriptions/billing", Component: "worker", SubscriptionEnv: "ORDERS_SUBSCRIPTION", Filter: `attributes.kind = "order"`}}}}}
	broker := &broker{subscriptions: map[string]subscription{}}
	c := b.PubSub["orders"].Consumers["billing"]
	broker.subscriptions[c.Subscription] = subscription{Name: c.Subscription, Topic: b.PubSub["orders"].Topic, Filter: domain.MessageFilter(domain.BaselineMessageFilter, c.Filter)}
	server := httptest.NewServer(broker)
	t.Cleanup(server.Close)
	p := &Provider{client: server.Client(), endpoint: server.URL + "/v1/", installation: "test", guard: func(context.Context) error { return nil }}
	spec := domain.MessagingSpec{CompositionID: strings.Repeat("a", 24), OwnershipToken: "owner", Subscriptions: domain.ResolveMessaging(b, "test", strings.Repeat("a", 24), time.Now().Add(time.Hour))}
	return p, broker, b, spec
}

func TestProvisionObserveRestartAndCleanup(t *testing.T) {
	p, b, baseline, spec := fixture(t)
	ctx := context.Background()
	if err := p.Validate(ctx, baseline); err != nil {
		t.Fatal(err)
	}

	got, err := p.Ensure(ctx, spec)
	if err != nil || len(got) != 1 || !got[0].Ready {
		t.Fatalf("ensure: %+v %v", got, err)
	}

	if b.writes != 1 {
		t.Fatal("wrong create count")
	}

	if err := p.Validate(ctx, baseline); err != nil {
		t.Fatalf("own preview rejected by inventory: %v", err)
	}

	restarted := *p
	spec.Subscriptions = got
	if _, err = restarted.Ensure(ctx, spec); err != nil || b.writes != 1 {
		t.Fatalf("restart recreated subscription: %v", err)
	}

	// A second preview gets an independent backlog and predicate.
	other := spec
	other.CompositionID = strings.Repeat("b", 24)
	other.OwnershipToken = "other"
	other.Subscriptions = domain.ResolveMessaging(baseline, "test", other.CompositionID, time.Now().Add(time.Hour))
	if _, err = p.Ensure(ctx, other); err != nil {
		t.Fatal(err)
	}

	if other.Subscriptions[0].Name == got[0].Name || other.Subscriptions[0].Filter == got[0].Filter {
		t.Fatal("previews share subscription or predicate")
	}

	if absent, err := p.Delete(ctx, spec); err != nil || !absent {
		t.Fatalf("cleanup: %v", err)
	}

	if _, ok := b.subscriptions[other.Subscriptions[0].Name]; !ok {
		t.Fatal("deleted another preview")
	}

	if _, ok := b.subscriptions[baseline.PubSub["orders"].Consumers["billing"].Subscription]; !ok {
		t.Fatal("deleted baseline")
	}

	if absent, err := p.Delete(ctx, spec); err != nil || !absent {
		t.Fatal("cleanup retry failed")
	}
}

func TestUnsafeBaselineAndUnexpectedSubscribers(t *testing.T) {
	for _, change := range []string{"baseline", "unexpected", "or-bypass", "foreign-preview", "wrong-topic", "topic-transform", "baseline-transform"} {
		t.Run(change, func(t *testing.T) {
			p, b, baseline, spec := fixture(t)
			ctx := context.Background()
			name := baseline.PubSub["orders"].Consumers["billing"].Subscription
			switch change {
			case "topic-transform":
				b.topicTransforms = []map[string]any{{"javascriptUdf": map[string]string{"code": "transform"}}}
			case "baseline-transform":
				s := b.subscriptions[name]
				s.Transforms = []map[string]any{{"javascriptUdf": map[string]string{"code": "transform"}}}
				b.subscriptions[name] = s
			case "baseline":
				s := b.subscriptions[name]
				s.Filter = ""
				b.subscriptions[name] = s
			case "wrong-topic":
				s := b.subscriptions[name]
				s.Topic = "projects/test-project/topics/other"
				b.subscriptions[name] = s
			default:
				s := subscription{Name: name + "-other", Topic: baseline.PubSub["orders"].Topic}
				if change == "or-bypass" {
					s.Filter = domain.BaselineMessageFilter + " OR attributes.all = \"yes\""
				}

				if change == "foreign-preview" {
					s.Filter = spec.Subscriptions[0].Filter
					s.Labels = map[string]string{"envy-installation": "foreign"}
				}

				b.subscriptions[s.Name] = s
			}

			if err := p.Validate(ctx, baseline); err == nil {
				t.Fatal("unsafe subscription accepted")
			}

			if b.writes != 0 {
				t.Fatal("onboarding mutated baseline")
			}
		})
	}
}

func TestOwnershipDriftMissingBacklogAndLeadership(t *testing.T) {
	ctx := context.Background()
	for _, change := range []string{"owner", "filter", "retention", "expiration", "push", "deadletter", "missing", "guard", "denied", "detached", "transform", "bigtable"} {
		t.Run(change, func(t *testing.T) {
			p, b, _, spec := fixture(t)
			got, err := p.Ensure(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}

			spec.Subscriptions = got
			name := got[0].Name
			s := b.subscriptions[name]
			switch change {
			case "owner":
				s.Labels["envy-owner"] = "other"
			case "detached":
				s.Detached = true
			case "transform":
				s.Transforms = []map[string]any{{"javascriptUdf": map[string]string{"code": "transform"}}}
			case "bigtable":
				s.BigTable = map[string]any{"table": "unexpected"}
			case "filter":
				s.Filter = ""
			case "retention":
				s.Retention = "600s"
			case "expiration":
				s.Expiration = map[string]string{"ttl": "86400s"}
			case "push":
				s.Push = map[string]any{"pushEndpoint": "https://example.com"}
			case "deadletter":
				s.DeadLetter = map[string]any{"deadLetterTopic": "projects/test-project/topics/dlq"}
			case "guard":
				p = p.WithGuard(func(context.Context) error { return domain.ErrNotLeader })
			case "denied":
				b.deny = true
			}

			b.subscriptions[name] = s
			if change == "missing" || change == "guard" {
				delete(b.subscriptions, name)
			}

			before := b.writes
			observed, err := p.Ensure(ctx, spec)
			if change == "missing" {
				if err != nil || !observed[0].BacklogMayBeLost || !observed[0].Ready {
					t.Fatalf("lost backlog unreported: %+v %v", observed, err)
				}

				return
			}

			if err == nil {
				t.Fatal("unsafe state accepted")
			}

			if change == "guard" && !errors.Is(err, domain.ErrNotLeader) {
				t.Fatal(err)
			}

			if b.writes != before {
				t.Fatal("unsafe state caused mutation")
			}

			if change == "owner" {
				if _, err = p.Delete(ctx, spec); err == nil {
					t.Fatal("foreign subscription deleted")
				}
			}
		})
	}
}

func TestRecreationCrashStillReportsBacklogLoss(t *testing.T) {
	p, b, _, spec := fixture(t)
	ctx := context.Background()
	original, err := p.Ensure(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	spec.Subscriptions = original
	delete(b.subscriptions, original[0].Name)
	if _, err = p.Ensure(ctx, spec); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash after broker creation but before the new observation commits.
	recovered, err := p.Ensure(ctx, spec)
	if err != nil || !recovered[0].BacklogMayBeLost || recovered[0].Instance == original[0].Instance {
		t.Fatalf("crash hid backlog loss: %+v %v", recovered, err)
	}
}

package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func messagingBaseline() Baseline {
	return Baseline{Components: map[string]BaselineBinding{"producer": {}, "consumer": {}}, PubSub: map[string]PubSubTopic{"events": {Topic: "projects/test-project/topics/events", Publishers: []string{"producer"}, Consumers: map[string]PubSubConsumer{"one": {Subscription: "projects/test-project/subscriptions/events", Component: "consumer", SubscriptionEnv: "EVENTS_SUB", Filter: `attributes.kind = "event"`}}}}}
}

func TestMessagingPlanAndBindings(t *testing.T) {
	b := messagingBaseline()
	if err := ValidateMessaging(b); err != nil {
		t.Fatal(err)
	}

	expiry := time.Now().Add(time.Hour)
	a := ResolveMessaging(b, "installation", "preview-a", expiry)
	if !reflect.DeepEqual(a, ResolveMessaging(b, "installation", "preview-a", expiry)) {
		t.Fatal("unstable names")
	}

	if a[0].Name == ResolveMessaging(b, "other", "preview-a", expiry)[0].Name {
		t.Fatal("installation collision")
	}

	if a[0].Ready || a[0].Retention != "604800s" || !a[0].ExpiresAt.Equal(expiry) {
		t.Fatal("wrong initial state")
	}

	if a[0].Filter != `(attributes.envy_composition = "preview-a") AND (attributes.kind = "event")` {
		t.Fatal(a[0].Filter)
	}

	if env := MessagingEnvironment(b, "consumer", false, a); env["EVENTS_SUB"] != "" || env["ENVY_MESSAGE_ISOLATION"] != "false" {
		t.Fatal("baseline subscription inherited")
	}

	if env := MessagingEnvironment(b, "consumer", true, a); env["EVENTS_SUB"] != a[0].Name {
		t.Fatal("missing consumer binding")
	}

	if env := MessagingEnvironment(b, "producer", true, a); len(env) != 1 {
		t.Fatal("producer received consumer configuration")
	}

	var old Composition
	if err := json.Unmarshal([]byte(`{"id":"old"}`), &old); err != nil || old.MessageIsolation {
		t.Fatal("legacy composition enabled isolation")
	}
}

func TestMessagingRegistrationRejectsAmbiguity(t *testing.T) {
	for _, which := range []string{"topic", "component", "env", "reserved", "filter", "length", "duplicate", "escape"} {
		t.Run(which, func(t *testing.T) {
			b := messagingBaseline()
			topic := b.PubSub["events"]
			c := topic.Consumers["one"]
			switch which {
			case "topic":
				topic.Topic = "https://elsewhere/topics/test"
			case "component":
				c.Component = "missing"
			case "env":
				c.SubscriptionEnv = ""
			case "reserved":
				c.SubscriptionEnv = "ENVY_COMPOSITION_ID"
			case "filter":
				c.Filter = `NOT attributes:envy_composition`
			case "escape":
				c.Filter = `attributes.kind = "order") OR (attributes.kind = "other"`
			case "length":
				c.Filter = strings.Repeat("x", 256)
			case "duplicate":
				topic.Consumers["two"] = c
			}

			topic.Consumers["one"] = c
			b.PubSub["events"] = topic
			if ValidateMessaging(b) == nil {
				t.Fatal("invalid registration accepted")
			}
		})
	}
}

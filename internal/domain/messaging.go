package domain

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	MessageRetention      = 7 * 24 * time.Hour
	BaselineMessageFilter = "NOT attributes:envy_composition"
)

type PubSubTopic struct {
	Topic      string                    `json:"topic"`
	Publishers []string                  `json:"publishers"`
	Consumers  map[string]PubSubConsumer `json:"consumers"`
}
type PubSubConsumer struct {
	Subscription    string `json:"subscription"`
	Component       string `json:"component"`
	SubscriptionEnv string `json:"subscription_env"`
	Filter          string `json:"filter,omitempty"`
}
type MessageSubscription struct {
	Instance         string    `json:"instance,omitempty"`
	Name             string    `json:"name"`
	Topic            string    `json:"topic"`
	Component        string    `json:"component"`
	SubscriptionEnv  string    `json:"subscription_env"`
	Filter           string    `json:"filter"`
	Retention        string    `json:"retention"`
	ExpiresAt        time.Time `json:"expires_at"`
	Ready            bool      `json:"ready"`
	BacklogMayBeLost bool      `json:"backlog_may_be_lost,omitempty"`
}
type MessagingSpec struct {
	CompositionID, OwnershipToken string
	Subscriptions                 []MessageSubscription
}
type MessagingProvider interface {
	Validate(context.Context, Baseline) error
	Ensure(context.Context, MessagingSpec) ([]MessageSubscription, error)
	Delete(context.Context, MessagingSpec) (bool, error)
}

var (
	pubsubResource = regexp.MustCompile(`^projects/[a-zA-Z0-9][a-zA-Z0-9.:-]*/(topics|subscriptions)/[a-zA-Z][a-zA-Z0-9._~+%-]{2,254}$`)
	messageEnv     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

func MessageFilter(base, business string) string {
	if business == "" {
		return base
	}

	return "(" + base + ") AND (" + business + ")"
}

func ValidateMessaging(b Baseline) error {
	if len(b.PubSub) > 20 {
		return Validation("at most twenty Pub/Sub topics are supported")
	}

	state := messagingValidation{topics: map[string]bool{}, subscriptions: map[string]bool{}, environments: map[string]bool{}}
	for name, topic := range b.PubSub {
		if err := state.validateTopic(b, name, topic); err != nil {
			return err
		}
	}

	if state.consumerCount > 40 {
		return Validation("at most forty Pub/Sub consumer bindings are supported")
	}

	return nil
}

type messagingValidation struct {
	topics, subscriptions, environments map[string]bool
	consumerCount                       int
}

func (state *messagingValidation) validateTopic(b Baseline, name string, topic PubSubTopic) error {
	if !ValidCatalogID(name) || !pubsubResource.MatchString(topic.Topic) || !strings.Contains(topic.Topic, "/topics/") || state.topics[topic.Topic] {
		return Validation("Pub/Sub topics require unique fully qualified topic names and logical DNS IDs")
	}

	state.topics[topic.Topic] = true
	if len(topic.Publishers) == 0 || len(topic.Consumers) == 0 {
		return Validation("Pub/Sub topics require publishers and consumer bindings")
	}

	if err := validatePublishers(b, topic.Publishers); err != nil {
		return err
	}

	for id, consumer := range topic.Consumers {
		if err := state.validateConsumer(b, id, consumer); err != nil {
			return err
		}
	}

	return nil
}

func validatePublishers(b Baseline, publishers []string) error {
	seen := map[string]bool{}
	for _, publisher := range publishers {
		if _, ok := b.Components[publisher]; !ok || seen[publisher] {
			return Validation("Pub/Sub publishers must be distinct bound components")
		}

		seen[publisher] = true
	}

	return nil
}

func (state *messagingValidation) validateConsumer(b Baseline, id string, consumer PubSubConsumer) error {
	state.consumerCount++
	if !ValidCatalogID(id) || !pubsubResource.MatchString(consumer.Subscription) || !strings.Contains(consumer.Subscription, "/subscriptions/") || state.subscriptions[consumer.Subscription] {
		return Validation("Pub/Sub consumers require unique fully qualified subscription names and logical DNS IDs")
	}

	state.subscriptions[consumer.Subscription] = true
	if _, ok := b.Components[consumer.Component]; !ok {
		return Validation("Pub/Sub consumer must be a bound component")
	}

	key := consumer.Component + "/" + consumer.SubscriptionEnv
	if !messageEnv.MatchString(consumer.SubscriptionEnv) || strings.HasPrefix(consumer.SubscriptionEnv, "ENVY_") || consumer.SubscriptionEnv == "POD_UID" || state.environments[key] {
		return Validation("Pub/Sub subscription environment keys must be valid, nonreserved and unique per component")
	}

	state.environments[key] = true
	if !balancedFilter(consumer.Filter) || strings.Contains(consumer.Filter, "envy_composition") || len(MessageFilter(`attributes.envy_composition = "00000000000000000000000000000000"`, consumer.Filter)) > 256 || strings.ContainsAny(consumer.Filter, "\r\n\x00") {
		return Validation("Pub/Sub business filter is invalid or exceeds the 256-byte composed filter limit")
	}

	return nil
}

func ResolveMessaging(b Baseline, installation, id string, expiry time.Time) []MessageSubscription {
	out := []MessageSubscription{}
	for name, t := range b.PubSub {
		for consumer, c := range t.Consumers {
			hash := sha256.Sum256([]byte(installation + "/" + id + "/" + name + "/" + consumer))
			project := strings.Split(t.Topic, "/")[1]
			out = append(out, MessageSubscription{Name: fmt.Sprintf("projects/%s/subscriptions/envy-%x", project, hash[:20]), Topic: t.Topic, Component: c.Component, SubscriptionEnv: c.SubscriptionEnv, Filter: MessageFilter(fmt.Sprintf("attributes.envy_composition = %q", id), c.Filter), Retention: "604800s", ExpiresAt: expiry})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// MessagingEnvironment overrides copied baseline subscription bindings even when isolation is off.
// A conforming consumer treats an empty subscription as disabled, never as baseline fallback.
func MessagingEnvironment(b Baseline, component string, isolated bool, subscriptions []MessageSubscription) map[string]string {
	env := map[string]string{"ENVY_MESSAGE_ISOLATION": fmt.Sprint(isolated)}
	for _, t := range b.PubSub {
		for _, c := range t.Consumers {
			if c.Component == component {
				env[c.SubscriptionEnv] = ""
			}
		}
	}

	if isolated {
		for _, s := range subscriptions {
			if s.Component == component {
				env[s.SubscriptionEnv] = s.Name
			}
		}
	}

	return env
}

// Prevent a business predicate from escaping the enclosing isolation conjunction.
// Pub/Sub validates the remaining grammar on the operator-prepared subscription.
func balancedFilter(filter string) bool {
	depth := 0
	quoted := false
	escaped := false
	for _, r := range filter {
		if quoted {
			if escaped {
				escaped = false
				continue
			}

			if r == '\\' {
				escaped = true
				continue
			}

			if r == '"' {
				quoted = false
			}

			continue
		}

		switch r {
		case '"':
			quoted = true
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}

	return depth == 0 && !quoted
}

package domain

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type Principal struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
}

type RequestIdentity struct {
	Principal Principal `json:"principal"`
	Channel   string    `json:"channel"`
	Task      string    `json:"task,omitempty"`
}

type identityContextKey struct{}

func WithRequestIdentity(ctx context.Context, identity RequestIdentity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func RequestIdentityFromContext(ctx context.Context) RequestIdentity {
	if identity, ok := ctx.Value(identityContextKey{}).(RequestIdentity); ok {
		return identity
	}

	return RequestIdentity{Principal: Principal{Kind: "unknown", ID: "unknown"}, Channel: "unknown"}
}

type Activity struct {
	ID             string          `json:"id"`
	OccurredAt     time.Time       `json:"occurred_at"`
	Actor          Principal       `json:"actor"`
	Channel        string          `json:"channel"`
	Task           string          `json:"task,omitempty"`
	Action         string          `json:"action"`
	Outcome        string          `json:"outcome"`
	Project        string          `json:"project,omitempty"`
	ResourceType   string          `json:"resource_type"`
	ResourceID     string          `json:"resource_id"`
	Composition    string          `json:"composition,omitempty"`
	Operation      string          `json:"operation,omitempty"`
	GenerationFrom int64           `json:"generation_from,omitempty"`
	GenerationTo   int64           `json:"generation_to,omitempty"`
	Changes        json.RawMessage `json:"changes,omitempty"`
}

type ActivityFilter struct {
	After, Project, Actor, Action, Outcome, ResourceType, ResourceID string
	From, To                                                         time.Time
	Limit                                                            int
}

type ActivityPage struct {
	Items      []Activity `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type CompositionRevision struct {
	Composition      string                       `json:"composition"`
	Project          string                       `json:"project"`
	Generation       int64                        `json:"generation"`
	Baseline         string                       `json:"baseline"`
	BaselineRevision string                       `json:"baseline_revision"`
	Overrides        map[string]ComponentOverride `json:"overrides"`
	CreatedAt        time.Time                    `json:"created_at"`
	Actor            Principal                    `json:"actor"`
	Channel          string                       `json:"channel"`
	Operation        string                       `json:"operation,omitempty"`
}

type RevisionsPage struct {
	Items      []CompositionRevision `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

func ValidChannel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "web", "cli", "mcp", "github", "api":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "api"
	}
}

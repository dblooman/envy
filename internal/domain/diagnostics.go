package domain

import (
	"context"
	"strconv"
	"time"
)

type LogOptions struct {
	TailLines    int64 `json:"tail_lines"`
	MaxBytes     int64 `json:"max_bytes"`
	SinceSeconds int64 `json:"since_seconds,omitempty"`
	Previous     bool  `json:"previous"`
}

func NormalizeLogOptions(o LogOptions) (LogOptions, error) {
	if o.TailLines == 0 {
		o.TailLines = 200
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 65536
	}
	if o.TailLines < 1 || o.TailLines > 1000 {
		return o, Validation("tail_lines must be between 1 and 1000")
	}
	if o.MaxBytes < 1 || o.MaxBytes > 262144 {
		return o, Validation("max_bytes must be between 1 and 262144")
	}
	if o.SinceSeconds < 0 || o.SinceSeconds > 86400 {
		return o, Validation("since_seconds must be between 1 and 86400, or omitted")
	}
	return o, nil
}
func EventCursor(after string) (int64, error) {
	if after == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(after, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != after {
		return 0, Validation("invalid event cursor")
	}
	return n, nil
}

type LifecycleEvent struct {
	ID          string      `json:"id"`
	Composition string      `json:"composition"`
	Project     string      `json:"project"`
	Generation  int64       `json:"generation"`
	Type        string      `json:"type"`
	Phase       Phase       `json:"phase"`
	OccurredAt  time.Time   `json:"occurred_at"`
	Operation   Operation   `json:"operation"`
	Conditions  []Condition `json:"conditions"`
	Error       *Error      `json:"error,omitempty"`
}
type EventsPage struct {
	Items      []LifecycleEvent `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
type LogTarget struct {
	Composition, Project, Component, Source, BaselineServiceHost string
	Workload                                                     WorkloadRef
}
type LogStream struct {
	Pod        string `json:"pod"`
	WorkloadID string `json:"workload_id"`
	Container  string `json:"container"`
	Text       string `json:"text"`
	Truncated  bool   `json:"truncated"`
	Error      *Error `json:"error,omitempty"`
}
type ComponentLogs struct {
	ID                  string      `json:"id"`
	Project             string      `json:"project"`
	Component           string      `json:"component"`
	Source              string      `json:"source"`
	CompositionFiltered bool        `json:"composition_filtered"`
	Message             string      `json:"message"`
	Streams             []LogStream `json:"streams"`
	Truncated           bool        `json:"truncated"`
	Partial             bool        `json:"partial"`
}
type LogReader interface {
	ReadLogs(context.Context, LogTarget, LogOptions) (ComponentLogs, error)
}

package eventsourcing

import (
	"context"
	"iter"
	"time"
)

type ReadOptions struct {
	TenantID       string
	AfterID        string
	Types          []string
	OccurredAfter  time.Time
	OccurredBefore time.Time
	Limit          int
}

type EventStore interface {
	Append(ctx context.Context, e Event) error
	AppendBatch(ctx context.Context, events []Event) error
	Read(ctx context.Context, opts ReadOptions) iter.Seq2[Event, error]
	Count(ctx context.Context, opts ReadOptions) (int64, error)
	Close() error
}

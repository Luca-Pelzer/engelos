// Package usage records in-process AI backend usage counters (calls, ok/error,
// tokens in/out and cache reads) broken down by consumer, and provides the
// plumbing to collect token counts from the wire clients.
//
// It is deliberately a stdlib-only leaf: the anthropic and openai clients import
// it to report per-response token usage via [Record], and the aibackend
// selector/manager wrap the backend handed to each consumer with [Wrap] to
// attach a label. Nothing here is persisted: all counters live in memory and
// reset when the daemon restarts.
package usage

import (
	"context"
	"sync"
	"time"
)

// Usage is the token accounting extracted from a single provider response.
// CacheReadTokens is the subset of input tokens served from a prompt cache
// (Anthropic reports it; OpenAI leaves it zero).
type Usage struct {
	InputTokens     int
	OutputTokens    int
	CacheReadTokens int
}

// --- context sink: carries per-call token usage from the wire client back up
// to the labeling wrapper without changing the Backend method signatures. ---

type ctxKey struct{}

// Sink accumulates the token usage reported during one backend call. It is
// created by [Wrap] (via [NewContext]) and written by the wire client (via
// [Record]). It is safe for concurrent writes.
type Sink struct {
	mu sync.Mutex
	u  Usage
}

// NewContext attaches a fresh [Sink] to ctx and returns both. Wire clients that
// find the sink in the context report their token usage to it via [Record].
func NewContext(ctx context.Context) (context.Context, *Sink) {
	s := &Sink{}
	return context.WithValue(ctx, ctxKey{}, s), s
}

// Record adds u to the [Sink] carried by ctx, if any. It is a no-op when no sink
// is attached (for example when a client is used directly, outside a [Wrap]),
// so it is always safe for a wire client to call.
func Record(ctx context.Context, u Usage) {
	s, _ := ctx.Value(ctxKey{}).(*Sink)
	if s == nil {
		return
	}
	s.mu.Lock()
	s.u.InputTokens += u.InputTokens
	s.u.OutputTokens += u.OutputTokens
	s.u.CacheReadTokens += u.CacheReadTokens
	s.mu.Unlock()
}

func (s *Sink) total() Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.u
}

// --- registry: the in-memory counter set. ---

type counter struct {
	calls, ok, errors              int64
	tokensIn, tokensOut, cacheRead int64
}

func (c *counter) add(ok bool, u Usage) {
	c.calls++
	if ok {
		c.ok++
	} else {
		c.errors++
	}
	c.tokensIn += int64(u.InputTokens)
	c.tokensOut += int64(u.OutputTokens)
	c.cacheRead += int64(u.CacheReadTokens)
}

// Registry holds the process-lifetime AI usage counters: a grand total plus a
// per-consumer breakdown. It is safe for concurrent use. All counts are
// in-memory only and reset on restart.
type Registry struct {
	mu      sync.Mutex
	since   time.Time
	total   counter
	byLabel map[string]*counter
}

// NewRegistry returns an empty Registry stamped with the current time as its
// "since" marker.
func NewRegistry() *Registry {
	return &Registry{since: time.Now().UTC(), byLabel: make(map[string]*counter)}
}

// observe records one completed backend call under label, updating both the
// grand total and the per-label breakdown.
func (r *Registry) observe(label string, ok bool, u Usage) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total.add(ok, u)
	c := r.byLabel[label]
	if c == nil {
		c = &counter{}
		r.byLabel[label] = c
	}
	c.add(ok, u)
}

// ConsumerSnapshot is one consumer's slice of the usage counters.
type ConsumerSnapshot struct {
	Calls     int64 `json:"calls"`
	OK        int64 `json:"ok"`
	Errors    int64 `json:"errors"`
	TokensIn  int64 `json:"tokens_in"`
	TokensOut int64 `json:"tokens_out"`
}

// Snapshot is the serialisable view of the registry for GET /api/v1/ai/usage.
type Snapshot struct {
	Since           time.Time                   `json:"since"`
	Calls           int64                       `json:"calls"`
	OK              int64                       `json:"ok"`
	Errors          int64                       `json:"errors"`
	TokensIn        int64                       `json:"tokens_in"`
	TokensOut       int64                       `json:"tokens_out"`
	CacheReadTokens int64                       `json:"cache_read_tokens"`
	ByConsumer      map[string]ConsumerSnapshot `json:"by_consumer"`
}

// Snapshot returns a consistent copy of the current counters. A nil Registry
// yields a zero-valued snapshot with an empty by_consumer map, so the API
// handler needs no nil special-casing.
func (r *Registry) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{ByConsumer: map[string]ConsumerSnapshot{}}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := Snapshot{
		Since:           r.since,
		Calls:           r.total.calls,
		OK:              r.total.ok,
		Errors:          r.total.errors,
		TokensIn:        r.total.tokensIn,
		TokensOut:       r.total.tokensOut,
		CacheReadTokens: r.total.cacheRead,
		ByConsumer:      make(map[string]ConsumerSnapshot, len(r.byLabel)),
	}
	for label, c := range r.byLabel {
		out.ByConsumer[label] = ConsumerSnapshot{
			Calls:     c.calls,
			OK:        c.ok,
			Errors:    c.errors,
			TokensIn:  c.tokensIn,
			TokensOut: c.tokensOut,
		}
	}
	return out
}

// --- labeling wrapper. ---

// Backend is the provider-neutral surface this package wraps. It matches
// aibackend.Backend structurally, so the manager (or any client) satisfies it
// without this package importing aibackend (keeping the dependency graph
// acyclic).
type Backend interface {
	Complete(ctx context.Context, systemPrompt, userText string) (string, error)
	Translate(ctx context.Context, text, targetLang string) (string, error)
}

// labeled wraps a Backend, tagging every call it makes with a consumer label
// and recording the call (and any token usage the wire client reports) into a
// Registry.
type labeled struct {
	inner Backend
	reg   *Registry
	label string
}

// Wrap returns a Backend that records each call it makes to inner under label
// in reg. A nil reg returns inner unchanged, so wrapping is free to skip when
// telemetry is not wired.
func Wrap(inner Backend, reg *Registry, label string) Backend {
	if reg == nil {
		return inner
	}
	return &labeled{inner: inner, reg: reg, label: label}
}

func (l *labeled) Complete(ctx context.Context, systemPrompt, userText string) (string, error) {
	ctx, sink := NewContext(ctx)
	out, err := l.inner.Complete(ctx, systemPrompt, userText)
	l.reg.observe(l.label, err == nil, sink.total())
	return out, err
}

func (l *labeled) Translate(ctx context.Context, text, targetLang string) (string, error) {
	ctx, sink := NewContext(ctx)
	out, err := l.inner.Translate(ctx, text, targetLang)
	l.reg.observe(l.label, err == nil, sink.total())
	return out, err
}

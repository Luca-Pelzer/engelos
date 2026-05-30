package contextmod

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Backend classifies a borderline chat message. The Claude client (injected by
// main) satisfies this; it is an interface here so the package stays decoupled
// and testable without HTTP.
type Backend interface {
	Complete(ctx context.Context, systemPrompt, userText string) (string, error)
}

// Verdict is the AI's classification of a borderline message.
type Verdict string

const (
	// VerdictAllow means the message is acceptable in context.
	VerdictAllow Verdict = "allow"
	// VerdictDelete means the message should be removed.
	VerdictDelete Verdict = "delete"
	// VerdictTimeout means the author should be timed out.
	VerdictTimeout Verdict = "timeout"
	// VerdictUnknown means the AI gave no usable answer; the caller should
	// fall back to its existing rules rather than act on this.
	VerdictUnknown Verdict = "unknown"
)

// Decision is the outcome of an escalation check.
type Decision struct {
	// Verdict is the AI classification.
	Verdict Verdict
	// Reason is a short human-readable justification for logs/audit.
	Reason string
	// Consulted reports whether the backend was actually called (false when
	// skipped by rate limit or because the message was not borderline).
	Consulted bool
}

// Options tunes the escalator's rate limits and the timeout it suggests.
type Options struct {
	// GlobalPerSecond caps backend calls per second across the channel.
	// <= 0 disables the limit. AI moderation calls cost money, so this should
	// stay low.
	GlobalPerSecond float64
	// TimeoutSeconds is the suggested timeout duration the caller may apply on
	// a timeout verdict. Default 600 (10 minutes) when <= 0.
	TimeoutSeconds int
}

// DefaultOptions returns conservative defaults: at most two AI checks per
// second and a 10-minute suggested timeout.
func DefaultOptions() Options {
	return Options{GlobalPerSecond: 2, TimeoutSeconds: 600}
}

// Escalator asks the Backend to classify borderline messages that the cheap
// rule-based AutoMod could not decide. It applies a global rate limit so a
// flood of borderline messages cannot run up unbounded AI calls, and it is
// fail-open: any backend error or unparseable answer yields VerdictUnknown so
// the caller keeps its existing behaviour. Safe for concurrent use.
type Escalator struct {
	backend Backend
	opts    Options

	mu      sync.Mutex
	global  *tokenBucket
	nowFunc func() time.Time
}

// NewEscalator builds an Escalator around backend with opts. A nil backend
// panics, since an escalator with nothing to call is a wiring error.
func NewEscalator(backend Backend, opts Options) *Escalator {
	if backend == nil {
		panic("contextmod: nil backend")
	}
	if opts.TimeoutSeconds <= 0 {
		opts.TimeoutSeconds = 600
	}
	e := &Escalator{backend: backend, opts: opts, nowFunc: time.Now}
	if opts.GlobalPerSecond > 0 {
		e.global = newTokenBucket(opts.GlobalPerSecond, opts.GlobalPerSecond)
	}
	return e
}

// TimeoutDuration returns the suggested timeout for a VerdictTimeout decision.
func (e *Escalator) TimeoutDuration() time.Duration {
	return time.Duration(e.opts.TimeoutSeconds) * time.Second
}

// Classify asks the backend whether text (in the channel's stated rules
// context) should be allowed, deleted, or met with a timeout. rules is the
// streamer's plain-language description of what is not allowed. An empty text
// or empty rules returns VerdictUnknown without calling the backend (there is
// nothing to judge against). A rate-limited call also returns VerdictUnknown.
func (e *Escalator) Classify(ctx context.Context, rules, username, text string) Decision {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(rules) == "" {
		return Decision{Verdict: VerdictUnknown}
	}
	if !e.allow() {
		return Decision{Verdict: VerdictUnknown}
	}
	out, err := e.backend.Complete(ctx, buildSystemPrompt(rules), text)
	if err != nil {
		return Decision{Verdict: VerdictUnknown, Reason: "backend error"}
	}
	v, reason := parseVerdict(out)
	return Decision{Verdict: v, Reason: reason, Consulted: true}
}

// allow consumes a token from the global limiter, returning false when the
// limit is hit.
func (e *Escalator) allow() bool {
	if e.global == nil {
		return true
	}
	now := e.nowFunc()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.global.allow(now)
}

// buildSystemPrompt instructs the model to answer with a single token verdict
// plus a short reason, given the channel's rules.
func buildSystemPrompt(rules string) string {
	return "You are a Twitch chat moderation assistant. The channel rules are: " + rules + "\n" +
		"Classify the user's message. Respond with EXACTLY one of these words on the first line: " +
		"ALLOW, DELETE, or TIMEOUT. On the same line after a space you may add a very short reason. " +
		"Use TIMEOUT only for clear, severe violations (slurs, threats, doxxing); use DELETE for milder " +
		"rule-breaking; use ALLOW when the message is acceptable or merely borderline. Do not explain further."
}

// parseVerdict extracts the leading verdict token and trailing reason from the
// model's reply. Anything unrecognised maps to VerdictUnknown so the caller
// fails open.
func parseVerdict(out string) (Verdict, string) {
	out = strings.TrimSpace(out)
	if out == "" {
		return VerdictUnknown, ""
	}
	// Consider only the first line.
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return VerdictUnknown, ""
	}
	token := strings.ToUpper(strings.Trim(fields[0], ".,:!"))
	reason := strings.TrimSpace(strings.TrimPrefix(out, fields[0]))
	switch token {
	case "ALLOW":
		return VerdictAllow, reason
	case "DELETE":
		return VerdictDelete, reason
	case "TIMEOUT":
		return VerdictTimeout, reason
	default:
		return VerdictUnknown, reason
	}
}

// tokenBucket is a minimal caller-locked token bucket mirroring the approach in
// internal/translate, avoiding a dependency on golang.org/x/time/rate.
type tokenBucket struct {
	ratePerSec float64
	burst      float64
	tokens     float64
	last       time.Time
}

func newTokenBucket(ratePerSec, burst float64) *tokenBucket {
	if burst < 1 {
		burst = 1
	}
	return &tokenBucket{ratePerSec: ratePerSec, burst: burst, tokens: burst}
}

func (b *tokenBucket) allow(now time.Time) bool {
	if b.last.IsZero() {
		b.last = now
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.ratePerSec
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

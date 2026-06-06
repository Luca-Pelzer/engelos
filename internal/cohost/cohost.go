package cohost

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Backend produces a co-host reply from a system prompt and the viewer's
// question. The Claude client (injected by main) satisfies this; it is an
// interface here so the orchestrator stays decoupled and testable without HTTP.
type Backend interface {
	Complete(ctx context.Context, systemPrompt, userText string) (string, error)
}

// Options tunes the responder's rate limits.
type Options struct {
	// PerUserEvery is the minimum spacing between answers for a single user.
	// Zero disables the per-user limit.
	PerUserEvery time.Duration
	// PerUserBurst is how many answers a user may get back-to-back before
	// PerUserEvery throttling applies. < 1 is treated as 1.
	PerUserBurst int
	// GlobalPerSecond caps total answers per second across the channel.
	// <= 0 disables the global limit.
	GlobalPerSecond float64
}

// DefaultOptions returns production defaults: one answer per user every 10s
// (burst 2) and at most one answer per second channel-wide.
func DefaultOptions() Options {
	return Options{
		PerUserEvery:    10 * time.Second,
		PerUserBurst:    2,
		GlobalPerSecond: 1,
	}
}

// Responder decides whether a chat message addresses the bot and, if so, asks
// the Backend for a reply, applying rate limits and a length cap. It is safe
// for concurrent use.
//
// It is best-effort and side-effect free: callers decide what to do with the
// returned reply. A backend error is returned so the caller can log it; on the
// dispatcher hot path the caller should swallow it so a co-host failure never
// blocks message handling.
type Responder struct {
	backend Backend
	opts    Options

	mu      sync.Mutex
	perUser map[string]*tokenBucket
	global  *tokenBucket
	nowFunc func() time.Time
}

// NewResponder builds a Responder around backend with opts. A nil backend
// panics, since a responder with nothing to call is a wiring error.
func NewResponder(backend Backend, opts Options) *Responder {
	if backend == nil {
		panic("cohost: nil backend")
	}
	if opts.PerUserBurst < 1 {
		opts.PerUserBurst = 1
	}
	r := &Responder{
		backend: backend,
		opts:    opts,
		perUser: make(map[string]*tokenBucket),
		nowFunc: time.Now,
	}
	if opts.GlobalPerSecond > 0 {
		r.global = newTokenBucket(opts.GlobalPerSecond, opts.GlobalPerSecond)
	}
	return r
}

// Respond decides whether text (from userID) addresses the bot named by
// cfg.BotName and, if so, returns the co-host reply. answered reports whether
// a reply was produced; when false reply is empty and err is nil (the message
// was not addressed, was rate-limited, or produced nothing).
//
// A non-nil err means the backend call failed; reply is empty and answered is
// false.
func (r *Responder) Respond(ctx context.Context, cfg Config, userID, username, text string) (reply string, answered bool, err error) {
	question, ok := addressedQuestion(text, cfg.BotName)
	if !ok {
		return "", false, nil
	}
	if !r.allow(userID) {
		return "", false, nil
	}
	out, err := r.backend.Complete(ctx, buildSystemPrompt(cfg), question)
	if err != nil {
		return "", false, err
	}
	out = truncateRunes(strings.TrimSpace(out), cfg.MaxReplyLen)
	if out == "" {
		return "", false, nil
	}
	return out, true, nil
}

// allow consults the global then per-user rate limiter, consuming a token from
// each only when both permit. It returns false when either limit is hit.
func (r *Responder) allow(userID string) bool {
	now := r.nowFunc()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.global != nil && !r.global.allow(now) {
		return false
	}
	if r.opts.PerUserEvery > 0 && userID != "" {
		b := r.perUser[userID]
		if b == nil {
			rate := 1.0 / r.opts.PerUserEvery.Seconds()
			b = newTokenBucket(rate, float64(r.opts.PerUserBurst))
			r.perUser[userID] = b
		}
		if !b.allow(now) {
			return false
		}
	}
	return true
}

// addressedQuestion reports whether text addresses the bot and, if so, returns
// the question with the addressing prefix stripped. A message addresses the bot
// when it starts with "!ask " or with the bot name (optionally prefixed by '@'
// and followed by a separator). Matching is case-insensitive.
func addressedQuestion(text, botName string) (string, bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", false
	}
	lower := strings.ToLower(t)

	const askPrefix = "!ask "
	if strings.HasPrefix(lower, askPrefix) {
		q := strings.TrimSpace(t[len(askPrefix):])
		return q, q != ""
	}

	name := strings.ToLower(strings.TrimSpace(botName))
	if name == "" {
		return "", false
	}
	// Accept an optional leading '@' before the bot name.
	body := t
	if strings.HasPrefix(lower, "@") {
		body = t[1:]
		lower = lower[1:]
	}
	if !strings.HasPrefix(lower, name) {
		return "", false
	}
	rest := body[len(name):]
	// The bot name must be a whole token: the next rune (if any) is a
	// separator, not a letter, so "botanist" does not match bot "bot".
	if rest != "" {
		r := rest[0]
		if !(r == ' ' || r == ',' || r == ':' || r == '?' || r == '!' || r == '\t') {
			return "", false
		}
	}
	q := strings.TrimLeft(rest, " ,:?!\t")
	q = strings.TrimSpace(q)
	return q, q != ""
}

// buildSystemPrompt assembles the co-host system prompt from the channel's
// persona, bot name and reply-length cap.
func buildSystemPrompt(cfg Config) string {
	persona := cfg.Persona
	if persona == "" {
		persona = defaultPersona
	}
	name := cfg.BotName
	if name == "" {
		name = defaultBotName
	}
	limit := cfg.MaxReplyLen
	if limit <= 0 {
		limit = defaultMaxReplyLen
	}
	return fmt.Sprintf(
		"You are %s, %s, answering live in a Twitch chat. "+
			"Reply in at most %d characters, in plain text with no markdown, no preamble and no quotation marks. "+
			"Be helpful and stay in character. If you do not know, say so briefly.",
		name, persona, limit)
}

// truncateRunes shortens s to at most max runes (not bytes), trimming trailing
// space left by the cut. A non-positive max returns s unchanged.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimRight(string(runes[:max]), " ")
}

// tokenBucket is a minimal caller-locked token bucket. Tokens refill
// continuously at ratePerSec up to burst. It mirrors the approach in
// internal/translate to avoid a dependency on golang.org/x/time/rate.
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

// allow refills based on elapsed time since the last call and consumes one
// token, returning whether a token was available. The caller must serialise
// access (the Responder holds its mutex).
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

package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
	"unicode"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Backend performs the actual translation of a single message. The Claude
// client in the claude subpackage satisfies this; it is an interface here so
// the orchestrator stays decoupled and testable without HTTP.
type Backend interface {
	// Translate returns text rendered into targetLang (an ISO 639-1 code), or
	// the empty string when nothing was produced.
	Translate(ctx context.Context, text, targetLang string) (string, error)
}

// Result describes the outcome of a translation attempt for one message.
type Result struct {
	// Translated is the translated text. Empty when Skipped is true.
	Translated string
	// Skipped reports that no translation was performed (and why, via Reason).
	Skipped bool
	// Reason is a short machine-friendly skip reason for logging/metrics, for
	// example "command", "too-short", "already-target", "rate-limited" or
	// "cached-empty". Empty when a translation was produced.
	Reason string
	// Cached reports that Translated came from the in-memory cache rather than
	// a fresh backend call.
	Cached bool
}

// Options tunes the orchestrator's skip heuristics and rate limits. The zero
// value is usable; [DefaultOptions] supplies sensible production defaults.
type Options struct {
	// MinWords skips messages with fewer than this many words. Very short
	// chat lines ("lol", "gg") carry little to translate and waste calls.
	MinWords int
	// CacheSize is the number of recent (targetLang,text) translations to
	// remember. <= 0 disables caching.
	CacheSize int
	// PerUserEvery is the minimum spacing between translations for a single
	// user. Zero disables the per-user limit.
	PerUserEvery time.Duration
	// PerUserBurst is how many translations a user may make back-to-back
	// before PerUserEvery throttling applies. < 1 is treated as 1.
	PerUserBurst int
	// GlobalPerSecond caps total translations per second across the whole
	// channel/process. <= 0 disables the global limit.
	GlobalPerSecond float64
}

// DefaultOptions returns production defaults: skip messages under 2 words,
// cache 1024 recent translations, allow a small per-user burst then one
// translation every 3 seconds, and cap global throughput at 5/sec.
func DefaultOptions() Options {
	return Options{
		MinWords:        2,
		CacheSize:       1024,
		PerUserEvery:    3 * time.Second,
		PerUserBurst:    3,
		GlobalPerSecond: 5,
	}
}

// Translator orchestrates message translation: it applies cheap skip rules,
// a language pre-filter, an in-memory cache and rate limiting around a
// [Backend]. It is safe for concurrent use.
//
// It is intentionally best-effort and side-effect free: callers decide what to
// do with a [Result] (post to chat, reply, drop). A backend error is returned
// to the caller, which on the dispatcher hot path should be logged and
// swallowed so a translation failure never blocks message handling.
type Translator struct {
	backend Backend
	opts    Options

	cache *lru.Cache[string, string]

	mu      sync.Mutex
	perUser map[string]*tokenBucket
	global  *tokenBucket
	nowFunc func() time.Time
}

// New builds a Translator around backend with opts. A nil backend panics, since
// a translator with nothing to call is a programming error at wiring time.
func New(backend Backend, opts Options) *Translator {
	if backend == nil {
		panic("translate: nil backend")
	}
	if opts.PerUserBurst < 1 {
		opts.PerUserBurst = 1
	}
	t := &Translator{
		backend: backend,
		opts:    opts,
		perUser: make(map[string]*tokenBucket),
		nowFunc: time.Now,
	}
	if opts.CacheSize > 0 {
		// lru.New only errors on a non-positive size, which we guard above.
		c, _ := lru.New[string, string](opts.CacheSize)
		t.cache = c
	}
	if opts.GlobalPerSecond > 0 {
		t.global = newTokenBucket(opts.GlobalPerSecond, opts.GlobalPerSecond)
	}
	return t
}

// Translate runs the full pipeline for one chat message authored by userID and
// renders it into targetLang. It returns a [Result] describing what happened.
//
// Pipeline order (cheapest, most-likely-to-skip checks first):
//  1. blank / command / emote-or-url-only / too-short  -> skip, no cost
//  2. already in target language (script + whatlanggo)  -> skip, no cost
//  3. cache hit                                         -> return cached
//  4. global + per-user rate limits                     -> skip if exceeded
//  5. backend translation                               -> cache and return
//
// A non-nil error means the backend call failed; the returned Result is the
// zero Result. Callers on the dispatcher hot path should log and ignore it.
func (t *Translator) Translate(ctx context.Context, userID, text, targetLang string) (Result, error) {
	msg := strings.TrimSpace(text)
	if msg == "" {
		return Result{Skipped: true, Reason: "empty"}, nil
	}
	if isCommand(msg) {
		return Result{Skipped: true, Reason: "command"}, nil
	}
	if isTokensOnly(msg) {
		return Result{Skipped: true, Reason: "no-words"}, nil
	}
	if wordCount(msg) < t.opts.MinWords {
		return Result{Skipped: true, Reason: "too-short"}, nil
	}
	if AlreadyInTargetLang(msg, targetLang) {
		return Result{Skipped: true, Reason: "already-target"}, nil
	}

	key := cacheKey(targetLang, msg)
	if t.cache != nil {
		if cached, ok := t.cache.Get(key); ok {
			if cached == "" {
				return Result{Skipped: true, Reason: "cached-empty"}, nil
			}
			return Result{Translated: cached, Cached: true}, nil
		}
	}

	if !t.allow(userID) {
		return Result{Skipped: true, Reason: "rate-limited"}, nil
	}

	out, err := t.backend.Translate(ctx, msg, targetLang)
	if err != nil {
		return Result{}, err
	}
	out = strings.TrimSpace(out)
	if t.cache != nil {
		t.cache.Add(key, out)
	}
	if out == "" || strings.EqualFold(out, msg) {
		// Model returned nothing useful or echoed the input (already in
		// target). Treat as a skip so callers do not post a no-op line.
		return Result{Skipped: true, Reason: "no-change"}, nil
	}
	return Result{Translated: out}, nil
}

// allow consults the global then the per-user rate limiter, consuming a token
// from each only when both permit. It returns false when either limit is hit.
func (t *Translator) allow(userID string) bool {
	now := t.nowFunc()
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.global != nil && !t.global.allow(now) {
		return false
	}
	if t.opts.PerUserEvery > 0 && userID != "" {
		b := t.perUser[userID]
		if b == nil {
			rate := 1.0 / t.opts.PerUserEvery.Seconds()
			b = newTokenBucket(rate, float64(t.opts.PerUserBurst))
			t.perUser[userID] = b
		}
		if !b.allow(now) {
			return false
		}
	}
	return true
}

// --- skip helpers ---

// isCommand reports whether msg is a bot command invocation (leading '!').
func isCommand(msg string) bool {
	return strings.HasPrefix(msg, "!")
}

// wordCount counts whitespace-separated fields.
func wordCount(msg string) int {
	return len(strings.Fields(msg))
}

// isTokensOnly reports whether msg contains no letters at all, i.e. it is made
// up solely of punctuation, digits, URLs or emote names without alphabetic
// content worth translating.
func isTokensOnly(msg string) bool {
	for _, r := range msg {
		if unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// cacheKey derives a stable cache key from the target language and message
// text. The message is hashed so arbitrarily long lines map to a bounded key.
func cacheKey(targetLang, msg string) string {
	sum := sha256.Sum256([]byte(msg))
	return normalizeLangCode(targetLang) + ":" + hex.EncodeToString(sum[:8])
}

// --- token bucket ---

// tokenBucket is a minimal lock-free-internally (caller-locked) token bucket.
// Tokens refill continuously at ratePerSec up to burst. It avoids a dependency
// on golang.org/x/time/rate for a few lines of arithmetic.
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
// access (the Translator holds its mutex).
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

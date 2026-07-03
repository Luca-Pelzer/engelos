package contextmod

import (
	"context"
	"encoding/json"
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

// Category labels the kind of violation the AI detected. It feeds the policy
// layer so spam (hit-and-run) is never escalated to a ban ladder while severe
// harms (threats, doxxing) are.
type Category string

const (
	CategoryNone       Category = "none"
	CategorySpam       Category = "spam"
	CategoryHarassment Category = "harassment"
	CategoryHate       Category = "hate"
	CategoryThreat     Category = "threat"
	CategoryDoxxing    Category = "doxxing"
	CategorySexual     Category = "sexual"
	CategorySexism     Category = "sexism"
	CategoryOther      Category = "other"
)

var knownCategories = map[Category]bool{
	CategoryNone:       true,
	CategorySpam:       true,
	CategoryHarassment: true,
	CategoryHate:       true,
	CategoryThreat:     true,
	CategoryDoxxing:    true,
	CategorySexual:     true,
	CategorySexism:     true,
	CategoryOther:      true,
}

// Decision is the outcome of an escalation check. Verdict is kept equal to
// Action for backward compatibility with existing callers.
type Decision struct {
	Verdict    Verdict
	Action     Verdict
	Category   Category
	Severity   int
	Confidence float64
	Reason     string
	Consulted  bool
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
	// DedupTTLSeconds enables the duplicate-message cache when non-zero:
	// identical messages within this window reuse the first classification
	// instead of paying for another backend call. Zero disables the cache.
	DedupTTLSeconds int
	// DedupMaxEntries bounds the dedup cache size. <= 0 uses a sane default.
	DedupMaxEntries int
	// ContextSize is the number of recent chat messages per channel injected
	// into the classify prompt as rolling conversation context. 0 (the zero
	// value) disables the feature: [Escalator.ClassifyInChannel] then judges
	// each message single-turn, byte-for-byte identically to the pre-context
	// behaviour. [DefaultOptions] sets 30.
	ContextSize int
	// TenantID scopes the per-channel context window; it forms the (tenant,
	// channel) buffer key. Optional (empty is a valid single-tenant scope).
	TenantID string
	// BotUsername, when set, is skipped by [Escalator.Observe] so the bot's own
	// messages never enter the context window. Matching is case-insensitive.
	// Optional.
	BotUsername string
}

// DefaultOptions returns conservative defaults: at most two AI checks per
// second, a 10-minute suggested timeout, and a 30-message rolling chat-context
// window feeding the classify prompt.
func DefaultOptions() Options {
	return Options{GlobalPerSecond: 2, TimeoutSeconds: 600, DedupTTLSeconds: 45, ContextSize: defaultContextSize}
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
	dedup   *dedupCache

	ctxBuf   *contextBuffer // nil when the context window is disabled
	tenantID string
	botUser  string
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
	e := &Escalator{
		backend:  backend,
		opts:     opts,
		nowFunc:  time.Now,
		tenantID: strings.TrimSpace(opts.TenantID),
		botUser:  strings.TrimSpace(opts.BotUsername),
	}
	if opts.GlobalPerSecond > 0 {
		e.global = newTokenBucket(opts.GlobalPerSecond, opts.GlobalPerSecond)
	}
	if opts.DedupTTLSeconds != 0 {
		ttl := time.Duration(opts.DedupTTLSeconds) * time.Second
		e.dedup = newDedupCache(ttl, opts.DedupMaxEntries)
	}
	// A zero ContextSize leaves ctxBuf nil, so ClassifyInChannel stays exactly
	// single-turn and Observe is a no-op.
	if opts.ContextSize > 0 {
		e.ctxBuf = newContextBuffer(opts.ContextSize)
	}
	return e
}

// TimeoutDuration returns the suggested timeout for a VerdictTimeout decision.
func (e *Escalator) TimeoutDuration() time.Duration {
	return time.Duration(e.opts.TimeoutSeconds) * time.Second
}

// Classify is [Escalator.ClassifyInChannel] with no channel context: it judges
// text single-turn, with no recent-chat history. It is retained for callers
// that do not track a channel and for backward compatibility.
func (e *Escalator) Classify(ctx context.Context, rules, username, text string) Decision {
	return e.ClassifyInChannel(ctx, "", rules, username, text)
}

// ClassifyInChannel asks the backend whether text (in the channel's stated
// rules context) should be allowed, deleted, or met with a timeout, giving the
// model the channel's recent chat history for context when the context window
// is enabled. rules is the streamer's plain-language description of what is not
// allowed. An empty text or empty rules returns VerdictUnknown without calling
// the backend (there is nothing to judge against). A rate-limited call also
// returns VerdictUnknown.
//
// History only affects the prompt, never the dedup key (which stays derived
// from the raw text), so identical messages still reuse one classification.
func (e *Escalator) ClassifyInChannel(ctx context.Context, channel, rules, username, text string) Decision {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(rules) == "" {
		return Decision{Verdict: VerdictUnknown}
	}
	// The pre-filter keeps clearly-harmless chatter off the paid backend. A
	// skip is an explicit allow (the rule engine already passed the message),
	// not VerdictUnknown, so the caller does not treat it as "no answer".
	if !needsAI(text) {
		return Decision{Verdict: VerdictAllow, Action: VerdictAllow, Category: CategoryNone}
	}
	var key string
	if e.dedup != nil {
		key = dedupKey(rules, text)
		if cached, ok := e.dedup.get(key); ok {
			cached.Consulted = false
			return cached
		}
	}
	if !e.allow() {
		return Decision{Verdict: VerdictUnknown}
	}
	out, err := e.backend.Complete(ctx, buildSystemPrompt(rules), e.buildUserPrompt(channel, username, text))
	if err != nil {
		return Decision{Verdict: VerdictUnknown, Reason: "backend error"}
	}
	d := parseAIVerdict(out)
	d.Consulted = true
	if e.dedup != nil {
		e.dedup.put(key, d)
	}
	return d
}

// Observe records a processed chat message into the rolling per-channel context
// window so later ClassifyInChannel calls can judge messages with recent
// history. It must be called for every processed message, regardless of whether
// the AI is consulted about it, so history exists before the first escalation.
// It is a no-op when the context window is disabled (Options.ContextSize == 0)
// and skips the bot's own messages when Options.BotUsername is set. Safe for
// concurrent use.
func (e *Escalator) Observe(channel, username, text string) {
	if e.ctxBuf == nil {
		return
	}
	if e.botUser != "" && strings.EqualFold(strings.TrimSpace(username), e.botUser) {
		return
	}
	e.ctxBuf.observe(e.tenantID, channel, username, text)
}

// buildUserPrompt renders the user message the model classifies. With the
// context window disabled or no history yet, it returns text unchanged, so the
// request is byte-for-byte identical to the single-turn behaviour. With history
// present it prepends a clearly delimited recent-chat block (oldest first) and
// marks the message under review.
func (e *Escalator) buildUserPrompt(channel, username, text string) string {
	if e.ctxBuf == nil {
		return text
	}
	history := e.ctxBuf.recent(e.tenantID, channel, e.opts.ContextSize)
	if len(history) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString("Recent chat (oldest first, context only, do NOT judge these):\n")
	for _, h := range history {
		b.WriteString(h.Username)
		b.WriteString(": ")
		b.WriteString(h.Text)
		b.WriteByte('\n')
	}
	b.WriteString("\n--- Message under review")
	if u := strings.TrimSpace(username); u != "" {
		b.WriteString(" (from ")
		b.WriteString(u)
		b.WriteByte(')')
	}
	b.WriteString(" ---\n")
	b.WriteString(text)
	return b.String()
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

// buildSystemPrompt instructs the model to emit a single JSON verdict object,
// given the channel's rules.
func buildSystemPrompt(rules string) string {
	return "You are a Twitch chat moderation assistant. The channel rules are: " + rules + "\n" +
		"Judge the user's message and respond with ONLY a single JSON object, no markdown, no code fences, no prose. " +
		"The object has these fields: " +
		`action (one of "allow", "delete", "timeout"), ` +
		`category (one of "none", "spam", "harassment", "hate", "threat", "doxxing", "sexual", "sexism", "other"), ` +
		"severity (integer 0 to 3), confidence (number 0.0 to 1.0), reason (short string).\n" +
		"Guidance: spam, ads, follow-me, or foreign promo links are hit-and-run; use action delete, category spam, severity usually 1. " +
		"Jokes or borderline messages should be action allow or a low severity. " +
		"Slurs, threats, or doxxing are severe; use action timeout, the matching category, severity 3.\n" +
		"Examples:\n" +
		`{"action":"delete","category":"spam","severity":1,"confidence":0.9,"reason":"foreign promo link"}` + "\n" +
		`{"action":"timeout","category":"threat","severity":3,"confidence":0.95,"reason":"threat of violence"}`
}

// aiVerdict is the wire shape the model is asked to emit.
type aiVerdict struct {
	Action     string  `json:"action"`
	Category   string  `json:"category"`
	Severity   int     `json:"severity"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// parseAIVerdict extracts a JSON verdict object from the model reply. It
// tolerates leading or trailing prose and markdown fences by slicing from the
// first '{' to the last '}'. On any failure it falls back to the legacy
// single-word parsing so a plaintext reply still works; failing that it returns
// VerdictUnknown so the caller fails open.
func parseAIVerdict(out string) Decision {
	if strings.TrimSpace(out) == "" {
		return Decision{Verdict: VerdictUnknown, Action: VerdictUnknown}
	}
	if d, ok := decodeJSONVerdict(out); ok {
		return d
	}
	v, reason := parseLegacyVerdict(out)
	cat := CategoryNone
	if v != VerdictAllow && v != VerdictUnknown {
		cat = CategoryOther
	}
	return Decision{Verdict: v, Action: v, Category: cat, Reason: reason}
}

// decodeJSONVerdict slices the first balanced-looking JSON object out of s and
// unmarshals it, clamping numeric ranges and validating the category. ok is
// false when no JSON object is present or it fails to decode or has no action.
func decodeJSONVerdict(s string) (Decision, bool) {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return Decision{}, false
	}
	var wire aiVerdict
	if err := json.Unmarshal([]byte(s[start:end+1]), &wire); err != nil {
		return Decision{}, false
	}
	v := actionToVerdict(wire.Action)
	if v == VerdictUnknown {
		return Decision{}, false
	}
	sev := wire.Severity
	if sev < 0 {
		sev = 0
	} else if sev > 3 {
		sev = 3
	}
	conf := wire.Confidence
	if conf < 0 {
		conf = 0
	} else if conf > 1 {
		conf = 1
	}
	cat := Category(strings.ToLower(strings.TrimSpace(wire.Category)))
	if !knownCategories[cat] {
		cat = CategoryOther
	}
	return Decision{
		Verdict:    v,
		Action:     v,
		Category:   cat,
		Severity:   sev,
		Confidence: conf,
		Reason:     strings.TrimSpace(wire.Reason),
	}, true
}

func actionToVerdict(action string) Verdict {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "allow":
		return VerdictAllow
	case "delete":
		return VerdictDelete
	case "timeout":
		return VerdictTimeout
	default:
		return VerdictUnknown
	}
}

// parseLegacyVerdict is the pre-JSON path: read the leading ALLOW/DELETE/TIMEOUT
// token and trailing reason from the first line. Anything unrecognised maps to
// VerdictUnknown so the caller fails open.
func parseLegacyVerdict(out string) (Verdict, string) {
	out = strings.TrimSpace(out)
	if out == "" {
		return VerdictUnknown, ""
	}
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

// PolicyConfig tunes the pure policy mapping from a Decision to an enforcement
// outcome.
type PolicyConfig struct {
	MinConfidence float64
	LowTimeout    time.Duration
	HighTimeout   time.Duration
}

// DefaultPolicy returns conservative defaults: audit-only below 0.6 confidence,
// a 60s timeout for severity 2 and 600s for severity 3.
func DefaultPolicy() PolicyConfig {
	return PolicyConfig{
		MinConfidence: 0.6,
		LowTimeout:    60 * time.Second,
		HighTimeout:   600 * time.Second,
	}
}

// PolicyOutcome is the enforcement decision derived from a Decision.
type PolicyOutcome struct {
	Action     Verdict
	Timeout    time.Duration
	FeedLadder bool
	AuditOnly  bool
}

// ApplyPolicy maps a Decision to a concrete enforcement outcome. It is pure: no
// clock, no IO, fully unit-testable. Spam is always delete-only and never feeds
// the repeat-offender ladder (hit-and-run); severity drives timeouts so jokes
// are never timed out.
func ApplyPolicy(d Decision, cfg PolicyConfig) PolicyOutcome {
	if d.Verdict == VerdictUnknown || d.Verdict == VerdictAllow {
		return PolicyOutcome{Action: VerdictAllow}
	}
	// Low-confidence punishments are recorded but not enforced, protecting
	// jokes and uncertain cases.
	if d.Consulted && d.Confidence < cfg.MinConfidence {
		return PolicyOutcome{Action: VerdictAllow, AuditOnly: true}
	}
	if d.Category == CategorySpam {
		return PolicyOutcome{Action: VerdictDelete}
	}
	// Severity is authoritative over the raw action: it alone decides timeout
	// versus delete, so a joke is never timed out and a severe case never slips
	// through as a mere delete.
	switch {
	case d.Severity >= 3:
		return PolicyOutcome{Action: VerdictTimeout, Timeout: cfg.HighTimeout, FeedLadder: true}
	case d.Severity == 2:
		return PolicyOutcome{Action: VerdictTimeout, Timeout: cfg.LowTimeout, FeedLadder: true}
	default:
		return PolicyOutcome{Action: VerdictDelete}
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

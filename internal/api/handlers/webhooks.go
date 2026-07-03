package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

const (
	webhookMaxBodyBytes  = 64 * 1024
	webhookSignatureHead = "X-Engelos-Signature"
	webhookRatePerSecond = 5
	webhookBurst         = 5
)

// Webhooks serves inbound, unauthenticated-but-HMAC-verified webhooks that fire
// a single named rule. It is mounted only when the Action-Engine store+runner
// are wired (router nil-guard). Every request is signature-checked against the
// rule's own secret and rate-limited per rule, so a leaked URL cannot spam chat.
type Webhooks struct {
	store    actions.Store
	runner   RuleRunner
	tenantID string
	logger   *slog.Logger
	limiter  *ruleRateLimiter
}

// NewWebhooks constructs the inbound webhook handler. A nil store or runner
// makes every request 501, so the route can be mounted defensively.
func NewWebhooks(store actions.Store, runner RuleRunner, tenantID string, logger *slog.Logger) *Webhooks {
	if logger == nil {
		logger = slog.Default()
	}
	return &Webhooks{
		store:    store,
		runner:   runner,
		tenantID: strings.TrimSpace(tenantID),
		logger:   logger,
		limiter:  newRuleRateLimiter(webhookRatePerSecond, webhookBurst, time.Now),
	}
}

// Handle serves POST /api/v1/channels/{channelSlug}/webhooks/{ruleName}. It
// loads the named webhook rule, enforces the 64KB body cap, verifies the
// X-Engelos-Signature HMAC against the rule secret, applies a per-rule rate
// limit, then fires the rule with the JSON body flattened into the trigger data.
func (h *Webhooks) Handle(w http.ResponseWriter, r *http.Request) {
	if h.store == nil || h.runner == nil {
		notImplemented(w)
		return
	}
	channel := normChannel(chi.URLParam(r, "channelSlug"))
	name := strings.TrimSpace(chi.URLParam(r, "ruleName"))
	if channel == "" || name == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}

	// A missing, non-webhook, or disabled rule is indistinguishable to a caller
	// (all 404) so the endpoint never becomes a rule-enumeration oracle.
	rule, err := h.store.Get(r.Context(), h.tenantID, channel, name)
	if err != nil || rule.TriggerKind != actions.TriggerWebhook || !rule.Enabled {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	secret := webhookSecretFrom(rule.TriggerFilter)
	if secret == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Read one byte past the cap so an oversized body is detectable and refused
	// rather than silently truncated (which would also break the signature).
	body, err := io.ReadAll(io.LimitReader(r.Body, webhookMaxBodyBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read_error"})
		return
	}
	if len(body) > webhookMaxBodyBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body_too_large"})
		return
	}

	if !validWebhookSignature(secret, body, r.Header.Get(webhookSignatureHead)) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_signature"})
		return
	}

	// Rate-limit only signature-valid requests, so an attacker without the
	// secret cannot exhaust a legitimate sender's budget.
	if !h.limiter.allow(h.tenantID + "\x00" + channel + "\x00" + name) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
		return
	}

	h.runner.RunRule(rule, actions.Trigger{
		Kind:      actions.TriggerWebhook,
		Channel:   channel,
		EventType: "webhook",
		Data:      flattenWebhookBody(body),
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// validWebhookSignature reports whether sigHex is a valid lowercase-hex
// HMAC-SHA256 of body under secret, compared in constant time.
func validWebhookSignature(secret string, body []byte, sigHex string) bool {
	sigHex = strings.TrimSpace(sigHex)
	if secret == "" || sigHex == "" {
		return false
	}
	got, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// flattenWebhookBody projects a JSON object body onto a flat trigger-data map:
// top-level string values are kept verbatim and every other value is stringified
// (as compact JSON). A non-object or invalid body yields an empty map.
func flattenWebhookBody(body []byte) map[string]any {
	data := map[string]any{}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return data
	}
	for k, raw := range obj {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			data[k] = s
		} else {
			data[k] = strings.TrimSpace(string(raw))
		}
	}
	return data
}

// ruleRateLimiter is a per-key token-bucket limiter. Each key (a rule identity)
// gets an independent bucket so one busy webhook never starves another.
type ruleRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64
	burst   float64
	now     func() time.Time
}

func newRuleRateLimiter(rate, burst float64, now func() time.Time) *ruleRateLimiter {
	if now == nil {
		now = time.Now
	}
	return &ruleRateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    rate,
		burst:   burst,
		now:     now,
	}
}

func (rl *ruleRateLimiter) allow(key string) bool {
	rl.mu.Lock()
	b := rl.buckets[key]
	if b == nil {
		b = &tokenBucket{tokens: rl.burst, capacity: rl.burst, rate: rl.rate, last: rl.now()}
		rl.buckets[key] = b
	}
	rl.mu.Unlock()
	return b.take(rl.now())
}

type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64
	last     time.Time
}

func (b *tokenBucket) take(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = min(b.capacity, b.tokens+elapsed*b.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

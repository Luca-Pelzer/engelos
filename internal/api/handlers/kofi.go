package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

const (
	kofiIntegrationID = "kofi"
	kofiTokenKey      = "verification_token"
	kofiRatePerSecond = 5
	kofiBurst         = 5
)

// KofiCredSource reads a decrypted integration credential; the Ko-fi handler
// uses it to fetch the tenant's stored verification token. *credstore.Store
// satisfies it.
type KofiCredSource interface {
	Get(ctx context.Context, tenantID, integrationID, key string) (string, error)
}

// kofiChannelLister returns the channels whose enabled rules want a given event
// type, so a channel-less donation can fan out to every interested channel.
// actions.Store satisfies it.
type kofiChannelLister interface {
	ListEventChannels(ctx context.Context, tenantID, eventType string) ([]string, error)
}

// DonationDispatcher routes a synthesized event into the action engine.
// *runtime.Dispatcher satisfies it via its Dispatch method.
type DonationDispatcher interface {
	Dispatch(ctx context.Context, ev adapters.Event)
}

// Kofi serves the inbound Ko-fi donation webhook. Ko-fi has no request-signing
// scheme; it instead embeds a per-account verification token in the payload,
// which is matched in constant time against the tenant's stored token. The
// endpoint is unauthenticated by design (Ko-fi cannot send cookies), so it
// enforces a body cap and a rate limit and reveals nothing about donors on the
// unhappy paths.
type Kofi struct {
	creds    KofiCredSource
	channels kofiChannelLister
	dispatch DonationDispatcher
	tenantID string
	logger   *slog.Logger
	limiter  *ruleRateLimiter
}

// NewKofi constructs the Ko-fi webhook handler. Any nil dependency makes every
// request 501, so the route can be mounted defensively.
func NewKofi(creds KofiCredSource, channels kofiChannelLister, dispatch DonationDispatcher, tenantID string, logger *slog.Logger) *Kofi {
	if logger == nil {
		logger = slog.Default()
	}
	return &Kofi{
		creds:    creds,
		channels: channels,
		dispatch: dispatch,
		tenantID: strings.TrimSpace(tenantID),
		logger:   logger,
		limiter:  newRuleRateLimiter(kofiRatePerSecond, kofiBurst, time.Now),
	}
}

// kofiPayload is the subset of Ko-fi's JSON webhook body the engine consumes.
// Ko-fi posts it as a single URL-encoded form field named "data".
type kofiPayload struct {
	VerificationToken string `json:"verification_token"`
	Type              string `json:"type"`
	FromName          string `json:"from_name"`
	Amount            string `json:"amount"`
	Currency          string `json:"currency"`
	Message           string `json:"message"`
}

// Handle serves POST /api/v1/integrations/kofi/webhook. It enforces the 64KB
// body cap, extracts the JSON "data" form field, verifies the embedded token in
// constant time against the tenant's stored verification token, rate-limits, and
// fans a donation event out to every channel with an enabled donation rule.
func (h *Kofi) Handle(w http.ResponseWriter, r *http.Request) {
	if h.creds == nil || h.channels == nil || h.dispatch == nil {
		notImplemented(w)
		return
	}

	// Read one byte past the cap so an oversized body is refused rather than
	// silently truncated.
	body, err := io.ReadAll(io.LimitReader(r.Body, webhookMaxBodyBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read_error"})
		return
	}
	if len(body) > webhookMaxBodyBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body_too_large"})
		return
	}

	form, err := url.ParseQuery(string(body))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_form"})
		return
	}
	data := form.Get("data")
	if data == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_data"})
		return
	}
	var p kofiPayload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	// A missing stored token, an unreadable one, and a mismatch are all a single
	// 401 so the endpoint never confirms whether a tenant has Ko-fi configured.
	stored, err := h.creds.Get(r.Context(), h.tenantID, kofiIntegrationID, kofiTokenKey)
	if err != nil || stored == "" ||
		subtle.ConstantTimeCompare([]byte(p.VerificationToken), []byte(stored)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	if !h.limiter.allow(h.tenantID) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
		return
	}

	channels, err := h.channels.ListEventChannels(r.Context(), h.tenantID, string(adapters.EventDonation))
	if err != nil {
		h.logger.Error("kofi: list donation channels failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}

	donation := &adapters.DonationEvent{
		From:     p.FromName,
		Amount:   p.Amount,
		Currency: p.Currency,
		Message:  p.Message,
		Kind:     p.Type,
	}
	for _, ch := range channels {
		h.dispatch.Dispatch(r.Context(), adapters.Event{
			Type:       adapters.EventDonation,
			Platform:   kofiIntegrationID,
			Channel:    ch,
			OccurredAt: time.Now().UTC(),
			Donation:   donation,
		})
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

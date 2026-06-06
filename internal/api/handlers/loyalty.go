package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/loyalty"
)

// LoyaltyResolver maps a viewer login to their public profile so dashboard
// grants can be keyed by the stable numeric user id, matching the keyspace the
// in-chat economy uses. It mirrors the resolver the chat economyAdapter relies
// on.
type LoyaltyResolver func(ctx context.Context, login string) (commands.UserProfile, error)

// Loyalty exposes read access to the points leaderboard plus manual
// grant/deduct controls for the dashboard. Endpoints are session-protected at
// the router layer and channel-scoped by the lower-cased channel login.
//
// Dashboard grants resolve the typed username to the platform's numeric user
// id before crediting, so the account lands in the same keyspace the viewer
// spends from in chat. When no resolver is wired, Adjust fails closed rather
// than writing a phantom login-keyed row.
type Loyalty struct {
	store    loyalty.Store
	tenantID string
	resolve  LoyaltyResolver
	logger   *slog.Logger
}

// NewLoyalty constructs the Loyalty handler. When store is nil every endpoint
// short-circuits to 501 so the router boots without the feature. resolve maps
// a username to its numeric id for Adjust; a nil resolver makes Adjust fail
// closed instead of writing a username-keyed phantom account.
func NewLoyalty(store loyalty.Store, tenantID string, resolve LoyaltyResolver, logger *slog.Logger) *Loyalty {
	if logger == nil {
		logger = slog.Default()
	}
	return &Loyalty{store: store, tenantID: strings.TrimSpace(tenantID), resolve: resolve, logger: logger}
}

// Leaderboard handles GET /api/v1/loyalty/leaderboard?channel=...&limit=...
func (h *Loyalty) Leaderboard(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	list, err := h.store.Leaderboard(r.Context(), h.tenantID, channel, 25)
	if err != nil {
		h.logger.Error("loyalty leaderboard failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i, a := range list {
		out = append(out, map[string]any{"rank": i + 1, "username": a.Username, "balance": a.Balance})
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel, "leaderboard": out})
}

type loyaltyAdjustRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
	Amount   int64  `json:"amount"`
}

// Adjust handles POST /api/v1/loyalty/adjust. A positive amount grants points
// (Earn), a negative amount deducts them (Spend). Body: {channel, username, amount}.
func (h *Loyalty) Adjust(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req loyaltyAdjustRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	channel := channelFromRequest(r, req.Channel)
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if channel == "" || username == "" || req.Amount == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel, username and non-zero amount are required"})
		return
	}

	// Resolve the typed username to its numeric id so the grant lands in the
	// same keyspace chat spends from. Fail closed when unresolvable: writing by
	// username would create a phantom account the viewer can never reach.
	if h.resolve == nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "could not resolve user"})
		return
	}
	prof, rerr := h.resolve(r.Context(), username)
	if rerr != nil || strings.TrimSpace(prof.ID) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "could not resolve user"})
		return
	}
	viewerID := prof.ID
	displayName := prof.Login
	if strings.TrimSpace(displayName) == "" {
		displayName = username
	}

	var acc loyalty.Account
	var err error
	if req.Amount > 0 {
		acc, err = h.store.Earn(r.Context(), h.tenantID, channel, viewerID, displayName, req.Amount)
	} else {
		acc, err = h.store.Spend(r.Context(), h.tenantID, channel, viewerID, -req.Amount)
	}
	if err != nil {
		switch {
		case errors.Is(err, loyalty.ErrInsufficient):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "insufficient"})
		case errors.Is(err, loyalty.ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		default:
			h.logger.Error("loyalty adjust failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": acc.Username, "balance": acc.Balance})
}

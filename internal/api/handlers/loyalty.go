package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/loyalty"
)

// Loyalty exposes read access to the points leaderboard plus manual
// grant/deduct controls for the dashboard. Endpoints are session-protected at
// the router layer and channel-scoped by the lower-cased channel login.
//
// Dashboard grants key the account by the lower-cased username (used as the
// viewer id), so a moderator can adjust points by typing a name without
// knowing the platform's numeric user id.
type Loyalty struct {
	store    loyalty.Store
	tenantID string
	logger   *slog.Logger
}

// NewLoyalty constructs the Loyalty handler. When store is nil every endpoint
// short-circuits to 501 so the router boots without the feature.
func NewLoyalty(store loyalty.Store, tenantID string, logger *slog.Logger) *Loyalty {
	if logger == nil {
		logger = slog.Default()
	}
	return &Loyalty{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// Leaderboard handles GET /api/v1/loyalty/leaderboard?channel=...&limit=...
func (h *Loyalty) Leaderboard(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
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
	channel := normChannel(req.Channel)
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if channel == "" || username == "" || req.Amount == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel, username and non-zero amount are required"})
		return
	}

	var acc loyalty.Account
	var err error
	if req.Amount > 0 {
		acc, err = h.store.Earn(r.Context(), h.tenantID, channel, username, username, req.Amount)
	} else {
		acc, err = h.store.Spend(r.Context(), h.tenantID, channel, username, -req.Amount)
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

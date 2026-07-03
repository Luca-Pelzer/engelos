package avatar

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// WSPath is the router-mounted path of the overlay WebSocket endpoint. It is
// echoed in the token response so the dashboard can assemble a ready-to-paste
// OBS browser-source URL.
const WSPath = "/api/v1/overlay/avatar/ws"

// TokenHandler serves the owner-gated endpoint that mints (on first read) and
// returns the per-channel overlay token, so the operator can build the OBS
// browser-source URL. The router mounts it behind the owner/admin guard at
//
//	GET /api/v1/overlay/avatar/token?channel={slug}
type TokenHandler struct {
	store    TokenStore
	tenantID string
	logger   *slog.Logger
}

// NewTokenHandler constructs a TokenHandler. A nil logger falls back to
// [slog.Default].
func NewTokenHandler(store TokenStore, tenantID string, logger *slog.Logger) *TokenHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TokenHandler{
		store:    store,
		tenantID: strings.TrimSpace(tenantID),
		logger:   logger.With("component", "avatar.token"),
	}
}

type tokenResponse struct {
	Channel string `json:"channel"`
	Token   string `json:"token"`
	WSPath  string `json:"ws_path"`
}

func (h *TokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "avatar_not_configured"})
		return
	}
	channel := normalizeChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	token, err := h.store.GetOrCreate(r.Context(), h.tenantID, channel)
	if err != nil {
		h.logger.Warn("avatar overlay token mint failed", "channel", channel, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{Channel: channel, Token: token, WSPath: WSPath})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/contextmod"
)

// ContextMod exposes per-channel AI context-moderation configuration (enabled
// plus the plain-language rules fed to the classifier) over HTTP for the
// dashboard. All endpoints are session-protected at the router layer. When the
// store is nil every endpoint returns 501 so the router still boots with the
// feature off.
type ContextMod struct {
	store    contextmod.Store
	tenantID string
	logger   *slog.Logger
}

// NewContextMod constructs the handler bundle.
func NewContextMod(store contextmod.Store, tenantID string, logger *slog.Logger) *ContextMod {
	if logger == nil {
		logger = slog.Default()
	}
	return &ContextMod{store: store, tenantID: tenantID, logger: logger}
}

// Get handles GET /api/v1/contextmod?channel=...
// Without a channel it lists every configured channel for the tenant; with a
// channel it returns that channel's config (or the disabled default).
func (h *ContextMod) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		configs, err := h.store.List(r.Context(), h.tenantID)
		if err != nil {
			h.writeStoreError(w, r, "contextmod list failed", err)
			return
		}
		out := make([]map[string]any, 0, len(configs))
		for _, c := range configs {
			out = append(out, contextmodJSON(c))
		}
		writeJSON(w, http.StatusOK, map[string]any{"configs": out})
		return
	}
	c, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "contextmod get failed", err)
		return
	}
	writeJSON(w, http.StatusOK, contextmodJSON(c))
}

// Set handles PUT /api/v1/contextmod. Body carries the channel plus any subset
// of the tunable fields; omitted fields keep the channel's current value.
func (h *ContextMod) Set(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req struct {
		Channel string  `json:"channel"`
		Enabled *bool   `json:"enabled"`
		Rules   *string `json:"rules"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	current, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "contextmod get failed", err)
		return
	}
	if req.Enabled != nil {
		current.Enabled = *req.Enabled
	}
	if req.Rules != nil {
		current.Rules = *req.Rules
	}
	saved, err := h.store.Set(r.Context(), current)
	if err != nil {
		h.writeStoreError(w, r, "contextmod set failed", err)
		return
	}
	writeJSON(w, http.StatusOK, contextmodJSON(saved))
}

// writeStoreError maps store sentinel errors to HTTP status codes.
func (h *ContextMod) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, contextmod.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

// contextmodJSON renders a Config into the wire shape with an RFC3339 timestamp.
func contextmodJSON(c contextmod.Config) map[string]any {
	return map[string]any{
		"channel":    c.Channel,
		"enabled":    c.Enabled,
		"rules":      c.Rules,
		"updated_at": c.UpdatedAt.Format(time.RFC3339),
	}
}

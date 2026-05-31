package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/cohost"
)

// CoHost exposes per-channel AI co-host configuration (enabled, the name
// viewers address, the persona, and the reply length cap) over HTTP for the
// dashboard. All endpoints are session-protected at the router layer. When the
// store is nil every endpoint returns 501 so the router still boots with the
// feature off.
type CoHost struct {
	store    cohost.Store
	tenantID string
	logger   *slog.Logger
}

// NewCoHost constructs the handler bundle.
func NewCoHost(store cohost.Store, tenantID string, logger *slog.Logger) *CoHost {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoHost{store: store, tenantID: tenantID, logger: logger}
}

// Get handles GET /api/v1/cohost?channel=...
// Without a channel it lists every configured channel for the tenant; with a
// channel it returns that channel's config (or the disabled default).
func (h *CoHost) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		configs, err := h.store.List(r.Context(), h.tenantID)
		if err != nil {
			h.writeStoreError(w, r, "cohost list failed", err)
			return
		}
		out := make([]map[string]any, 0, len(configs))
		for _, c := range configs {
			out = append(out, cohostJSON(c))
		}
		writeJSON(w, http.StatusOK, map[string]any{"configs": out})
		return
	}
	c, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "cohost get failed", err)
		return
	}
	writeJSON(w, http.StatusOK, cohostJSON(c))
}

// Set handles PUT /api/v1/cohost. Body carries the channel plus any subset of
// the tunable fields; omitted fields keep the channel's current value.
func (h *CoHost) Set(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req struct {
		Channel     string  `json:"channel"`
		Enabled     *bool   `json:"enabled"`
		BotName     *string `json:"bot_name"`
		Persona     *string `json:"persona"`
		MaxReplyLen *int    `json:"max_reply_len"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
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
		h.writeStoreError(w, r, "cohost get failed", err)
		return
	}
	if req.Enabled != nil {
		current.Enabled = *req.Enabled
	}
	if req.BotName != nil {
		current.BotName = *req.BotName
	}
	if req.Persona != nil {
		current.Persona = *req.Persona
	}
	if req.MaxReplyLen != nil {
		current.MaxReplyLen = *req.MaxReplyLen
	}
	saved, err := h.store.Set(r.Context(), current)
	if err != nil {
		h.writeStoreError(w, r, "cohost set failed", err)
		return
	}
	writeJSON(w, http.StatusOK, cohostJSON(saved))
}

// writeStoreError maps store sentinel errors to HTTP status codes.
func (h *CoHost) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, cohost.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

// cohostJSON renders a Config into the wire shape with an RFC3339 timestamp.
func cohostJSON(c cohost.Config) map[string]any {
	return map[string]any{
		"channel":       c.Channel,
		"enabled":       c.Enabled,
		"bot_name":      c.BotName,
		"persona":       c.Persona,
		"max_reply_len": c.MaxReplyLen,
		"updated_at":    c.UpdatedAt.Format(time.RFC3339),
	}
}

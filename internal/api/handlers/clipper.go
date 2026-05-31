package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/clipper"
)

// Clipper exposes per-channel auto-clipper tuning (enabled plus the unique-user
// thresholds and rate knobs) over HTTP for the dashboard. All endpoints are
// session-protected at the router layer. When the store is nil every endpoint
// returns 501 so the router still boots with the feature off.
type Clipper struct {
	store    clipper.Store
	tenantID string
	logger   *slog.Logger
}

// NewClipper constructs the handler bundle.
func NewClipper(store clipper.Store, tenantID string, logger *slog.Logger) *Clipper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Clipper{store: store, tenantID: tenantID, logger: logger}
}

// Get handles GET /api/v1/clipper?channel=...
// Without a channel it lists every configured channel for the tenant; with a
// channel it returns that channel's config (or the disabled default).
func (h *Clipper) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		configs, err := h.store.List(r.Context(), h.tenantID)
		if err != nil {
			h.writeStoreError(w, r, "clipper list failed", err)
			return
		}
		out := make([]map[string]any, 0, len(configs))
		for _, c := range configs {
			out = append(out, clipperJSON(c))
		}
		writeJSON(w, http.StatusOK, map[string]any{"configs": out})
		return
	}
	c, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "clipper get failed", err)
		return
	}
	writeJSON(w, http.StatusOK, clipperJSON(c))
}

// Set handles PUT /api/v1/clipper. Body carries the channel plus any subset of
// the tunable fields; numeric zero means "inherit the running default".
func (h *Clipper) Set(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	var req struct {
		Channel            string   `json:"channel"`
		Enabled            *bool    `json:"enabled"`
		KeywordThreshold   *int     `json:"keyword_threshold"`
		EmoteThreshold     *int     `json:"emote_threshold"`
		CopypastaThreshold *int     `json:"copypasta_threshold"`
		MinMessages        *int     `json:"min_messages"`
		SpikeFactor        *float64 `json:"spike_factor"`
		CompositeThreshold *float64 `json:"composite_threshold"`
		CooldownSeconds    *int     `json:"cooldown_seconds"`
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
		h.writeStoreError(w, r, "clipper get failed", err)
		return
	}
	if req.Enabled != nil {
		current.Settings.Enabled = *req.Enabled
	}
	if req.KeywordThreshold != nil {
		current.Settings.KeywordThreshold = *req.KeywordThreshold
	}
	if req.EmoteThreshold != nil {
		current.Settings.EmoteThreshold = *req.EmoteThreshold
	}
	if req.CopypastaThreshold != nil {
		current.Settings.CopypastaThreshold = *req.CopypastaThreshold
	}
	if req.MinMessages != nil {
		current.Settings.MinMessages = *req.MinMessages
	}
	if req.SpikeFactor != nil {
		current.Settings.SpikeFactor = *req.SpikeFactor
	}
	if req.CompositeThreshold != nil {
		current.Settings.CompositeThreshold = *req.CompositeThreshold
	}
	if req.CooldownSeconds != nil {
		current.Settings.CooldownSeconds = *req.CooldownSeconds
	}
	saved, err := h.store.Set(r.Context(), current)
	if err != nil {
		h.writeStoreError(w, r, "clipper set failed", err)
		return
	}
	writeJSON(w, http.StatusOK, clipperJSON(saved))
}

// writeStoreError maps store sentinel errors to HTTP status codes.
func (h *Clipper) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, clipper.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func (h *Clipper) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "clipper_not_enabled"})
}

// clipperJSON renders a Config into the wire shape with an RFC3339 timestamp.
func clipperJSON(c clipper.Config) map[string]any {
	return map[string]any{
		"channel":             c.Channel,
		"enabled":             c.Settings.Enabled,
		"keyword_threshold":   c.Settings.KeywordThreshold,
		"emote_threshold":     c.Settings.EmoteThreshold,
		"copypasta_threshold": c.Settings.CopypastaThreshold,
		"min_messages":        c.Settings.MinMessages,
		"spike_factor":        c.Settings.SpikeFactor,
		"composite_threshold": c.Settings.CompositeThreshold,
		"cooldown_seconds":    c.Settings.CooldownSeconds,
		"updated_at":          c.UpdatedAt.Format(time.RFC3339),
	}
}

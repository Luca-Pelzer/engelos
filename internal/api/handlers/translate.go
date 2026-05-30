package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/translate"
)

// Translate exposes per-channel chat-translation configuration (enabled, target
// language, output mode, minimum word count) over HTTP for the dashboard. All
// endpoints are session-protected at the router layer. When the store is nil
// every endpoint returns 501 so the router still boots with the feature off.
type Translate struct {
	store    translate.Store
	tenantID string
	logger   *slog.Logger
}

// NewTranslate constructs the handler bundle.
func NewTranslate(store translate.Store, tenantID string, logger *slog.Logger) *Translate {
	if logger == nil {
		logger = slog.Default()
	}
	return &Translate{store: store, tenantID: tenantID, logger: logger}
}

// Get handles GET /api/v1/translate?channel=...
// Without a channel it lists every configured channel for the tenant; with a
// channel it returns that channel's config (or the disabled default).
func (h *Translate) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		configs, err := h.store.List(r.Context(), h.tenantID)
		if err != nil {
			h.writeStoreError(w, r, "translate list failed", err)
			return
		}
		out := make([]map[string]any, 0, len(configs))
		for _, c := range configs {
			out = append(out, translateJSON(c))
		}
		writeJSON(w, http.StatusOK, map[string]any{"configs": out})
		return
	}
	c, err := h.store.GetOrDefault(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "translate get failed", err)
		return
	}
	writeJSON(w, http.StatusOK, translateJSON(c))
}

// Set handles PUT /api/v1/translate.
// Body: {channel, enabled, target_lang, output_mode, min_word_count}.
func (h *Translate) Set(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	var req struct {
		Channel      string `json:"channel"`
		Enabled      *bool  `json:"enabled"`
		TargetLang   string `json:"target_lang"`
		OutputMode   string `json:"output_mode"`
		MinWordCount int    `json:"min_word_count"`
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
		h.writeStoreError(w, r, "translate get failed", err)
		return
	}
	if req.Enabled != nil {
		current.Enabled = *req.Enabled
	}
	if req.TargetLang != "" {
		current.TargetLang = req.TargetLang
	}
	if req.OutputMode != "" {
		current.OutputMode = req.OutputMode
	}
	if req.MinWordCount > 0 {
		current.MinWordCount = req.MinWordCount
	}
	saved, err := h.store.Set(r.Context(), current)
	if err != nil {
		h.writeStoreError(w, r, "translate set failed", err)
		return
	}
	writeJSON(w, http.StatusOK, translateJSON(saved))
}

// writeStoreError maps store sentinel errors to HTTP status codes.
func (h *Translate) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, translate.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func (h *Translate) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "translate_not_enabled"})
}

// translateJSON renders a Config into the wire shape with an RFC3339 timestamp.
func translateJSON(c translate.Config) map[string]any {
	return map[string]any{
		"channel":        c.Channel,
		"enabled":        c.Enabled,
		"target_lang":    c.TargetLang,
		"output_mode":    c.OutputMode,
		"min_word_count": c.MinWordCount,
		"updated_at":     c.UpdatedAt.Format(time.RFC3339),
	}
}

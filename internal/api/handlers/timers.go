package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/timers"
)

// Timers exposes CRUD over the per-channel auto-announcement timers store.
// Endpoints are session-protected at the router layer and channel-scoped by
// the lower-cased channel login. Intervals cross the wire in seconds.
type Timers struct {
	store    timers.Store
	tenantID string
	logger   *slog.Logger
}

// NewTimers constructs the Timers handler. When store is nil every endpoint
// short-circuits to 501 so the router boots without the feature.
func NewTimers(store timers.Store, tenantID string, logger *slog.Logger) *Timers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Timers{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/timers?channel=...
func (h *Timers) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	list, err := h.store.List(r.Context(), h.tenantID, channel)
	if err != nil {
		h.logger.Error("timers list failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		out = append(out, timerJSON(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel, "timers": out})
}

type timerWriteRequest struct {
	Channel         string `json:"channel"`
	Name            string `json:"name"`
	Message         string `json:"message"`
	IntervalSeconds int    `json:"interval_seconds"`
	MinChatLines    int    `json:"min_chat_lines"`
	Enabled         bool   `json:"enabled"`
}

// Create handles POST /api/v1/timers.
func (h *Timers) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req timerWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel and name are required"})
		return
	}
	t, err := h.store.Create(r.Context(), timers.Timer{
		TenantID:     h.tenantID,
		Channel:      channel,
		Name:         strings.TrimSpace(req.Name),
		Message:      req.Message,
		Interval:     time.Duration(req.IntervalSeconds) * time.Second,
		MinChatLines: req.MinChatLines,
		Enabled:      req.Enabled,
		CreatedBy:    "dashboard",
	})
	if err != nil {
		h.writeWriteError(w, err, "timers create failed")
		return
	}
	writeJSON(w, http.StatusCreated, timerJSON(t))
}

// Update handles PUT /api/v1/timers/{name}.
func (h *Timers) Update(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req timerWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	channel := normChannel(req.Channel)
	name := chi.URLParam(r, "name")
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	t, err := h.store.Update(r.Context(), h.tenantID, channel, name,
		req.Message, time.Duration(req.IntervalSeconds)*time.Second, req.MinChatLines, req.Enabled)
	if err != nil {
		h.writeWriteError(w, err, "timers update failed")
		return
	}
	writeJSON(w, http.StatusOK, timerJSON(t))
}

// Delete handles DELETE /api/v1/timers/{name}?channel=...
func (h *Timers) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	name := chi.URLParam(r, "name")
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if err := h.store.Delete(r.Context(), h.tenantID, channel, name); err != nil {
		if errors.Is(err, timers.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		h.logger.Error("timers delete failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Timers) writeWriteError(w http.ResponseWriter, err error, logMsg string) {
	switch {
	case errors.Is(err, timers.ErrAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already_exists"})
	case errors.Is(err, timers.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid"})
	case errors.Is(err, timers.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	default:
		h.logger.Error(logMsg, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
	}
}

func timerJSON(t timers.Timer) map[string]any {
	return map[string]any{
		"name":             t.Name,
		"message":          t.Message,
		"interval_seconds": int(t.Interval / time.Second),
		"min_chat_lines":   t.MinChatLines,
		"enabled":          t.Enabled,
	}
}

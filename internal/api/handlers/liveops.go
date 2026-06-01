package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/liveops"
)

// LiveOps exposes the per-channel event-plan store (scheduled stream events).
// Endpoints are session-protected at the router layer and channel-scoped by
// the lower-cased channel login. Times cross the wire as RFC3339 strings.
type LiveOps struct {
	store    liveops.Store
	tenantID string
	logger   *slog.Logger
}

// NewLiveOps constructs the LiveOps handler. When store is nil every endpoint
// short-circuits to 501 so the router boots without the feature.
func NewLiveOps(store liveops.Store, tenantID string, logger *slog.Logger) *LiveOps {
	if logger == nil {
		logger = slog.Default()
	}
	return &LiveOps{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/liveops?channel=...
func (h *LiveOps) List(w http.ResponseWriter, r *http.Request) {
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
		h.logger.Error("liveops list failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, eventJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel, "events": out})
}

type eventCreateRequest struct {
	Channel     string `json:"channel"`
	Name        string `json:"name"`
	Description string `json:"description"`
	StartsAt    string `json:"starts_at"`
	EndsAt      string `json:"ends_at"`
}

// Create handles POST /api/v1/liveops. starts_at is required RFC3339; ends_at optional.
func (h *LiveOps) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req eventCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel and name are required"})
		return
	}
	startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_starts_at"})
		return
	}
	var endsAt *time.Time
	if strings.TrimSpace(req.EndsAt) != "" {
		e, perr := time.Parse(time.RFC3339, req.EndsAt)
		if perr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_ends_at"})
			return
		}
		endsAt = &e
	}
	ev, err := h.store.Add(r.Context(), h.tenantID, channel, strings.TrimSpace(req.Name), req.Description, startsAt, endsAt)
	if err != nil {
		if errors.Is(err, liveops.ErrInvalid) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid"})
			return
		}
		h.logger.Error("liveops add failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusCreated, eventJSON(ev))
}

// Delete handles DELETE /api/v1/liveops/{number}?channel=...
func (h *LiveOps) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_number"})
		return
	}
	if err := h.store.Delete(r.Context(), h.tenantID, channel, number); err != nil {
		if errors.Is(err, liveops.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		h.logger.Error("liveops delete failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func eventJSON(e liveops.Event) map[string]any {
	out := map[string]any{
		"number":      e.Number,
		"name":        e.Name,
		"description": e.Description,
		"starts_at":   e.StartsAt.Format(time.RFC3339),
	}
	if e.EndsAt != nil {
		out["ends_at"] = e.EndsAt.Format(time.RFC3339)
	}
	return out
}

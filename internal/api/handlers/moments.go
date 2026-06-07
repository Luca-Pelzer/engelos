package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/moments"
)

// momentWindowMin and momentWindowMax bound the dashboard-supplied window so a
// typo cannot open a moment that never closes or closes instantly. They mirror
// the bounds the !here chat command enforces.
const (
	momentWindowMin = 10 * time.Second
	momentWindowMax = 10 * time.Minute
)

// MomentBroadcaster pushes an overlay alert when a moment opens or closes from
// the dashboard. *runtime.WSBroadcaster satisfies it; nil skips the push.
type MomentBroadcaster interface {
	Broadcast(eventType string, payload any)
}

// Moments exposes the BeReal-style moment feature over HTTP for the dashboard:
// start a moment, see the live one, end it, and browse the archive. All
// endpoints are session-protected at the router. When the store is nil every
// endpoint returns 501.
type Moments struct {
	store    moments.Store
	bc       MomentBroadcaster
	tenantID string
	logger   *slog.Logger
}

// NewMoments constructs the handler bundle. A nil store makes every endpoint
// return 501; a nil broadcaster simply skips overlay pushes.
func NewMoments(store moments.Store, bc MomentBroadcaster, tenantID string, logger *slog.Logger) *Moments {
	if logger == nil {
		logger = slog.Default()
	}
	return &Moments{store: store, bc: bc, tenantID: tenantID, logger: logger}
}

// Get handles GET /api/v1/moments?channel=...&limit=...
// It returns the channel's active moment (or null) plus the recent archive.
func (h *Moments) Get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	var activeJSON any
	active, err := h.store.Active(r.Context(), h.tenantID, channel)
	switch {
	case err == nil:
		activeJSON = momentJSON(active)
	case errors.Is(err, moments.ErrNoActive):
		activeJSON = nil
	default:
		h.writeStoreError(w, r, "moments active failed", err)
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"), 20)
	history, err := h.store.History(r.Context(), h.tenantID, channel, limit)
	if err != nil {
		h.writeStoreError(w, r, "moments history failed", err)
		return
	}
	out := make([]map[string]any, 0, len(history))
	for _, m := range history {
		out = append(out, momentJSON(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": activeJSON, "history": out})
}

// Open handles POST /api/v1/moments.
// Body: {channel, title, window_sec?}. It starts a moment and pushes the
// moment.opened overlay alert.
func (h *Moments) Open(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	var req struct {
		Channel   string `json:"channel"`
		Title     string `json:"title"`
		WindowSec int    `json:"window_sec"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := channelFromRequest(r, req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	window := clampWindow(time.Duration(req.WindowSec) * time.Second)
	m, err := h.store.Open(r.Context(), h.tenantID, channel, req.Title, "dashboard", window)
	if err != nil {
		if errors.Is(err, moments.ErrActiveExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a moment is already active"})
			return
		}
		h.writeStoreError(w, r, "moments open failed", err)
		return
	}
	if h.bc != nil {
		h.bc.Broadcast("moment.opened", map[string]any{
			"title":      m.Title,
			"window_sec": int(window.Seconds()),
		})
	}
	writeJSON(w, http.StatusOK, momentJSON(m))
}

// End handles POST /api/v1/moments/end.
// Body: {channel}. It closes the active moment and pushes moment.closed.
func (h *Moments) End(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	var req struct {
		Channel string `json:"channel"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := channelFromRequest(r, req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	m, err := h.store.End(r.Context(), h.tenantID, channel, time.Now())
	if err != nil {
		if errors.Is(err, moments.ErrNoActive) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active moment"})
			return
		}
		h.writeStoreError(w, r, "moments end failed", err)
		return
	}
	if h.bc != nil {
		h.bc.Broadcast("moment.closed", map[string]any{
			"title":        m.Title,
			"rarity":       string(m.Rarity),
			"participants": m.Participants,
		})
	}
	writeJSON(w, http.StatusOK, momentJSON(m))
}

// Participants handles GET /api/v1/moments/{momentID}/participants?channel=...
// It returns the usernames who reacted to a given moment.
func (h *Moments) Participants(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	momentID := chi.URLParam(r, "momentID")
	names, err := h.store.Participants(r.Context(), h.tenantID, channel, momentID)
	if err != nil {
		if errors.Is(err, moments.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "moment not found"})
			return
		}
		h.writeStoreError(w, r, "moments participants failed", err)
		return
	}
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"participants": names})
}

func (h *Moments) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func (h *Moments) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, moments.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func (h *Moments) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "moments_not_enabled"})
}

// clampWindow keeps a requested window inside [momentWindowMin, momentWindowMax],
// defaulting a zero/negative request to one minute.
func clampWindow(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Minute
	}
	if d < momentWindowMin {
		return momentWindowMin
	}
	if d > momentWindowMax {
		return momentWindowMax
	}
	return d
}

func parseLimit(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// momentJSON renders a Moment into the wire shape with RFC3339 timestamps.
func momentJSON(m moments.Moment) map[string]any {
	out := map[string]any{
		"id":           m.ID,
		"channel":      m.Channel,
		"title":        m.Title,
		"status":       m.Status,
		"rarity":       string(m.Rarity),
		"participants": m.Participants,
		"opened_by":    m.OpenedBy,
		"opened_at":    m.OpenedAt.Format(time.RFC3339),
		"closes_at":    m.ClosesAt.Format(time.RFC3339),
	}
	if !m.ClosedAt.IsZero() {
		out["closed_at"] = m.ClosedAt.Format(time.RFC3339)
	}
	return out
}

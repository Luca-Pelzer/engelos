package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/counters"
)

// Counters exposes CRUD over the named-integer counters store. All endpoints
// are session-protected at the router layer; Counters itself trusts the user
// injected by SessionAuth. Counters are persisted under the lower-cased
// channel login so dashboard edits match the chat engine's lookup key. There
// is no dedicated create endpoint: Set with an absolute value upserts the
// counter, and List drives the dashboard display.
type Counters struct {
	store    counters.Store
	tenantID string
	logger   *slog.Logger
}

// NewCounters constructs the Counters handler bundle. When store is nil every
// endpoint short-circuits to 501 so the router boots without the feature.
func NewCounters(store counters.Store, tenantID string, logger *slog.Logger) *Counters {
	if logger == nil {
		logger = slog.Default()
	}
	return &Counters{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/counters?channel=...
func (h *Counters) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	list, err := h.store.List(r.Context(), h.tenantID, channel)
	if err != nil {
		h.writeStoreError(w, r, "counters list failed", err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, c := range list {
		out = append(out, counterJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel":  channel,
		"counters": out,
	})
}

// Set handles PUT /api/v1/counters/{name}.
// Body: {channel, value}. name comes from the path, not the body. value is
// required and assigns an absolute count, creating the counter if absent.
func (h *Counters) Set(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	var req struct {
		Channel string `json:"channel"`
		Value   *int64 `json:"value"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if req.Value == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "value is required"})
		return
	}
	c, err := h.store.Set(r.Context(), h.tenantID, channel, name, *req.Value)
	if err != nil {
		h.writeStoreError(w, r, "counters set failed", err)
		return
	}
	writeJSON(w, http.StatusOK, counterJSON(c))
}

// Add handles POST /api/v1/counters/{name}/add.
// Body: {channel, delta}. name comes from the path, not the body. delta is
// required (and may be negative); the counter is created at 0 first when
// absent. The response carries the NEW value after the increment.
func (h *Counters) Add(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	var req struct {
		Channel string `json:"channel"`
		Delta   *int64 `json:"delta"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if req.Delta == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "delta is required"})
		return
	}
	c, err := h.store.Add(r.Context(), h.tenantID, channel, name, *req.Delta)
	if err != nil {
		h.writeStoreError(w, r, "counters add failed", err)
		return
	}
	writeJSON(w, http.StatusOK, counterJSON(c))
}

// Delete handles DELETE /api/v1/counters/{name}?channel=...
func (h *Counters) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if err := h.store.Delete(r.Context(), h.tenantID, channel, name); err != nil {
		h.writeStoreError(w, r, "counters delete failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeStoreError maps store sentinel errors to HTTP status codes.
// ErrNotFound → 404, ErrInvalid and anything else → 400. Counters has no
// conflict sentinel because Add and Set upsert.
func (h *Counters) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, counters.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "counter not found"})
	case errors.Is(err, counters.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func (h *Counters) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func (h *Counters) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "counters_not_enabled"})
}

// counterJSON renders a Counter into the wire shape shared by List, Set and
// Add, with an RFC3339 timestamp.
func counterJSON(c counters.Counter) map[string]any {
	return map[string]any{
		"id":         c.ID,
		"name":       c.Name,
		"value":      c.Value,
		"updated_at": c.UpdatedAt.Format(time.RFC3339),
	}
}

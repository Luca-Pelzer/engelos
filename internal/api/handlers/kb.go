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

	"github.com/Luca-Pelzer/engelos/internal/kb"
)

// KB exposes CRUD + full-text search over the per-channel knowledge base.
// Endpoints are session-protected at the router layer, which additionally gates
// writes to owner/admin and reads to owner/mod. Entries are keyed by the
// lower-cased channel login so dashboard edits match the chat engine's lookup
// key.
type KB struct {
	store    kb.Store
	tenantID string
	logger   *slog.Logger
}

// NewKB constructs the KB handler. When store is nil every endpoint
// short-circuits to 501 so the router boots without the feature.
func NewKB(store kb.Store, tenantID string, logger *slog.Logger) *KB {
	if logger == nil {
		logger = slog.Default()
	}
	return &KB{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/channels/{slug}/kb?query=&category=&limit=. With a
// query it runs FTS search (best match first); without one it lists entries
// newest-first. category filters both paths; limit defaults to 50 for a list
// and is capped by the store for a search.
func (h *KB) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	limit := parseLimit(r.URL.Query().Get("limit"), 50)

	var (
		list []kb.Entry
		err  error
	)
	if q != "" {
		list, err = h.store.Search(r.Context(), h.tenantID, channel, q, category, limit)
	} else {
		list, err = h.store.List(r.Context(), h.tenantID, channel, category, limit)
	}
	if err != nil {
		h.writeStoreError(w, r, "kb list failed", err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, kbJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": channel,
		"query":   q,
		"entries": out,
	})
}

type kbWriteRequest struct {
	Channel  string `json:"channel"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Enabled  *bool  `json:"enabled"`
}

// Create handles POST /api/v1/channels/{slug}/kb. Body: {category, title,
// content, enabled?}. enabled defaults to true when omitted.
func (h *KB) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	var req kbWriteRequest
	if !h.decode(w, r, &req) {
		return
	}
	channel := channelFromRequest(r, req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	e := kb.Entry{
		TenantID: h.tenantID,
		Channel:  channel,
		Category: req.Category,
		Title:    req.Title,
		Content:  req.Content,
		Enabled:  req.Enabled == nil || *req.Enabled,
	}
	created, err := h.store.Create(r.Context(), e)
	if err != nil {
		h.writeStoreError(w, r, "kb create failed", err)
		return
	}
	writeJSON(w, http.StatusCreated, kbJSON(created))
}

// Update handles PUT /api/v1/channels/{slug}/kb/{id}. Body carries the mutable
// fields; id comes from the path. enabled defaults to true when omitted.
func (h *KB) Update(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	var req kbWriteRequest
	if !h.decode(w, r, &req) {
		return
	}
	channel := channelFromRequest(r, req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	e := kb.Entry{
		ID:       id,
		TenantID: h.tenantID,
		Channel:  channel,
		Category: req.Category,
		Title:    req.Title,
		Content:  req.Content,
		Enabled:  req.Enabled == nil || *req.Enabled,
	}
	updated, err := h.store.Update(r.Context(), e)
	if err != nil {
		h.writeStoreError(w, r, "kb update failed", err)
		return
	}
	writeJSON(w, http.StatusOK, kbJSON(updated))
}

// Delete handles DELETE /api/v1/channels/{slug}/kb/{id}?channel=...
func (h *KB) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	channel := channelFromRequest(r, r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if err := h.store.Delete(r.Context(), h.tenantID, channel, id); err != nil {
		h.writeStoreError(w, r, "kb delete failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeStoreError maps store sentinel errors to HTTP status codes.
// ErrNotFound → 404, ErrInvalid → 400, anything else → 400 with the detail.
func (h *KB) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, kb.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case errors.Is(err, kb.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func (h *KB) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func (h *KB) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "kb_not_enabled"})
}

// kbJSON renders an Entry into the wire shape shared by List, Search, Create and
// Update, with RFC3339 timestamps.
func kbJSON(e kb.Entry) map[string]any {
	return map[string]any{
		"id":         e.ID,
		"category":   e.Category,
		"title":      e.Title,
		"content":    e.Content,
		"enabled":    e.Enabled,
		"created_at": e.CreatedAt.Format(time.RFC3339),
		"updated_at": e.UpdatedAt.Format(time.RFC3339),
	}
}

package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/quotes"
)

// Quotes exposes CRUD over the per-channel quotes store. Endpoints are
// session-protected at the router layer. Quotes are keyed by the lower-cased
// channel login so dashboard edits match the chat engine's lookup key.
type Quotes struct {
	store    quotes.Store
	tenantID string
	logger   *slog.Logger
}

// NewQuotes constructs the Quotes handler. When store is nil every endpoint
// short-circuits to 501 so the router boots without the feature.
func NewQuotes(store quotes.Store, tenantID string, logger *slog.Logger) *Quotes {
	if logger == nil {
		logger = slog.Default()
	}
	return &Quotes{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/quotes?channel=...
func (h *Quotes) List(w http.ResponseWriter, r *http.Request) {
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
		h.logger.Error("quotes list failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, q := range list {
		out = append(out, quoteJSON(q))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel, "quotes": out})
}

type quoteCreateRequest struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

// Create handles POST /api/v1/quotes. Body: {channel, text}.
func (h *Quotes) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req quoteCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	q, err := h.store.Add(r.Context(), h.tenantID, channel, req.Text, "dashboard")
	if err != nil {
		if errors.Is(err, quotes.ErrInvalid) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_text"})
			return
		}
		h.logger.Error("quotes add failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusCreated, quoteJSON(q))
}

// Delete handles DELETE /api/v1/quotes/{number}?channel=...
func (h *Quotes) Delete(w http.ResponseWriter, r *http.Request) {
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
		if errors.Is(err, quotes.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		h.logger.Error("quotes delete failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func quoteJSON(q quotes.Quote) map[string]any {
	return map[string]any{
		"number":     q.Number,
		"text":       q.Text,
		"created_by": q.CreatedBy,
		"created_at": q.CreatedAt,
	}
}

package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/rewards"
)

// Rewards exposes CRUD over the per-channel rewards catalog store. Endpoints
// are session-protected at the router layer and channel-scoped by the
// lower-cased channel login.
type Rewards struct {
	store    rewards.Store
	tenantID string
	logger   *slog.Logger
}

// NewRewards constructs the Rewards handler. When store is nil every endpoint
// short-circuits to 501 so the router boots without the feature.
func NewRewards(store rewards.Store, tenantID string, logger *slog.Logger) *Rewards {
	if logger == nil {
		logger = slog.Default()
	}
	return &Rewards{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/rewards?channel=...
func (h *Rewards) List(w http.ResponseWriter, r *http.Request) {
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
		h.logger.Error("rewards list failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, rw := range list {
		out = append(out, rewardJSON(rw))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel, "rewards": out})
}

type rewardWriteRequest struct {
	Channel     string `json:"channel"`
	Name        string `json:"name"`
	Cost        int64  `json:"cost"`
	Description string `json:"description"`
}

// Create handles POST /api/v1/rewards. Body: {channel, name, cost, description}.
func (h *Rewards) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req rewardWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel and name are required"})
		return
	}
	rw, err := h.store.Create(r.Context(), rewards.Reward{
		TenantID:    h.tenantID,
		Channel:     channel,
		Name:        strings.TrimSpace(req.Name),
		Cost:        req.Cost,
		Description: req.Description,
		CreatedBy:   "dashboard",
	})
	if err != nil {
		switch {
		case errors.Is(err, rewards.ErrAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already_exists"})
		case errors.Is(err, rewards.ErrInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid"})
		default:
			h.logger.Error("rewards create failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		}
		return
	}
	writeJSON(w, http.StatusCreated, rewardJSON(rw))
}

// Update handles PUT /api/v1/rewards/{name}. Body: {channel, cost, description}.
func (h *Rewards) Update(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	var req rewardWriteRequest
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
	rw, err := h.store.Update(r.Context(), h.tenantID, channel, name, req.Cost, req.Description)
	if err != nil {
		if errors.Is(err, rewards.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		h.logger.Error("rewards update failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, rewardJSON(rw))
}

// Delete handles DELETE /api/v1/rewards/{name}?channel=...
func (h *Rewards) Delete(w http.ResponseWriter, r *http.Request) {
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
		if errors.Is(err, rewards.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		h.logger.Error("rewards delete failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func rewardJSON(r rewards.Reward) map[string]any {
	return map[string]any{
		"name":        r.Name,
		"cost":        r.Cost,
		"description": r.Description,
	}
}

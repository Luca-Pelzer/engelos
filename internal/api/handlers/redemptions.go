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

	"github.com/Luca-Pelzer/engelos/internal/redemptions"
)

// Redemptions exposes CRUD over the reward→action bindings store. All
// endpoints are session-protected at the router layer; Redemptions itself
// trusts the user injected by SessionAuth. Bindings are persisted under the
// lower-cased channel login so dashboard-created bindings match the
// executor's lookup key.
type Redemptions struct {
	store    redemptions.Store
	tenantID string
	logger   *slog.Logger
}

// NewRedemptions constructs the Redemptions handler bundle. When store is
// nil every endpoint short-circuits to 501 so the router boots without the
// feature.
func NewRedemptions(store redemptions.Store, tenantID string, logger *slog.Logger) *Redemptions {
	if logger == nil {
		logger = slog.Default()
	}
	return &Redemptions{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/redemptions?channel=...
func (h *Redemptions) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	bindings, err := h.store.List(r.Context(), h.tenantID, channel)
	if err != nil {
		h.logger.WarnContext(r.Context(), "redemptions list failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, bindingJSON(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel":  channel,
		"bindings": out,
	})
}

// Create handles POST /api/v1/redemptions.
// Body: {channel, reward_id, reward_title?, action_type, action_param?, enabled?, auto_fulfill?}.
// enabled defaults to true when omitted; auto_fulfill defaults to false.
func (h *Redemptions) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	var req struct {
		Channel     string `json:"channel"`
		RewardID    string `json:"reward_id"`
		RewardTitle string `json:"reward_title"`
		ActionType  string `json:"action_type"`
		ActionParam string `json:"action_param"`
		Enabled     *bool  `json:"enabled"`
		AutoFulfill bool   `json:"auto_fulfill"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	binding := redemptions.Binding{
		RewardID:    req.RewardID,
		RewardTitle: req.RewardTitle,
		ActionType:  req.ActionType,
		ActionParam: req.ActionParam,
		Enabled:     enabled,
		AutoFulfill: req.AutoFulfill,
	}
	created, err := h.store.Create(r.Context(), h.tenantID, channel, binding)
	if err != nil {
		h.writeStoreError(w, r, "redemptions create failed", err)
		return
	}
	writeJSON(w, http.StatusCreated, bindingJSON(created))
}

// Update handles PUT /api/v1/redemptions/{rewardID}.
// Body: {channel, reward_title?, action_type, action_param?, enabled?, auto_fulfill?}.
// reward_id comes from the path, not the body. Because Update replaces all
// mutable fields, omitted enabled defaults to true.
func (h *Redemptions) Update(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	rewardID := strings.TrimSpace(chi.URLParam(r, "rewardID"))
	if rewardID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reward_id is required"})
		return
	}
	var req struct {
		Channel     string `json:"channel"`
		RewardTitle string `json:"reward_title"`
		ActionType  string `json:"action_type"`
		ActionParam string `json:"action_param"`
		Enabled     *bool  `json:"enabled"`
		AutoFulfill bool   `json:"auto_fulfill"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	binding := redemptions.Binding{
		RewardTitle: req.RewardTitle,
		ActionType:  req.ActionType,
		ActionParam: req.ActionParam,
		Enabled:     enabled,
		AutoFulfill: req.AutoFulfill,
	}
	updated, err := h.store.Update(r.Context(), h.tenantID, channel, rewardID, binding)
	if err != nil {
		h.writeStoreError(w, r, "redemptions update failed", err)
		return
	}
	writeJSON(w, http.StatusOK, bindingJSON(updated))
}

// SetEnabled handles POST /api/v1/redemptions/{rewardID}/enabled.
// Body: {channel, enabled}. enabled is required.
func (h *Redemptions) SetEnabled(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	rewardID := strings.TrimSpace(chi.URLParam(r, "rewardID"))
	if rewardID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reward_id is required"})
		return
	}
	var req struct {
		Channel string `json:"channel"`
		Enabled *bool  `json:"enabled"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if req.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled is required"})
		return
	}
	if err := h.store.SetEnabled(r.Context(), h.tenantID, channel, rewardID, *req.Enabled); err != nil {
		h.writeStoreError(w, r, "redemptions set enabled failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reward_id": rewardID,
		"enabled":   *req.Enabled,
	})
}

// Delete handles DELETE /api/v1/redemptions/{rewardID}?channel=...
func (h *Redemptions) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	rewardID := strings.TrimSpace(chi.URLParam(r, "rewardID"))
	if rewardID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reward_id is required"})
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if err := h.store.Delete(r.Context(), h.tenantID, channel, rewardID); err != nil {
		h.writeStoreError(w, r, "redemptions delete failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeStoreError maps store sentinel errors to HTTP status codes. ErrNotFound
// → 404, ErrConflict → 409, ErrInvalid and anything else → 400.
func (h *Redemptions) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, redemptions.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "binding not found"})
	case errors.Is(err, redemptions.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "binding already exists for this reward"})
	case errors.Is(err, redemptions.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func (h *Redemptions) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func (h *Redemptions) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "redemptions_not_enabled"})
}

// normChannel lower-cases and trims a channel login so stored bindings match
// the executor's lookup key (strings.ToLower(strings.TrimSpace(login))).
func normChannel(channel string) string {
	return strings.ToLower(strings.TrimSpace(channel))
}

// bindingJSON renders a Binding into the wire shape shared by List, Create
// and Update, with RFC3339 timestamps.
func bindingJSON(b redemptions.Binding) map[string]any {
	return map[string]any{
		"reward_id":    b.RewardID,
		"reward_title": b.RewardTitle,
		"action_type":  b.ActionType,
		"action_param": b.ActionParam,
		"enabled":      b.Enabled,
		"auto_fulfill": b.AutoFulfill,
		"created_at":   b.CreatedAt.Format(time.RFC3339),
		"updated_at":   b.UpdatedAt.Format(time.RFC3339),
	}
}

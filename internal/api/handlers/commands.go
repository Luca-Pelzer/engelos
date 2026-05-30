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

	"github.com/Luca-Pelzer/engelos/internal/customcommands"
)

// Commands exposes CRUD over the custom-commands store. All endpoints are
// session-protected at the router layer; Commands itself trusts the user
// injected by SessionAuth. Commands are persisted under the lower-cased
// channel login so dashboard-created commands match the chat engine's
// lookup key.
type Commands struct {
	store    customcommands.Store
	tenantID string
	logger   *slog.Logger
}

// NewCommands constructs the Commands handler bundle. When store is nil
// every endpoint short-circuits to 501 so the router boots without the
// feature.
func NewCommands(store customcommands.Store, tenantID string, logger *slog.Logger) *Commands {
	if logger == nil {
		logger = slog.Default()
	}
	return &Commands{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// List handles GET /api/v1/commands?channel=...
func (h *Commands) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	channel := normChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	commands, err := h.store.List(r.Context(), h.tenantID, channel)
	if err != nil {
		h.logger.WarnContext(r.Context(), "commands list failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(commands))
	for _, c := range commands {
		out = append(out, commandJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel":  channel,
		"commands": out,
	})
}

// Create handles POST /api/v1/commands.
// Body: {channel, name, response, min_role?, created_by?}.
// min_role defaults to "everyone" when omitted or empty.
func (h *Commands) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		h.notImplemented(w)
		return
	}
	var req struct {
		Channel   string `json:"channel"`
		Name      string `json:"name"`
		Response  string `json:"response"`
		MinRole   string `json:"min_role"`
		CreatedBy string `json:"created_by"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	minRole := strings.TrimSpace(req.MinRole)
	if minRole == "" {
		minRole = "everyone"
	}
	cmd := customcommands.CustomCommand{
		TenantID:  h.tenantID,
		Channel:   channel,
		Name:      req.Name,
		Response:  req.Response,
		MinRole:   minRole,
		CreatedBy: req.CreatedBy,
	}
	created, err := h.store.Create(r.Context(), cmd)
	if err != nil {
		h.writeStoreError(w, r, "commands create failed", err)
		return
	}
	writeJSON(w, http.StatusCreated, commandJSON(created))
}

// Update handles PUT /api/v1/commands/{name}.
// Body: {channel, response, min_role?}. name comes from the path, not the
// body. min_role defaults to "everyone" when omitted or empty.
func (h *Commands) Update(w http.ResponseWriter, r *http.Request) {
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
		Channel  string `json:"channel"`
		Response string `json:"response"`
		MinRole  string `json:"min_role"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	channel := normChannel(req.Channel)
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	minRole := strings.TrimSpace(req.MinRole)
	if minRole == "" {
		minRole = "everyone"
	}
	updated, err := h.store.Update(r.Context(), h.tenantID, channel, name, req.Response, minRole)
	if err != nil {
		h.writeStoreError(w, r, "commands update failed", err)
		return
	}
	writeJSON(w, http.StatusOK, commandJSON(updated))
}

// Delete handles DELETE /api/v1/commands/{name}?channel=...
func (h *Commands) Delete(w http.ResponseWriter, r *http.Request) {
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
		h.writeStoreError(w, r, "commands delete failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeStoreError maps store sentinel errors to HTTP status codes.
// ErrNotFound → 404, ErrAlreadyExists → 409, ErrInvalid and anything else
// → 400.
func (h *Commands) writeStoreError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, customcommands.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "command not found"})
	case errors.Is(err, customcommands.ErrAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "command already exists"})
	case errors.Is(err, customcommands.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		h.logger.WarnContext(r.Context(), msg, slog.Any("err", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func (h *Commands) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func (h *Commands) notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "commands_not_enabled"})
}

// commandJSON renders a CustomCommand into the wire shape shared by List,
// Create and Update, with RFC3339 timestamps.
func commandJSON(c customcommands.CustomCommand) map[string]any {
	return map[string]any{
		"id":         c.ID,
		"name":       c.Name,
		"response":   c.Response,
		"min_role":   c.MinRole,
		"created_by": c.CreatedBy,
		"created_at": c.CreatedAt.Format(time.RFC3339),
		"updated_at": c.UpdatedAt.Format(time.RFC3339),
	}
}

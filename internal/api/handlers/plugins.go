package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/plugins"
)

// Plugins serves the plugin catalog and its restart-based enable/disable
// toggles. Because chat commands and chi routes only wire at startup, a toggle
// PERSISTS IMMEDIATELY but takes effect on the next restart: every view reports
// enabled (what is mounted in this process) alongside desired (the persisted
// state), so the dashboard can surface "restart required". A nil registry or
// state store makes the endpoints return 501 so the router boots without them.
type Plugins struct {
	registry *plugins.Registry
	state    plugins.StateStore
	active   map[string]bool
	tenantID string
	logger   *slog.Logger
}

// NewPlugins constructs the handler. active is the snapshot of resolved-enabled
// plugin ids captured at startup (what is actually mounted this process); it is
// read-only and never mutated by a toggle, which only writes desired state.
func NewPlugins(registry *plugins.Registry, state plugins.StateStore, active map[string]bool, tenantID string, logger *slog.Logger) *Plugins {
	if logger == nil {
		logger = slog.Default()
	}
	return &Plugins{
		registry: registry,
		state:    state,
		active:   active,
		tenantID: strings.TrimSpace(tenantID),
		logger:   logger,
	}
}

type pluginView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Tier            string `json:"tier"`
	SettingsHref    string `json:"settings_href,omitempty"`
	DefaultEnabled  bool   `json:"default_enabled"`
	Enabled         bool   `json:"enabled"`
	Desired         bool   `json:"desired"`
	RestartRequired bool   `json:"restart_required"`
}

// List handles GET /api/v1/plugins, returning every registered plugin with its
// default, current (mounted) and desired (persisted) enablement.
func (h *Plugins) List(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		notImplemented(w)
		return
	}
	out := make([]pluginView, 0)
	for _, m := range h.registry.List() {
		desired := h.desired(r.Context(), m.ID, m.DefaultEnabled)
		current := h.active[m.ID]
		out = append(out, pluginView{
			ID:              m.ID,
			Name:            m.Name,
			Description:     m.Description,
			Tier:            string(m.Tier),
			SettingsHref:    m.SettingsHref,
			DefaultEnabled:  m.DefaultEnabled,
			Enabled:         current,
			Desired:         desired,
			RestartRequired: current != desired,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// Update handles PUT /api/v1/plugins/{id} with body {"enabled": bool}. It
// persists the desired state and echoes the resulting view; the change takes
// effect on the next restart, so restart_required is true whenever the new
// desired state differs from what is currently mounted.
func (h *Plugins) Update(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil || h.state == nil {
		notImplemented(w)
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	m, ok := h.registry.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled is required"})
		return
	}
	if err := h.state.Set(r.Context(), h.tenantID, id, *req.Enabled); err != nil {
		h.logger.Error("plugin state set failed", "plugin", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	man := m.Manifest()
	current := h.active[id]
	writeJSON(w, http.StatusOK, pluginView{
		ID:              man.ID,
		Name:            man.Name,
		Description:     man.Description,
		Tier:            string(man.Tier),
		SettingsHref:    man.SettingsHref,
		DefaultEnabled:  man.DefaultEnabled,
		Enabled:         current,
		Desired:         *req.Enabled,
		RestartRequired: current != *req.Enabled,
	})
}

// desired returns the persisted desired state for a plugin, falling back to def
// when no state store is wired or no explicit toggle is stored.
func (h *Plugins) desired(ctx context.Context, id string, def bool) bool {
	if h.state == nil {
		return def
	}
	got, err := h.state.GetOrDefault(ctx, h.tenantID, id, def)
	if err != nil {
		h.logger.Warn("plugin desired-state read failed", "plugin", id, "err", err)
		return def
	}
	return got
}

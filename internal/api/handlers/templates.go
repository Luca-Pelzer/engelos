package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

// Templates serves the read-only catalog of built-in workflow templates and
// instantiates one into a channel. A nil store makes Apply return 501 so the
// route can be mounted defensively; the catalog is static and always available.
type Templates struct {
	store    actions.Store
	tenantID string
	logger   *slog.Logger
}

// NewTemplates constructs the templates handler. When store is nil Apply
// short-circuits to 501 while List still serves the static catalog.
func NewTemplates(store actions.Store, tenantID string, logger *slog.Logger) *Templates {
	if logger == nil {
		logger = slog.Default()
	}
	return &Templates{
		store:    store,
		tenantID: strings.TrimSpace(tenantID),
		logger:   logger,
	}
}

// List handles GET /api/v1/actions/templates, returning the built-in workflow
// templates so the dashboard can offer one-click rule creation.
func (h *Templates) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"templates": actions.Templates()})
}

// Apply handles POST /api/v1/channels/{channelSlug}/actions/templates/{id}/apply:
// it instantiates the named template as a new rule in the workspace-scoped
// channel, returning 409 when a rule of that name already exists there.
func (h *Templates) Apply(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	channel := channelFromRequest(r, "")
	if channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	tmpl, ok := actions.TemplateByID(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	rule := tmpl.Rule
	rule.TenantID = h.tenantID
	rule.Channel = channel

	ru, err := h.store.Create(r.Context(), rule)
	if err != nil {
		switch {
		case errors.Is(err, actions.ErrAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already_exists"})
		case errors.Is(err, actions.ErrInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid"})
		default:
			h.logger.Error("templates apply failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		}
		return
	}
	writeJSON(w, http.StatusCreated, ruleJSON(ru))
}

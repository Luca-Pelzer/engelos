package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/integrations"
)

// IntegrationCredentials is the credential surface the integrations endpoints
// need. The integrations credential store satisfies it. A nil store makes the
// credential endpoints 501 and every integration report disconnected.
type IntegrationCredentials interface {
	ListKeys(ctx context.Context, tenantID, integrationID string) ([]string, error)
	Put(ctx context.Context, tenantID, integrationID, key, plaintext string) error
	Delete(ctx context.Context, tenantID, integrationID string) error
}

// Integrations serves the tenant-level integrations catalog and credential
// management. It is owner/admin-gated at the router layer.
type Integrations struct {
	registry *integrations.Registry
	creds    IntegrationCredentials
	tenantID string
	logger   *slog.Logger
}

// NewIntegrations constructs the handler. A nil registry makes every endpoint
// 501 so the router boots without the framework wired.
func NewIntegrations(registry *integrations.Registry, creds IntegrationCredentials, tenantID string, logger *slog.Logger) *Integrations {
	if logger == nil {
		logger = slog.Default()
	}
	return &Integrations{registry: registry, creds: creds, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

type integrationView struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Icon           string   `json:"icon,omitempty"`
	AuthKind       string   `json:"auth_kind"`
	SetupHref      string   `json:"setup_href,omitempty"`
	Connected      bool     `json:"connected"`
	CredentialKeys []string `json:"credential_keys"`
}

// List handles GET /api/v1/integrations, returning each registered manifest with
// its connection state and stored credential key names (never values).
func (h *Integrations) List(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		notImplemented(w)
		return
	}
	out := make([]integrationView, 0)
	for _, m := range h.registry.List() {
		keys := h.keysFor(r.Context(), m.ID)
		out = append(out, integrationView{
			ID:             m.ID,
			Name:           m.Name,
			Description:    m.Description,
			Icon:           m.Icon,
			AuthKind:       string(m.AuthKind),
			SetupHref:      m.SetupHref,
			Connected:      h.connected(r.Context(), m.ID, keys),
			CredentialKeys: keys,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type credMask struct {
	Set  bool   `json:"set"`
	Hint string `json:"hint,omitempty"`
}

// PutCredentials handles PUT /api/v1/integrations/{id}/credentials: it encrypts
// and stores every supplied value, then responds with a masked view (set + a
// non-reversible hint) that NEVER echoes a plaintext value.
func (h *Integrations) PutCredentials(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil || h.creds == nil {
		notImplemented(w)
		return
	}
	id := chi.URLParam(r, "id")
	if _, ok := h.registry.Get(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	var req struct {
		Values map[string]string `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	if len(req.Values) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "values is required"})
		return
	}
	masked := map[string]credMask{}
	for key, val := range req.Values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if err := h.creds.Put(r.Context(), h.tenantID, id, key, val); err != nil {
			h.logger.Error("integration credential store failed", "integration", id, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
			return
		}
		masked[key] = credMask{Set: val != "", Hint: hintSecret(val)}
	}
	keys := h.keysFor(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          id,
		"connected":   len(keys) > 0,
		"credentials": masked,
	})
}

// DeleteCredentials handles DELETE /api/v1/integrations/{id}/credentials,
// revoking every stored credential for the integration.
func (h *Integrations) DeleteCredentials(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil || h.creds == nil {
		notImplemented(w)
		return
	}
	id := chi.URLParam(r, "id")
	if _, ok := h.registry.Get(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if err := h.creds.Delete(r.Context(), h.tenantID, id); err != nil {
		h.logger.Error("integration credential revoke failed", "integration", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "connected": false})
}

// connected reports whether an integration is connected: an integration that
// implements ConnectionProber answers from its own config source, otherwise the
// answer falls back to whether it has stored credentials.
func (h *Integrations) connected(ctx context.Context, id string, keys []string) bool {
	if integ, ok := h.registry.Get(id); ok {
		if prober, ok := integ.(integrations.ConnectionProber); ok {
			return prober.Connected(ctx, h.tenantID)
		}
	}
	return len(keys) > 0
}

// keysFor returns the stored credential key names for an integration, or an
// empty slice when no store is wired or the lookup fails.
func (h *Integrations) keysFor(ctx context.Context, id string) []string {
	if h.creds == nil {
		return []string{}
	}
	keys, err := h.creds.ListKeys(ctx, h.tenantID, id)
	if err != nil {
		h.logger.Warn("integration list keys failed", "integration", id, "err", err)
		return []string{}
	}
	if keys == nil {
		return []string{}
	}
	return keys
}

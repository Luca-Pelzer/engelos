package handlers

import (
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

// Connections exposes the current user's linked OAuth identities (Twitch
// today, Discord/Spotify later) to the dashboard so a streamer can see
// whether the bot is authorized, as whom, with which scopes, and whether the
// stored token is still valid. It is read-only; (re)authorization happens by
// redirecting to the provider login routes. When the store is nil every
// endpoint returns 501 so the router still boots with auth disabled.
type Connections struct {
	store    auth.Store
	tenantID string
	logger   *slog.Logger
}

// NewConnections constructs the handler bundle.
func NewConnections(store auth.Store, tenantID string, logger *slog.Logger) *Connections {
	if logger == nil {
		logger = slog.Default()
	}
	return &Connections{store: store, tenantID: tenantID, logger: logger}
}

// List handles GET /api/v1/connections. It returns the session user's linked
// identities. Tokens are never exposed; only metadata and derived status.
func (h *Connections) List(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	identities, err := h.store.GetOAuthIdentitiesByUser(r.Context(), h.tenantID, user.ID)
	if err != nil {
		h.logger.WarnContext(r.Context(), "connections list failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	out := make([]map[string]any, 0, len(identities))
	for _, id := range identities {
		out = append(out, connectionJSON(id))
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

// Delete handles DELETE /api/v1/connections/{id}. It unlinks one OAuth
// identity (a Twitch/Discord login or bot link). The target is verified to
// belong to the session user BEFORE deletion: DeleteOAuthIdentity takes a
// bare id, so without this ownership check any logged-in user could unlink
// another tenant's identity by guessing its ULID. Returns 404 (not 403) for
// an identity the caller does not own, so the endpoint never reveals whether
// an unowned id exists.
func (h *Connections) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_id"})
		return
	}
	identities, err := h.store.GetOAuthIdentitiesByUser(r.Context(), h.tenantID, user.ID)
	if err != nil {
		h.logger.WarnContext(r.Context(), "connections delete lookup failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	owned := false
	for _, ident := range identities {
		if ident.ID == id {
			owned = true
			break
		}
	}
	if !owned {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if err := h.store.DeleteOAuthIdentity(r.Context(), id); err != nil {
		h.logger.WarnContext(r.Context(), "connections delete failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

// Delete handles DELETE /api/v1/connections/{id}, unlinking one OAuth
// identity. It verifies the identity belongs to the session user before the
// bare-id store delete, returning 404 (not 403) for an id the caller does
// not own so the endpoint never reveals whether an unowned id exists.
func (h *Connections) Delete(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_id"})
		return
	}
	identities, err := h.store.GetOAuthIdentitiesByUser(r.Context(), h.tenantID, user.ID)
	if err != nil {
		h.logger.WarnContext(r.Context(), "connections delete lookup failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	owned := false
	for _, ident := range identities {
		if ident.ID == id {
			owned = true
			break
		}
	}
	if !owned {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if err := h.store.DeleteOAuthIdentity(r.Context(), id); err != nil {
		h.logger.WarnContext(r.Context(), "connections delete failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// connectionJSON renders an identity into the wire shape: metadata plus
// derived flags the dashboard needs (token expiry state, and whether the
// clip-creation scope was granted so the UI can warn before the auto-clipper
// silently no-ops).
func connectionJSON(id auth.OAuthIdentity) map[string]any {
	expired := !id.ExpiresAt.IsZero() && !id.ExpiresAt.After(time.Now().UTC())
	return map[string]any{
		"id":              id.ID,
		"provider":        id.Provider,
		"provider_login":  id.ProviderLogin,
		"purpose":         id.Purpose,
		"scopes":          id.Scopes,
		"can_create_clip": slices.Contains(id.Scopes, "clips:edit"),
		"expires_at":      oauthExpiry(id.ExpiresAt),
		"expired":         expired,
		"updated_at":      id.UpdatedAt.Format(time.RFC3339),
	}
}

// oauthExpiry formats a token expiry, returning an empty string when the
// provider issued no expiry (a NULL expires_at).
func oauthExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

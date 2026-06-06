package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/workspaces"
)

// Workspaces exposes the workspace, membership and invitation endpoints. The
// non-scoped routes (/me/workspaces, POST /workspaces) authorize on the session
// user; the scoped routes live behind WorkspaceMiddleware and read the resolved
// workspace + membership from the request context.
type Workspaces struct {
	store    workspaces.Store
	tenantID string
	logger   *slog.Logger
}

// NewWorkspaces constructs the handler. A nil store makes every endpoint return
// 501 so the router boots without the feature.
func NewWorkspaces(store workspaces.Store, tenantID string, logger *slog.Logger) *Workspaces {
	if logger == nil {
		logger = slog.Default()
	}
	return &Workspaces{store: store, tenantID: strings.TrimSpace(tenantID), logger: logger}
}

// ListMine handles GET /api/v1/me/workspaces, the channel-switcher payload: every
// workspace the session user is a member of, with their role.
func (h *Workspaces) ListMine(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	views, err := h.store.ListMembershipsForUser(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("workspaces list mine failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(views))
	for _, v := range views {
		out = append(out, map[string]any{
			"slug":         v.Workspace.Slug,
			"display_name": v.Workspace.DisplayName,
			"twitch_login": v.Workspace.TwitchLogin,
			"role":         string(v.Role),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": out})
}

type createWorkspaceRequest struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

// Create handles POST /api/v1/workspaces: onboarding. The session user becomes
// the owner of a new workspace whose slug is the channel login. The owner
// membership is created in the same step.
func (h *Workspaces) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req createWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	slug := normChannel(req.Slug)
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug is required"})
		return
	}
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		display = slug
	}
	ws, err := h.store.CreateWorkspace(r.Context(), workspaces.Workspace{
		TenantID:       h.tenantID,
		Slug:           slug,
		TwitchLogin:    slug,
		DisplayName:    display,
		OwnerUserID:    user.ID,
		AutoVerifyMods: true,
	})
	if err != nil {
		h.writeWriteError(w, err, "workspaces create failed")
		return
	}
	if _, err := h.store.UpsertMembership(r.Context(), workspaces.Membership{
		WorkspaceID: ws.ID,
		UserID:      user.ID,
		Role:        workspaces.RoleOwner,
		Source:      workspaces.SourceOwner,
	}); err != nil {
		h.logger.Error("workspaces owner membership failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	writeJSON(w, http.StatusCreated, workspaceJSON(ws, workspaces.RoleOwner))
}

// Get handles GET /api/v1/channels/{channelSlug}: the current workspace plus the
// caller's role, read straight from the workspace context the middleware set.
func (h *Workspaces) Get(w http.ResponseWriter, r *http.Request) {
	wc, ok := middleware.WorkspaceFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	writeJSON(w, http.StatusOK, workspaceJSON(wc.Workspace, wc.Membership.Role))
}

// ListMembers handles GET /api/v1/channels/{channelSlug}/members.
func (h *Workspaces) ListMembers(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	wc, ok := middleware.WorkspaceFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	members, err := h.store.ListMembers(r.Context(), wc.Workspace.ID)
	if err != nil {
		h.logger.Error("workspaces list members failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		out = append(out, map[string]any{
			"user_id": m.UserID,
			"role":    string(m.Role),
			"source":  string(m.Source),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

type inviteRequest struct {
	TwitchLogin string `json:"twitch_login"`
	Role        string `json:"role"`
}

// Invite handles POST /api/v1/channels/{channelSlug}/invitations (owner-only via
// RequireRole). It records a pending mod invitation addressed to a Twitch login;
// the grant is materialised into a membership when that login next signs in. v1
// invites mods only, so any role other than mod is rejected.
func (h *Workspaces) Invite(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	wc, ok := middleware.WorkspaceFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	user, _ := middleware.UserFromContext(r.Context())
	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	login := normChannel(req.TwitchLogin)
	if login == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "twitch_login is required"})
		return
	}
	role := workspaces.Role(strings.TrimSpace(req.Role))
	if role == "" {
		role = workspaces.RoleMod
	}
	if role != workspaces.RoleMod {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only mod invitations are supported"})
		return
	}
	inv, err := h.store.CreateInvitation(r.Context(), workspaces.Invitation{
		WorkspaceID: wc.Workspace.ID,
		TwitchLogin: login,
		Role:        role,
		InvitedBy:   user.ID,
	})
	if err != nil {
		h.writeWriteError(w, err, "workspaces invite failed")
		return
	}
	writeJSON(w, http.StatusCreated, invitationJSON(inv))
}

// ListInvitations handles GET /api/v1/channels/{channelSlug}/invitations
// (owner-only). It returns every invitation on the workspace, accepted or not,
// so the members UI can show pending and historical grants.
func (h *Workspaces) ListInvitations(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	wc, ok := middleware.WorkspaceFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	invites, err := h.store.ListInvitations(r.Context(), wc.Workspace.ID)
	if err != nil {
		h.logger.Error("workspaces list invitations failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	out := make([]map[string]any, 0, len(invites))
	for _, inv := range invites {
		out = append(out, invitationJSON(inv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": out})
}

// DeleteInvitation handles DELETE /api/v1/channels/{channelSlug}/invitations/{id}
// (owner-only): revoke a pending invitation. The id is verified to belong to the
// resolved workspace before deletion so an owner cannot revoke another channel's
// invitation by guessing its id.
func (h *Workspaces) DeleteInvitation(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	wc, ok := middleware.WorkspaceFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	invites, err := h.store.ListInvitations(r.Context(), wc.Workspace.ID)
	if err != nil {
		h.logger.Error("workspaces delete invitation lookup failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
		return
	}
	found := false
	for _, inv := range invites {
		if inv.ID == id {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	if err := h.store.DeleteInvitation(r.Context(), id); err != nil {
		h.writeWriteError(w, err, "workspaces delete invitation failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveMember handles DELETE /api/v1/channels/{channelSlug}/members/{userID}
// (owner-only): revoke a user's membership. Revoking the workspace owner is
// rejected so the channel never loses its owner, and the scope is the resolved
// workspace so an owner only affects members of their own channel.
func (h *Workspaces) RemoveMember(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	wc, ok := middleware.WorkspaceFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	userID := strings.TrimSpace(chi.URLParam(r, "userID"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userID is required"})
		return
	}
	if userID == wc.Workspace.OwnerUserID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove the workspace owner"})
		return
	}
	if err := h.store.DeleteMembership(r.Context(), wc.Workspace.ID, userID); err != nil {
		h.writeWriteError(w, err, "workspaces remove member failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type autoVerifyRequest struct {
	Enabled bool `json:"enabled"`
}

// SetAutoVerify handles PUT /api/v1/channels/{channelSlug}/auto-verify
// (owner-only): toggle whether a logging-in Twitch moderator of this channel is
// auto-granted mod access.
func (h *Workspaces) SetAutoVerify(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		notImplemented(w)
		return
	}
	wc, ok := middleware.WorkspaceFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var req autoVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	if err := h.store.SetAutoVerifyMods(r.Context(), wc.Workspace.ID, req.Enabled); err != nil {
		h.writeWriteError(w, err, "workspaces set auto-verify failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto_verify_mods": req.Enabled})
}

func (h *Workspaces) writeWriteError(w http.ResponseWriter, err error, logMsg string) {
	switch {
	case errors.Is(err, workspaces.ErrAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already_exists"})
	case errors.Is(err, workspaces.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid"})
	case errors.Is(err, workspaces.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	default:
		h.logger.Error(logMsg, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store_error"})
	}
}

func workspaceJSON(ws workspaces.Workspace, role workspaces.Role) map[string]any {
	return map[string]any{
		"slug":             ws.Slug,
		"display_name":     ws.DisplayName,
		"twitch_login":     ws.TwitchLogin,
		"auto_verify_mods": ws.AutoVerifyMods,
		"role":             string(role),
	}
}

func invitationJSON(inv workspaces.Invitation) map[string]any {
	out := map[string]any{
		"id":           inv.ID,
		"twitch_login": inv.TwitchLogin,
		"role":         string(inv.Role),
		"accepted":     inv.AcceptedAt != nil,
		"created_at":   inv.CreatedAt,
	}
	if inv.ExpiresAt != nil {
		out["expires_at"] = *inv.ExpiresAt
	}
	return out
}

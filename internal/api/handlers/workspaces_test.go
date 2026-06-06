package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/workspaces"
)

func newWSHandlerStore(t *testing.T) workspaces.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:wshandlertest-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := workspaces.OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedWorkspace(t *testing.T, store workspaces.Store, slug, ownerID string) workspaces.Workspace {
	t.Helper()
	ws, err := store.CreateWorkspace(context.Background(), workspaces.Workspace{
		TenantID: "local", Slug: slug, TwitchLogin: slug, DisplayName: slug, OwnerUserID: ownerID,
	})
	require.NoError(t, err)
	_, err = store.UpsertMembership(context.Background(), workspaces.Membership{
		WorkspaceID: ws.ID, UserID: ownerID, Role: workspaces.RoleOwner, Source: workspaces.SourceOwner,
	})
	require.NoError(t, err)
	return ws
}

// ownerCtxRequest builds a request whose context carries the owner user and the
// resolved workspace, mimicking what SessionAuth + WorkspaceMiddleware inject.
func ownerCtxRequest(t *testing.T, method, target, body string, ws workspaces.Workspace, ownerID string, urlParams map[string]string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	ctx := middleware.WithUser(r.Context(), auth.User{ID: ownerID, Role: auth.RoleOwner})
	ctx = middleware.WithWorkspace(ctx, middleware.WorkspaceContext{
		Workspace:  ws,
		Membership: workspaces.Membership{WorkspaceID: ws.ID, UserID: ownerID, Role: workspaces.RoleOwner},
	})
	if len(urlParams) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range urlParams {
			rctx.URLParams.Add(k, v)
		}
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	return r.WithContext(ctx)
}

func TestWorkspaces_Invite_CreatesPendingModInvitation(t *testing.T) {
	t.Parallel()
	store := newWSHandlerStore(t)
	ws := seedWorkspace(t, store, "streamer", "owner-1")
	h := NewWorkspaces(store, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := ownerCtxRequest(t, http.MethodPost, "/", `{"twitch_login":"Moddy"}`, ws, "owner-1", nil)
	rec := httptest.NewRecorder()
	h.Invite(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "moddy", got["twitch_login"])
	assert.Equal(t, "mod", got["role"])
	assert.Equal(t, false, got["accepted"])

	pending, err := store.ListPendingInvitationsForLogin(context.Background(), "moddy")
	require.NoError(t, err)
	require.Len(t, pending, 1)
}

func TestWorkspaces_Invite_RejectsNonModRole(t *testing.T) {
	t.Parallel()
	store := newWSHandlerStore(t)
	ws := seedWorkspace(t, store, "streamer", "owner-1")
	h := NewWorkspaces(store, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := ownerCtxRequest(t, http.MethodPost, "/", `{"twitch_login":"x","role":"owner"}`, ws, "owner-1", nil)
	rec := httptest.NewRecorder()
	h.Invite(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWorkspaces_RemoveMember_OwnerCannotBeRemoved(t *testing.T) {
	t.Parallel()
	store := newWSHandlerStore(t)
	ws := seedWorkspace(t, store, "streamer", "owner-1")
	h := NewWorkspaces(store, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := ownerCtxRequest(t, http.MethodDelete, "/members/owner-1", "", ws, "owner-1",
		map[string]string{"userID": "owner-1"})
	rec := httptest.NewRecorder()
	h.RemoveMember(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWorkspaces_RemoveMember_RevokesMod(t *testing.T) {
	t.Parallel()
	store := newWSHandlerStore(t)
	ws := seedWorkspace(t, store, "streamer", "owner-1")
	_, err := store.UpsertMembership(context.Background(), workspaces.Membership{
		WorkspaceID: ws.ID, UserID: "mod-9", Role: workspaces.RoleMod, Source: workspaces.SourceInvite,
	})
	require.NoError(t, err)
	h := NewWorkspaces(store, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := ownerCtxRequest(t, http.MethodDelete, "/members/mod-9", "", ws, "owner-1",
		map[string]string{"userID": "mod-9"})
	rec := httptest.NewRecorder()
	h.RemoveMember(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	members, err := store.ListMembers(context.Background(), ws.ID)
	require.NoError(t, err)
	assert.Len(t, members, 1)
}

func TestWorkspaces_DeleteInvitation_RejectsForeignID(t *testing.T) {
	t.Parallel()
	store := newWSHandlerStore(t)
	wsA := seedWorkspace(t, store, "streamer-a", "owner-a")
	wsB := seedWorkspace(t, store, "streamer-b", "owner-b")
	invB, err := store.CreateInvitation(context.Background(), workspaces.Invitation{
		WorkspaceID: wsB.ID, TwitchLogin: "x", Role: workspaces.RoleMod,
	})
	require.NoError(t, err)
	h := NewWorkspaces(store, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := ownerCtxRequest(t, http.MethodDelete, "/invitations/"+invB.ID, "", wsA, "owner-a",
		map[string]string{"id": invB.ID})
	rec := httptest.NewRecorder()
	h.DeleteInvitation(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, "owner of A must not delete an invitation of B")
	gone, err := store.ListInvitations(context.Background(), wsB.ID)
	require.NoError(t, err)
	assert.Len(t, gone, 1, "B's invitation must remain")
}

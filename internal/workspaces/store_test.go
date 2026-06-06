package workspaces

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "ws.db") + "?_pragma=busy_timeout(5000)"
	s, err := OpenSQLiteStore(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleWorkspace(slug, owner string) Workspace {
	return Workspace{TenantID: "default", Slug: slug, TwitchLogin: slug, OwnerUserID: owner, AutoVerifyMods: true}
}

func TestStoreWorkspaceCreateGetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	w, err := s.CreateWorkspace(ctx, sampleWorkspace("streamera", "user1"))
	require.NoError(t, err)
	assert.NotEmpty(t, w.ID)
	assert.False(t, w.CreatedAt.IsZero())

	got, err := s.GetWorkspaceBySlug(ctx, "default", "streamera")
	require.NoError(t, err)
	assert.Equal(t, w.ID, got.ID)
	assert.True(t, got.AutoVerifyMods)

	byID, err := s.GetWorkspaceByID(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, "streamera", byID.Slug)
}

func TestStoreWorkspaceDuplicateSlug(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.CreateWorkspace(ctx, sampleWorkspace("dup", "user1"))
	require.NoError(t, err)
	_, err = s.CreateWorkspace(ctx, sampleWorkspace("dup", "user2"))
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

func TestStoreWorkspaceNotFoundAndValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.GetWorkspaceBySlug(ctx, "default", "nope")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = s.CreateWorkspace(ctx, Workspace{TenantID: "default", Slug: "", OwnerUserID: "u"})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestStoreSetAutoVerifyMods(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	w, err := s.CreateWorkspace(ctx, sampleWorkspace("toggle", "user1"))
	require.NoError(t, err)
	require.NoError(t, s.SetAutoVerifyMods(ctx, w.ID, false))
	got, err := s.GetWorkspaceByID(ctx, w.ID)
	require.NoError(t, err)
	assert.False(t, got.AutoVerifyMods)
	assert.ErrorIs(t, s.SetAutoVerifyMods(ctx, "missing", true), ErrNotFound)
}

func TestStoreMembershipUpsertAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	wa, _ := s.CreateWorkspace(ctx, sampleWorkspace("chana", "owner1"))
	wb, _ := s.CreateWorkspace(ctx, sampleWorkspace("chanb", "owner2"))

	_, err := s.UpsertMembership(ctx, Membership{WorkspaceID: wa.ID, UserID: "owner1", Role: RoleOwner, Source: SourceOwner})
	require.NoError(t, err)
	_, err = s.UpsertMembership(ctx, Membership{WorkspaceID: wa.ID, UserID: "modder", Role: RoleMod, Source: SourceInvite})
	require.NoError(t, err)
	_, err = s.UpsertMembership(ctx, Membership{WorkspaceID: wb.ID, UserID: "modder", Role: RoleMod, Source: SourceTwitchVerified})
	require.NoError(t, err)

	views, err := s.ListMembershipsForUser(ctx, "modder")
	require.NoError(t, err)
	require.Len(t, views, 2)
	assert.Equal(t, "chana", views[0].Workspace.Slug)
	assert.Equal(t, RoleMod, views[0].Role)

	upgraded, err := s.UpsertMembership(ctx, Membership{WorkspaceID: wa.ID, UserID: "modder", Role: RoleOwner, Source: SourceOwner})
	require.NoError(t, err)
	assert.Equal(t, RoleOwner, upgraded.Role)
	m, err := s.GetMembership(ctx, wa.ID, "modder")
	require.NoError(t, err)
	assert.Equal(t, RoleOwner, m.Role)

	members, err := s.ListMembers(ctx, wa.ID)
	require.NoError(t, err)
	assert.Len(t, members, 2)

	require.NoError(t, s.DeleteMembership(ctx, wa.ID, "modder"))
	_, err = s.GetMembership(ctx, wa.ID, "modder")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreInvitationLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	w, _ := s.CreateWorkspace(ctx, sampleWorkspace("invchan", "owner1"))

	inv, err := s.CreateInvitation(ctx, Invitation{WorkspaceID: w.ID, TwitchLogin: "CoolMod", Role: RoleMod, InvitedBy: "owner1"})
	require.NoError(t, err)
	assert.NotEmpty(t, inv.Token)
	assert.Equal(t, "coolmod", inv.TwitchLogin)

	byTok, err := s.GetInvitationByToken(ctx, inv.Token)
	require.NoError(t, err)
	assert.Equal(t, inv.ID, byTok.ID)

	pending, err := s.ListPendingInvitationsForLogin(ctx, "coolmod")
	require.NoError(t, err)
	require.Len(t, pending, 1)

	require.NoError(t, s.MarkInvitationAccepted(ctx, inv.ID, time.Now().UTC()))
	pending, err = s.ListPendingInvitationsForLogin(ctx, "coolmod")
	require.NoError(t, err)
	assert.Len(t, pending, 0)

	all, err := s.ListInvitations(ctx, w.ID)
	require.NoError(t, err)
	assert.Len(t, all, 1)
	require.NoError(t, s.DeleteInvitation(ctx, inv.ID))
	assert.ErrorIs(t, s.DeleteInvitation(ctx, inv.ID), ErrNotFound)
}

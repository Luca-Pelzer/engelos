package handlers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/nicklaw5/helix/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/workspaces"
)

func newWorkspacesTestStore(t *testing.T) workspaces.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:wsoauthtest-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := workspaces.OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOAuth_OpenLogin_NonOwnerRefusedWhenClosed(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	// Reflects the real default: workspaces store is wired (owners still get
	// provisioning), but ENGELOS_OPEN_LOGIN=false so non-owners are refused.
	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(false)

	fake := newFakeHelix("42", "stranger", "s@example.com", "Stranger")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	// Closed mode refuses a non-owner with a friendly redirect (not a raw
	// 403) and creates no account or session.
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/login?denied=account", resp.Header.Get("Location"))
	assert.Empty(t, sessionCookie(resp), "no session must be minted for a refused non-owner")
	_, err := store.GetUserByEmail(context.Background(), oauthTestTenant, "s@example.com")
	require.Error(t, err, "refused non-owner must not get an account")
}

func TestOAuth_OpenLogin_NonOwnerLandsOnOnboard(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(true)

	fake := newFakeHelix("42", "stranger", "s@example.com", "Stranger")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/onboard", resp.Header.Get("Location"))
	assert.NotEmpty(t, sessionCookie(resp), "a session must be minted for an open-login user")
}

func TestOAuth_OpenLogin_OwnerProvisionsWorkspaceAndLandsOnIt(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(true)

	fake := newFakeHelix("7", "streamer", "o@example.com", "Streamer")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/channels/streamer", resp.Header.Get("Location"))

	got, err := ws.GetWorkspaceBySlug(context.Background(), oauthTestTenant, "streamer")
	require.NoError(t, err)
	assert.Equal(t, "streamer", got.TwitchLogin)
	members, err := ws.ListMembers(context.Background(), got.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, workspaces.RoleOwner, members[0].Role)
	assert.Equal(t, workspaces.SourceOwner, members[0].Source)
}

// TestOAuth_ClosedMode_OwnerProvisionsWorkspaceAndLandsOnIt is the regression
// guard for the C2 blocker: in closed mode (ENGELOS_OPEN_LOGIN=false, the
// default) an owner login MUST still be provisioned a workspace and routed to
// it via landingPath. The store is wired (as the daemon always wires it) but
// openLogin is false; before the fix the owner hit the !openLogin early
// redirect to /?login=success and skipped provisioning entirely.
func TestOAuth_ClosedMode_OwnerProvisionsWorkspaceAndLandsOnIt(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(false)

	fake := newFakeHelix("7", "streamer", "o@example.com", "Streamer")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	// Owner lands on their provisioned workspace, not the bare success page.
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/channels/streamer", resp.Header.Get("Location"))
	assert.NotEmpty(t, sessionCookie(resp), "owner must get a dashboard session even in closed mode")

	// Workspace provisioned with an owner membership.
	got, err := ws.GetWorkspaceBySlug(context.Background(), oauthTestTenant, "streamer")
	require.NoError(t, err, "owner workspace must be provisioned in closed mode")
	assert.Equal(t, "streamer", got.TwitchLogin)
	members, err := ws.ListMembers(context.Background(), got.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, workspaces.RoleOwner, members[0].Role)
	assert.Equal(t, workspaces.SourceOwner, members[0].Source)
}

func TestOAuth_OpenLogin_OwnerLoginIsIdempotent(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(true)

	fake := newFakeHelix("7", "streamer", "o@example.com", "Streamer")
	_ = runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok2"}, "user")

	assert.Equal(t, "/channels/streamer", resp.Header.Get("Location"))
	all, err := ws.ListWorkspaces(context.Background(), oauthTestTenant)
	require.NoError(t, err)
	assert.Len(t, all, 1, "a second owner login must not create a duplicate workspace")
}

func TestOAuth_OpenLogin_InvitedUserAutoJoins(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(true)

	ctx := context.Background()
	host, err := ws.CreateWorkspace(ctx, workspaces.Workspace{
		TenantID: oauthTestTenant, Slug: "streamer", TwitchLogin: "streamer",
		DisplayName: "Streamer", OwnerUserID: "owner-user",
	})
	require.NoError(t, err)
	_, err = ws.CreateInvitation(ctx, workspaces.Invitation{
		WorkspaceID: host.ID, TwitchLogin: "moddy", Role: workspaces.RoleMod, Token: "tok-invite",
	})
	require.NoError(t, err)

	fake := newFakeHelix("99", "moddy", "m@example.com", "Moddy")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/channels/streamer", resp.Header.Get("Location"))
	members, err := ws.ListMembers(ctx, host.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, workspaces.RoleMod, members[0].Role)
	assert.Equal(t, workspaces.SourceInvite, members[0].Source)
}

type fakeModGetter struct {
	mods map[string]bool
	err  error
}

func (f *fakeModGetter) GetModerators(p *helix.GetModeratorsParams) (*helix.ModeratorsResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	resp := &helix.ModeratorsResponse{}
	for _, uid := range p.UserIDs {
		if f.mods[uid] {
			resp.Data.Moderators = append(resp.Data.Moderators, helix.Moderator{UserID: uid})
		}
	}
	return resp, nil
}

func TestOAuth_OpenLogin_AutoVerifiesTwitchMod(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	host, err := ws.CreateWorkspace(context.Background(), workspaces.Workspace{
		TenantID: oauthTestTenant, Slug: "streamer", TwitchLogin: "streamer",
		TwitchChannelID: "100", DisplayName: "Streamer", OwnerUserID: "owner-1", AutoVerifyMods: true,
	})
	require.NoError(t, err)

	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(true).
		WithModVerify(func(context.Context, workspaces.Workspace) (*helix.Client, bool) { return nil, false })
	h.newModGetter = func(context.Context, workspaces.Workspace) (helixModGetter, bool) {
		return &fakeModGetter{mods: map[string]bool{"55": true}}, true
	}

	fake := newFakeHelix("55", "moddy", "m@example.com", "Moddy")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/channels/streamer", resp.Header.Get("Location"))
	members, err := ws.ListMembers(context.Background(), host.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, workspaces.RoleMod, members[0].Role)
	assert.Equal(t, workspaces.SourceTwitchVerified, members[0].Source)
}

func TestOAuth_OpenLogin_NonModNotVerified(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	_, err := ws.CreateWorkspace(context.Background(), workspaces.Workspace{
		TenantID: oauthTestTenant, Slug: "streamer", TwitchLogin: "streamer",
		TwitchChannelID: "100", DisplayName: "Streamer", OwnerUserID: "owner-1", AutoVerifyMods: true,
	})
	require.NoError(t, err)

	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(true)
	h.newModGetter = func(context.Context, workspaces.Workspace) (helixModGetter, bool) {
		return &fakeModGetter{mods: map[string]bool{}}, true
	}

	fake := newFakeHelix("77", "stranger", "s@example.com", "Stranger")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	assert.Equal(t, "/onboard", resp.Header.Get("Location"))
}

func TestOAuth_OpenLogin_AutoVerifySkipsWhenToggleOff(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	host, err := ws.CreateWorkspace(context.Background(), workspaces.Workspace{
		TenantID: oauthTestTenant, Slug: "streamer", TwitchLogin: "streamer",
		TwitchChannelID: "100", DisplayName: "Streamer", OwnerUserID: "owner-1", AutoVerifyMods: false,
	})
	require.NoError(t, err)

	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(true)
	called := false
	h.newModGetter = func(context.Context, workspaces.Workspace) (helixModGetter, bool) {
		called = true
		return &fakeModGetter{mods: map[string]bool{"55": true}}, true
	}

	fake := newFakeHelix("55", "moddy", "m@example.com", "Moddy")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	assert.Equal(t, "/onboard", resp.Header.Get("Location"))
	assert.False(t, called, "auto-verify must not query Helix when the workspace toggle is off")
	members, err := ws.ListMembers(context.Background(), host.ID)
	require.NoError(t, err)
	assert.Empty(t, members)
}

func sessionCookie(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == DefaultCookieName {
			return c.Value
		}
	}
	return ""
}

// TestOAuth_ClosedMode_NonOwnerRedirectsAndCreatesNothing is the dedicated
// regression guard for the denied-UX contract: in closed mode
// (ENGELOS_OPEN_LOGIN=false, the default) a non-owner purpose=user callback
// must 303-redirect to /login?denied=account and must create neither a user
// nor a session. The workspaces store is wired (as the daemon wires it) to
// prove the flag, not the store absence, now controls the door.
func TestOAuth_ClosedMode_NonOwnerRedirectsAndCreatesNothing(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	ws := newWorkspacesTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg()).
		WithOwnerLogins([]string{"streamer"}).
		WithWorkspaces(ws).
		WithOpenLogin(false)

	fake := newFakeHelix("42", "stranger", "s@example.com", "Stranger")
	resp := runCallbackWithPurpose(t, h, fake, &oauth2.Token{AccessToken: "tok"}, "user")

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, _ := resp.Location()
	require.NotNil(t, loc)
	assert.Equal(t, "/login?denied=account", loc.String())
	assert.Empty(t, sessionCookie(resp), "no session cookie for a refused non-owner")
	_, err := store.GetUserByEmail(context.Background(), oauthTestTenant, "s@example.com")
	require.Error(t, err, "no user must be created for a refused non-owner")
	// The Twitch identity row must not have been persisted either: a stranger
	// refused at the access gate never reaches CreateOAuthIdentity.
	_, err = store.GetOAuthIdentityByProviderUserID(context.Background(), auth.ProviderTwitch, "42")
	require.Error(t, err, "no oauth identity must be persisted for a refused non-owner")
}

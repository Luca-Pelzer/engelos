package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/auth"
)

func newDiscordHandler(t *testing.T, store auth.Store) *DiscordOAuth {
	t.Helper()
	core := newOAuthHandler(t, store, newOAuthCfg())
	cfg := &oauth2.Config{
		ClientID:     "disc-client",
		ClientSecret: "disc-secret",
		RedirectURL:  "https://bot.example.com/api/v1/auth/discord/callback",
		Scopes:       []string{"identify", "email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://discord.com/oauth2/authorize",
			TokenURL: "https://discord.com/api/oauth2/token",
		},
	}
	return NewDiscordOAuth(core, cfg)
}

// runDiscordCallback drives a happy-path callback with an injected token
// and identity, so tests never touch the real Discord API.
func runDiscordCallback(t *testing.T, h *DiscordOAuth, du discordUser) *http.Response {
	t.Helper()
	h.exchange = func(context.Context, string) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "atk", RefreshToken: "rtk", Expiry: time.Now().Add(time.Hour)}, nil
	}
	h.fetchUser = func(context.Context, string) (discordUser, error) { return du, nil }
	state := buildStateValue("randomstate", auth.OAuthPurposeUser)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/discord/callback?state="+state+"&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: state})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	return w.Result()
}

func TestDiscordOAuth_Login_RedirectsToDiscord(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newDiscordHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/discord/login?purpose=user", nil)
	w := httptest.NewRecorder()
	h.Login(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, _ := resp.Location()
	require.NotNil(t, loc)
	assert.Equal(t, "discord.com", loc.Host)
	var hasState bool
	for _, c := range resp.Cookies() {
		if c.Name == OAuthStateCookieName {
			hasState = true
		}
	}
	assert.True(t, hasState, "state cookie must be set")
}

func TestDiscordOAuth_Callback_BootstrapOwnerHappyPath(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newDiscordHandler(t, store)
	resp := runDiscordCallback(t, h, discordUser{ID: "disc-1", Username: "OwnerName", Email: "owner@d.com"})
	defer resp.Body.Close()

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	var sess *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == DefaultCookieName {
			sess = c
		}
	}
	require.NotNil(t, sess, "owner must get a session")
	u, err := store.GetUserByEmail(context.Background(), oauthTestTenant, "owner@d.com")
	require.NoError(t, err)
	assert.Equal(t, auth.RoleOwner, u.Role)
	assert.Equal(t, "ownername", u.Username)
}

func TestDiscordOAuth_Callback_RejectsStranger(t *testing.T) {
	store := newOAuthTestStore(t)
	// Owner already exists -> bootstrap closed, no allowlist for stranger.
	seedExistingUser(t, store, "owner@example.com", "owner", auth.RoleOwner)
	h := newDiscordHandler(t, store)
	resp := runDiscordCallback(t, h, discordUser{ID: "disc-x", Username: "stranger", Email: "x@d.com"})
	defer resp.Body.Close()

	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	for _, c := range resp.Cookies() {
		require.NotEqual(t, DefaultCookieName, c.Name)
	}
	_, err := store.GetUserByEmail(context.Background(), oauthTestTenant, "x@d.com")
	require.Error(t, err, "stranger must not get an account")
}

func TestDiscordOAuth_Callback_AllowlistAdmits(t *testing.T) {
	store := newOAuthTestStore(t)
	seedExistingUser(t, store, "owner@example.com", "owner", auth.RoleOwner)
	core := newOAuthHandler(t, store, newOAuthCfg()).WithOwnerDiscordLogins([]string{"secondowner"})
	cfg := &oauth2.Config{ClientID: "c", ClientSecret: "s", RedirectURL: "https://x/cb",
		Endpoint: oauth2.Endpoint{AuthURL: "https://discord.com/oauth2/authorize", TokenURL: "https://discord.com/api/oauth2/token"}}
	h := NewDiscordOAuth(core, cfg)

	resp := runDiscordCallback(t, h, discordUser{ID: "disc-2", Username: "SecondOwner", Email: "2@d.com"})
	defer resp.Body.Close()

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	var sess *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == DefaultCookieName {
			sess = c
		}
	}
	require.NotNil(t, sess, "allowlisted discord login must get a session")
}

func TestDiscordOAuth_Callback_InvalidState(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newDiscordHandler(t, store)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/discord/callback?state=aaa&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "bbb"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDiscordOAuth_Disabled_NilConfig(t *testing.T) {
	store := newOAuthTestStore(t)
	core := newOAuthHandler(t, store, newOAuthCfg())
	h := NewDiscordOAuth(core, nil)
	w := httptest.NewRecorder()
	h.Login(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/discord/login", nil))
	require.Equal(t, http.StatusNotImplemented, w.Result().StatusCode)
}

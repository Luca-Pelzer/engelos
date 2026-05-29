package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nicklaw5/helix/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/secrets"
)

const (
	oauthTestTenant   = "local"
	oauthTestClientID = "twitch-client-id"
)

func newOAuthTestStore(t *testing.T) auth.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:oauthhandlertest-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	key := bytes.Repeat([]byte{0x42}, 32)
	box, err := secrets.NewBox(key)
	require.NoError(t, err)
	s, err := auth.OpenSQLiteStore(context.Background(), dsn, logger, auth.WithCrypto(box))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newOAuthCfg() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     oauthTestClientID,
		ClientSecret: "twitch-client-secret",
		RedirectURL:  "http://localhost:8080/api/v1/auth/twitch/callback",
		Scopes:       []string{"user:read:email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://id.twitch.tv/oauth2/authorize",
			TokenURL: "https://id.twitch.tv/oauth2/token",
		},
	}
}

func newOAuthHandler(t *testing.T, store auth.Store, cfg *oauth2.Config) *OAuth {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewOAuth(store, oauthTestTenant, logger, cfg).WithCookieSecure(false)
}

type fakeHelix struct {
	resp *helix.UsersResponse
	err  error
}

func (f *fakeHelix) GetUsers(*helix.UsersParams) (*helix.UsersResponse, error) {
	return f.resp, f.err
}

func newFakeHelix(id, login, email, display string) *fakeHelix {
	return &fakeHelix{
		resp: &helix.UsersResponse{
			Data: helix.ManyUsers{Users: []helix.User{{
				ID:          id,
				Login:       login,
				DisplayName: display,
				Email:       email,
			}}},
		},
	}
}

// TestOAuth_Disabled_NilStore covers the bootstrap-time degraded state
// where the OAuth feature is wired but no Store is available: both
// handlers must return 501 so the router can still be built.
func TestOAuth_Disabled_NilStore(t *testing.T) {
	t.Parallel()
	h := NewOAuth(nil, oauthTestTenant, nil, newOAuthCfg())

	w := httptest.NewRecorder()
	h.Login(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/login", nil))
	assert.Equal(t, http.StatusNotImplemented, w.Result().StatusCode)

	w = httptest.NewRecorder()
	h.Callback(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?state=x&code=y", nil))
	assert.Equal(t, http.StatusNotImplemented, w.Result().StatusCode)
}

func TestOAuth_Disabled_NilConfig(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := NewOAuth(store, oauthTestTenant, nil, nil)

	w := httptest.NewRecorder()
	h.Login(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/login", nil))
	assert.Equal(t, http.StatusNotImplemented, w.Result().StatusCode)

	w = httptest.NewRecorder()
	h.Callback(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?state=x&code=y", nil))
	assert.Equal(t, http.StatusNotImplemented, w.Result().StatusCode)
}

func TestOAuth_Login_RedirectsAndSetsStateCookie(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())

	w := httptest.NewRecorder()
	h.Login(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/login", nil))
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)

	var stateCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == OAuthStateCookieName {
			stateCookie = c
			break
		}
	}
	require.NotNil(t, stateCookie, "expected state cookie")
	assert.True(t, stateCookie.HttpOnly)
	assert.False(t, stateCookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, stateCookie.SameSite,
		"SameSite must be Lax — Strict would drop the cookie on the cross-site return from Twitch")
	assert.Greater(t, stateCookie.MaxAge, 0)
	assert.NotEmpty(t, stateCookie.Value)

	loc, err := resp.Location()
	require.NoError(t, err)
	assert.Equal(t, "id.twitch.tv", loc.Host)
	assert.Equal(t, "/oauth2/authorize", loc.Path)
	q := loc.Query()
	assert.Equal(t, oauthTestClientID, q.Get("client_id"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, stateCookie.Value, q.Get("state"))
}

func TestOAuth_Callback_MissingStateCookie(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?state=abc&code=xyz", nil)
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "invalid_state", body["error"])
}

func TestOAuth_Callback_MismatchedState_ClearsCookie(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?state=client&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "server"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "invalid_state", body["error"])

	cleared := false
	for _, c := range resp.Cookies() {
		if c.Name == OAuthStateCookieName {
			cleared = c.MaxAge < 0
		}
	}
	assert.True(t, cleared, "state cookie must be cleared on mismatch to prevent replay")
}

func TestOAuth_Callback_MissingCode(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?state=tok", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "tok"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "missing_code", body["error"])
}

func TestOAuth_Callback_ProviderError_RedirectsToErrorMarker(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/twitch/callback?error=access_denied", nil)
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, _ := resp.Location()
	require.NotNil(t, loc)
	assert.Equal(t, "/?login=error", loc.String())
}

func TestOAuth_Callback_ExchangeFailure(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())
	h.exchange = func(context.Context, string) (*oauth2.Token, error) {
		return nil, errors.New("twitch returned 400")
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/twitch/callback?state=tok&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "tok"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "oauth_exchange_failed", body["error"])
}

func TestOAuth_Callback_IdentityFetchFailure(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())
	h.exchange = func(context.Context, string) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "atk"}, nil
	}
	h.newHelix = func(string, string) (helixUserGetter, error) {
		return &fakeHelix{err: errors.New("helix down")}, nil
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/twitch/callback?state=tok&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "tok"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "identity_fetch_failed", body["error"])
}

func runCallbackHappyPath(t *testing.T, h *OAuth, fake *fakeHelix, tok *oauth2.Token) *http.Response {
	t.Helper()
	h.exchange = func(context.Context, string) (*oauth2.Token, error) {
		return tok, nil
	}
	h.newHelix = func(clientID, userAccessToken string) (helixUserGetter, error) {
		assert.Equal(t, oauthTestClientID, clientID, "helix client must be built with our client id")
		assert.Equal(t, tok.AccessToken, userAccessToken, "helix client must use the access token")
		return fake, nil
	}
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/twitch/callback?state=tok&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "tok"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	return w.Result()
}

func TestOAuth_Callback_NewUser_HappyPath(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())
	tok := (&oauth2.Token{
		AccessToken:  "ATK-plaintext-DO-NOT-LEAK",
		RefreshToken: "RTK-plaintext-DO-NOT-LEAK",
		Expiry:       time.Now().Add(time.Hour),
	}).WithExtra(map[string]any{"scope": "user:read:email chat:read"})
	fake := newFakeHelix("tw-42", "ninjalogin", "ninja@example.com", "Ninja")

	resp := runCallbackHappyPath(t, h, fake, tok)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, _ := resp.Location()
	require.NotNil(t, loc)
	assert.Equal(t, "/?login=success", loc.String())

	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == DefaultCookieName {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie, "session cookie must be set")
	assert.True(t, sessionCookie.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, sessionCookie.SameSite)
	assert.Greater(t, sessionCookie.MaxAge, 0)

	ctx := context.Background()
	sess, err := store.GetSessionByTokenHash(ctx, auth.HashTokenString(sessionCookie.Value))
	require.NoError(t, err)
	user, err := store.GetUserByID(ctx, oauthTestTenant, sess.UserID)
	require.NoError(t, err)
	assert.Equal(t, "ninja@example.com", user.Email)
	assert.Equal(t, "ninjalogin", user.Username)
	assert.Equal(t, auth.RoleViewer, user.Role)
	assert.False(t, user.LastLoginAt.IsZero())

	identity, err := store.GetOAuthIdentityByProviderUserID(ctx, auth.ProviderTwitch, "tw-42")
	require.NoError(t, err)
	assert.Equal(t, user.ID, identity.UserID)
	assert.Equal(t, "ATK-plaintext-DO-NOT-LEAK", identity.AccessToken, "tokens decrypt back to plaintext")
	assert.Equal(t, "RTK-plaintext-DO-NOT-LEAK", identity.RefreshToken)
	assert.Equal(t, []string{"user:read:email", "chat:read"}, identity.Scopes)
	assert.WithinDuration(t, tok.Expiry, identity.ExpiresAt, time.Second)
}

func TestOAuth_Callback_NewUser_SyntheticEmail(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())
	tok := &oauth2.Token{AccessToken: "atk", RefreshToken: "rtk", Expiry: time.Now().Add(time.Hour)}
	fake := newFakeHelix("tw-noemail", "shyuser", "", "Shy")

	resp := runCallbackHappyPath(t, h, fake, tok)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)

	ctx := context.Background()
	identity, err := store.GetOAuthIdentityByProviderUserID(ctx, auth.ProviderTwitch, "tw-noemail")
	require.NoError(t, err)
	user, err := store.GetUserByID(ctx, oauthTestTenant, identity.UserID)
	require.NoError(t, err)
	assert.Equal(t, "shyuser@twitch.local", user.Email,
		"missing provider email must be replaced by a stable synthetic placeholder")
}

func TestOAuth_Callback_ReturningUser_NoDuplicate(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)
	h := newOAuthHandler(t, store, newOAuthCfg())
	ctx := context.Background()

	preHash, err := auth.HashPassword("placeholder")
	require.NoError(t, err)
	pre, err := store.CreateUser(ctx, auth.User{
		TenantID:     oauthTestTenant,
		Email:        "existing@example.com",
		Username:     "existing",
		PasswordHash: []byte(preHash),
		Role:         auth.RoleAdmin,
	})
	require.NoError(t, err)
	_, err = store.CreateOAuthIdentity(ctx, auth.OAuthIdentity{
		TenantID:       oauthTestTenant,
		UserID:         pre.ID,
		Provider:       auth.ProviderTwitch,
		ProviderUserID: "tw-existing",
		ProviderLogin:  "existing",
		Purpose:        auth.OAuthPurposeUser,
		AccessToken:    "old-atk",
	})
	require.NoError(t, err)

	users, err := store.ListUsers(ctx, oauthTestTenant)
	require.NoError(t, err)
	before := len(users)

	tok := &oauth2.Token{AccessToken: "new-atk", RefreshToken: "new-rtk", Expiry: time.Now().Add(time.Hour)}
	fake := newFakeHelix("tw-existing", "existing", "existing@example.com", "Existing")
	resp := runCallbackHappyPath(t, h, fake, tok)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)

	users, err = store.ListUsers(ctx, oauthTestTenant)
	require.NoError(t, err)
	assert.Equal(t, before, len(users), "returning OAuth login must not create a duplicate user row")

	identity, err := store.GetOAuthIdentityByProviderUserID(ctx, auth.ProviderTwitch, "tw-existing")
	require.NoError(t, err)
	assert.Equal(t, pre.ID, identity.UserID)
	assert.Equal(t, "new-atk", identity.AccessToken, "tokens must be refreshed on every login")
	assert.Equal(t, "new-rtk", identity.RefreshToken)

	var sessCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == DefaultCookieName {
			sessCookie = c
		}
	}
	require.NotNil(t, sessCookie, "returning user must still receive a session cookie")
}

func TestOAuth_Callback_ExchangeViaTokenURL(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "xyz", r.PostForm.Get("code"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token": "real-atk",
			"refresh_token": "real-rtk",
			"token_type": "bearer",
			"expires_in": 3600,
			"scope": "user:read:email"
		}`))
	}))
	t.Cleanup(tokenServer.Close)

	cfg := newOAuthCfg()
	cfg.Endpoint.TokenURL = tokenServer.URL
	h := newOAuthHandler(t, store, cfg)
	h.newHelix = func(string, string) (helixUserGetter, error) {
		return newFakeHelix("tw-real", "realuser", "real@example.com", "Real"), nil
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/twitch/callback?state=tok&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "tok"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	ctx := context.Background()
	identity, err := store.GetOAuthIdentityByProviderUserID(ctx, auth.ProviderTwitch, "tw-real")
	require.NoError(t, err)
	assert.Equal(t, "real-atk", identity.AccessToken,
		"token returned by the (httptest) token server must be the one persisted")
	assert.Equal(t, "real-rtk", identity.RefreshToken)
	assert.Equal(t, []string{"user:read:email"}, identity.Scopes)
}

// TestOAuth_GenerateStateIsUnique guards the entropy of the state value:
// repeated calls must never collide. Even a tiny collision rate breaks
// the CSRF guarantee.
func TestOAuth_GenerateStateIsUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, 256)
	for range 256 {
		s, err := generateState()
		require.NoError(t, err)
		_, dup := seen[s]
		assert.False(t, dup, "state collision")
		seen[s] = struct{}{}
	}
}

func TestOAuth_NewOAuth_HandlesNilLogger(t *testing.T) {
	t.Parallel()
	h := NewOAuth(nil, "local", nil, nil)
	assert.NotNil(t, h.logger)
}

func TestOAuth_WithCookieSecure_And_SessionTTL(t *testing.T) {
	t.Parallel()
	h := NewOAuth(nil, "local", nil, nil).
		WithCookieSecure(true).
		WithSessionTTL(2 * time.Hour)
	assert.True(t, h.cookieSecure)
	assert.Equal(t, 2*time.Hour, h.sessionTTL)

	h.WithSessionTTL(0)
	assert.Equal(t, 2*time.Hour, h.sessionTTL, "non-positive TTL must be ignored")
}

func TestOAuth_ParseGrantedScopes(t *testing.T) {
	t.Parallel()
	assert.Nil(t, parseGrantedScopes(nil))
	assert.Nil(t, parseGrantedScopes(&oauth2.Token{}))

	tok := (&oauth2.Token{AccessToken: "x"}).WithExtra(map[string]any{
		"scope": "a b c",
	})
	assert.Equal(t, []string{"a", "b", "c"}, parseGrantedScopes(tok))
}

// fakeStoreCreateFails wraps a real store and forces CreateOAuthIdentity
// to return a non-not-found error so we can exercise the late-failure
// branch of Callback (everything else succeeded; persistence broke).
type fakeStoreCreateFails struct {
	auth.Store
}

func (f *fakeStoreCreateFails) CreateOAuthIdentity(ctx context.Context, o auth.OAuthIdentity) (auth.OAuthIdentity, error) {
	return auth.OAuthIdentity{}, errors.New("disk full")
}

func TestOAuth_Callback_PersistIdentityFails(t *testing.T) {
	t.Parallel()
	base := newOAuthTestStore(t)
	store := &fakeStoreCreateFails{Store: base}
	h := newOAuthHandler(t, store, newOAuthCfg())
	h.exchange = func(context.Context, string) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "atk", Expiry: time.Now().Add(time.Hour)}, nil
	}
	h.newHelix = func(string, string) (helixUserGetter, error) {
		return newFakeHelix("tw-x", "x", "x@example.com", "X"), nil
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/twitch/callback?state=tok&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "tok"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "internal_error")
	assert.NotContains(t, string(body), "atk", "tokens must never leak into error responses")
}

func TestOAuth_Callback_DoesNotLogTokens(t *testing.T) {
	t.Parallel()
	store := newOAuthTestStore(t)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := NewOAuth(store, oauthTestTenant, logger, newOAuthCfg()).WithCookieSecure(false)
	h.exchange = func(context.Context, string) (*oauth2.Token, error) {
		return &oauth2.Token{
			AccessToken:  "TOKMARKER-SUPER-SECRET-ATK",
			RefreshToken: "TOKMARKER-SUPER-SECRET-RTK",
			Expiry:       time.Now().Add(time.Hour),
		}, nil
	}
	h.newHelix = func(string, string) (helixUserGetter, error) {
		return newFakeHelix("tw-q", "q", "q@example.com", "Q"), nil
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/twitch/callback?state=tok&code=xyz", nil)
	req.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "tok"})
	w := httptest.NewRecorder()
	h.Callback(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	logs := logBuf.String()
	assert.NotContains(t, logs, "TOKMARKER-SUPER-SECRET-ATK", "access token must never appear in logs")
	assert.NotContains(t, logs, "TOKMARKER-SUPER-SECRET-RTK", "refresh token must never appear in logs")
}

// Sanity: the package-level constant matches what we tell callers.
func TestOAuth_StateCookieNameStableForCallers(t *testing.T) {
	t.Parallel()
	assert.True(t, strings.HasPrefix(OAuthStateCookieName, "engelos_"),
		"state cookie name should be namespaced to engelos so it cannot collide with unrelated cookies")
}

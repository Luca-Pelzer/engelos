package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

func newKickCfg() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "kick-client-id",
		ClientSecret: "kick-client-secret",
		RedirectURL:  "http://localhost:8080/api/v1/auth/kick/callback",
		Scopes:       []string{"chat:write", "events:subscribe"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://id.kick.com/oauth/authorize",
			TokenURL: "https://id.kick.com/oauth/token",
		},
	}
}

func newKickHandler(t *testing.T, store auth.Store, cfg *oauth2.Config) *KickOAuth {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewKickOAuth(store, oauthTestTenant, logger, cfg).WithCookieSecure(false)
}

func cookieByName(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestKickOAuth_LoginSetsStateAndVerifier(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newKickHandler(t, store, newKickCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/kick/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "id.kick.com/oauth/authorize")
	assert.Contains(t, loc, "client_id=kick-client-id")
	assert.Contains(t, loc, "code_challenge=")
	assert.Contains(t, loc, "code_challenge_method=S256")

	cookies := rec.Result().Cookies()
	state := cookieByName(cookies, KickStateCookieName)
	verifier := cookieByName(cookies, KickVerifierCookieName)
	require.NotNil(t, state)
	require.NotNil(t, verifier)
	assert.NotEmpty(t, state.Value)
	assert.NotEmpty(t, verifier.Value)
}

func TestKickOAuth_LoginDisabled(t *testing.T) {
	h := newKickHandler(t, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/kick/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestKickOAuth_CallbackHappyPath(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)

	profile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer access-tok", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"data":[{"user_id":4242,"name":"engelguard"}]}`)
	}))
	defer profile.Close()

	h := newKickHandler(t, store, newKickCfg())
	h.profileURL = profile.URL
	h.exchange = func(_ context.Context, code, verifier string) (*oauth2.Token, error) {
		assert.Equal(t, "auth-code", code)
		assert.Equal(t, "the-verifier", verifier)
		return &oauth2.Token{AccessToken: "access-tok", RefreshToken: "refresh-tok"}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/kick/callback?state=xyz&code=auth-code", nil)
	req.AddCookie(&http.Cookie{Name: KickStateCookieName, Value: "xyz"})
	req.AddCookie(&http.Cookie{Name: KickVerifierCookieName, Value: "the-verifier"})
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/?kick=connected", rec.Header().Get("Location"))

	id, err := store.GetBotIdentity(context.Background(), oauthTestTenant, auth.ProviderKick)
	require.NoError(t, err)
	assert.Equal(t, "4242", id.ProviderUserID)
	assert.Equal(t, "engelguard", id.ProviderLogin)
	assert.Equal(t, "access-tok", id.AccessToken)
	assert.Equal(t, "refresh-tok", id.RefreshToken)
	assert.Equal(t, auth.OAuthPurposeBot, id.Purpose)
}

func TestKickOAuth_CallbackMissingVerifier(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)
	h := newKickHandler(t, store, newKickCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/kick/callback?state=xyz&code=auth-code", nil)
	req.AddCookie(&http.Cookie{Name: KickStateCookieName, Value: "xyz"})
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestKickOAuth_CallbackBadState(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)
	h := newKickHandler(t, store, newKickCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/kick/callback?state=xyz&code=c", nil)
	req.AddCookie(&http.Cookie{Name: KickStateCookieName, Value: "DIFFERENT"})
	req.AddCookie(&http.Cookie{Name: KickVerifierCookieName, Value: "v"})
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestKickOAuth_CallbackUnauthenticated(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newKickHandler(t, store, newKickCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/kick/callback?state=xyz&code=c", nil)
	req.AddCookie(&http.Cookie{Name: KickStateCookieName, Value: "xyz"})
	req.AddCookie(&http.Cookie{Name: KickVerifierCookieName, Value: "v"})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestKickOAuth_CallbackProviderError(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)
	h := newKickHandler(t, store, newKickCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/kick/callback?error=access_denied", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/?kick=error", rec.Header().Get("Location"))
}

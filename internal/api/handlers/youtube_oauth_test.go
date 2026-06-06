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

func newYouTubeCfg() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "youtube-client-id",
		ClientSecret: "youtube-client-secret",
		RedirectURL:  "http://localhost:8080/api/v1/auth/youtube/callback",
		Scopes:       []string{"https://www.googleapis.com/auth/youtube.force-ssl"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
	}
}

func newYouTubeHandler(t *testing.T, store auth.Store, cfg *oauth2.Config) *YouTubeOAuth {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewYouTubeOAuth(store, oauthTestTenant, logger, cfg).WithCookieSecure(false)
}

func TestYouTubeOAuth_LoginRedirects(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newYouTubeHandler(t, store, newYouTubeCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/youtube/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "accounts.google.com/o/oauth2/auth")
	assert.Contains(t, loc, "client_id=youtube-client-id")
	assert.Contains(t, loc, "access_type=offline")
	assert.Contains(t, loc, "prompt=consent")

	var stateCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == YouTubeStateCookieName {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie)
	assert.NotEmpty(t, stateCookie.Value)
}

func TestYouTubeOAuth_LoginDisabled(t *testing.T) {
	h := newYouTubeHandler(t, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/youtube/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestYouTubeOAuth_CallbackHappyPath(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)

	profile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer access-tok", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"id":"googleuser123","name":"Stream Bot","email":"bot@example.com"}`)
	}))
	defer profile.Close()

	h := newYouTubeHandler(t, store, newYouTubeCfg())
	h.profileURL = profile.URL
	h.exchange = func(_ context.Context, code string) (*oauth2.Token, error) {
		assert.Equal(t, "auth-code", code)
		return &oauth2.Token{AccessToken: "access-tok", RefreshToken: "refresh-tok"}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/youtube/callback?state=xyz&code=auth-code", nil)
	req.AddCookie(&http.Cookie{Name: YouTubeStateCookieName, Value: "xyz"})
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/?youtube=connected", rec.Header().Get("Location"))

	id, err := store.GetBotIdentity(context.Background(), oauthTestTenant, auth.ProviderYouTube)
	require.NoError(t, err)
	assert.Equal(t, "googleuser123", id.ProviderUserID)
	assert.Equal(t, "Stream Bot", id.ProviderLogin)
	assert.Equal(t, "access-tok", id.AccessToken)
	assert.Equal(t, "refresh-tok", id.RefreshToken)
	assert.Equal(t, auth.OAuthPurposeBot, id.Purpose)
}

func TestYouTubeOAuth_CallbackBadState(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)
	h := newYouTubeHandler(t, store, newYouTubeCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/youtube/callback?state=xyz&code=c", nil)
	req.AddCookie(&http.Cookie{Name: YouTubeStateCookieName, Value: "DIFFERENT"})
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestYouTubeOAuth_CallbackUnauthenticated(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newYouTubeHandler(t, store, newYouTubeCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/youtube/callback?state=xyz&code=c", nil)
	req.AddCookie(&http.Cookie{Name: YouTubeStateCookieName, Value: "xyz"})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestYouTubeOAuth_CallbackProviderError(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)
	h := newYouTubeHandler(t, store, newYouTubeCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/youtube/callback?error=access_denied", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/?youtube=error", rec.Header().Get("Location"))
}

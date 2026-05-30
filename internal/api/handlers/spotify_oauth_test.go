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

func newSpotifyCfg() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "spotify-client-id",
		ClientSecret: "spotify-client-secret",
		RedirectURL:  "http://localhost:8080/api/v1/auth/spotify/callback",
		Scopes:       []string{"user-modify-playback-state"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.spotify.com/authorize",
			TokenURL: "https://accounts.spotify.com/api/token",
		},
	}
}

func newSpotifyHandler(t *testing.T, store auth.Store, cfg *oauth2.Config) *SpotifyOAuth {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewSpotifyOAuth(store, oauthTestTenant, logger, cfg).WithCookieSecure(false)
}

// seedUser creates a dashboard user to link the Spotify identity to.
func seedUser(t *testing.T, store auth.Store) auth.User {
	t.Helper()
	u := auth.User{
		ID:           auth.NewUserID(),
		TenantID:     oauthTestTenant,
		Email:        "streamer@example.com",
		Username:     "streamer",
		PasswordHash: []byte("0123456789abcdef0123456789abcdef"),
		Role:         auth.RoleAdmin,
	}
	created, err := store.CreateUser(context.Background(), u)
	require.NoError(t, err)
	return created
}

func TestSpotifyOAuth_LoginRedirects(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newSpotifyHandler(t, store, newSpotifyCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/spotify/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	loc := rec.Header().Get("Location")
	assert.Contains(t, loc, "accounts.spotify.com/authorize")
	assert.Contains(t, loc, "client_id=spotify-client-id")

	var stateCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == SpotifyStateCookieName {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie)
	assert.NotEmpty(t, stateCookie.Value)
}

func TestSpotifyOAuth_LoginDisabled(t *testing.T) {
	h := newSpotifyHandler(t, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/spotify/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestSpotifyOAuth_CallbackHappyPath(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)

	profile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer access-tok", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"id":"spotifyuser123","display_name":"DJ Streamer"}`)
	}))
	defer profile.Close()

	h := newSpotifyHandler(t, store, newSpotifyCfg())
	h.profileURL = profile.URL
	h.exchange = func(_ context.Context, code string) (*oauth2.Token, error) {
		assert.Equal(t, "auth-code", code)
		return &oauth2.Token{AccessToken: "access-tok", RefreshToken: "refresh-tok"}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/spotify/callback?state=xyz&code=auth-code", nil)
	req.AddCookie(&http.Cookie{Name: SpotifyStateCookieName, Value: "xyz"})
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/?spotify=connected", rec.Header().Get("Location"))

	id, err := store.GetBotIdentity(context.Background(), oauthTestTenant, auth.ProviderSpotify)
	require.NoError(t, err)
	assert.Equal(t, "spotifyuser123", id.ProviderUserID)
	assert.Equal(t, "DJ Streamer", id.ProviderLogin)
	assert.Equal(t, "access-tok", id.AccessToken)
	assert.Equal(t, "refresh-tok", id.RefreshToken)
	assert.Equal(t, auth.OAuthPurposeBot, id.Purpose)
}

func TestSpotifyOAuth_CallbackBadState(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)
	h := newSpotifyHandler(t, store, newSpotifyCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/spotify/callback?state=xyz&code=c", nil)
	req.AddCookie(&http.Cookie{Name: SpotifyStateCookieName, Value: "DIFFERENT"})
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSpotifyOAuth_CallbackUnauthenticated(t *testing.T) {
	store := newOAuthTestStore(t)
	h := newSpotifyHandler(t, store, newSpotifyCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/spotify/callback?state=xyz&code=c", nil)
	req.AddCookie(&http.Cookie{Name: SpotifyStateCookieName, Value: "xyz"})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSpotifyOAuth_CallbackProviderError(t *testing.T) {
	store := newOAuthTestStore(t)
	user := seedUser(t, store)
	h := newSpotifyHandler(t, store, newSpotifyCfg())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/spotify/callback?error=access_denied", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/?spotify=error", rec.Header().Get("Location"))
}

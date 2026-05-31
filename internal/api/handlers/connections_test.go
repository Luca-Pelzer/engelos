package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/secrets"
)

func newCryptoStore(t *testing.T) auth.Store {
	t.Helper()
	dsn := "file:connteststore-" + time.Now().Format("150405.000000000") + "?mode=memory&cache=shared"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	box, err := secrets.NewBox(bytes.Repeat([]byte{0x24}, 32))
	require.NoError(t, err)
	s, err := auth.OpenSQLiteStore(context.Background(), dsn, logger, auth.WithCrypto(box))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// loginAs mints a real session for user and returns the request cookie, so the
// test exercises the full SessionAuth middleware path rather than faking the
// context value (whose key is unexported).
func loginAs(t *testing.T, store auth.Store, user auth.User) *http.Cookie {
	t.Helper()
	token, sess, err := auth.NewSession(user.TenantID, user.ID, "test-agent", "127.0.0.1", time.Hour)
	require.NoError(t, err)
	require.NoError(t, store.CreateSession(context.Background(), sess))
	return &http.Cookie{Name: apimw.SessionCookieName, Value: token}
}

func serveConnections(t *testing.T, store auth.Store, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.NewConnections(store, testTenant, logger)
	handler := apimw.SessionAuth(store, apimw.SessionCookieName, logger)(
		apimw.RequireSession(http.HandlerFunc(h.List)),
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestConnections_RequiresSession(t *testing.T) {
	store := newCryptoStore(t)
	rr := serveConnections(t, store, nil)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestConnections_ListsTwitchIdentityWithClipScope(t *testing.T) {
	store := newCryptoStore(t)
	user := seedUser(t, store, "streamer@example.com", testPassword, false)
	_, err := store.CreateOAuthIdentity(context.Background(), auth.OAuthIdentity{
		TenantID:       testTenant,
		UserID:         user.ID,
		Provider:       auth.ProviderTwitch,
		ProviderUserID: "12345",
		ProviderLogin:  "engelswtf",
		Purpose:        auth.OAuthPurposeBot,
		AccessToken:    "access",
		RefreshToken:   "refresh",
		Scopes:         []string{"chat:read", "chat:edit", "clips:edit"},
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	})
	require.NoError(t, err)

	rr := serveConnections(t, store, loginAs(t, store, user))
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		Connections []struct {
			Provider      string   `json:"provider"`
			ProviderLogin string   `json:"provider_login"`
			Purpose       string   `json:"purpose"`
			Scopes        []string `json:"scopes"`
			CanCreateClip bool     `json:"can_create_clip"`
			Expired       bool     `json:"expired"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Connections, 1)
	c := body.Connections[0]
	require.Equal(t, auth.ProviderTwitch, c.Provider)
	require.Equal(t, "engelswtf", c.ProviderLogin)
	require.Equal(t, auth.OAuthPurposeBot, c.Purpose)
	require.True(t, c.CanCreateClip)
	require.False(t, c.Expired)
}

func TestConnections_ClipScopeMissingAndExpired(t *testing.T) {
	store := newCryptoStore(t)
	user := seedUser(t, store, "noclip@example.com", testPassword, false)
	_, err := store.CreateOAuthIdentity(context.Background(), auth.OAuthIdentity{
		TenantID:       testTenant,
		UserID:         user.ID,
		Provider:       auth.ProviderTwitch,
		ProviderUserID: "999",
		ProviderLogin:  "smallstreamer",
		Purpose:        auth.OAuthPurposeUser,
		AccessToken:    "access",
		RefreshToken:   "refresh",
		Scopes:         []string{"chat:read", "chat:edit"},
		ExpiresAt:      time.Now().Add(-time.Hour).UTC(),
	})
	require.NoError(t, err)

	rr := serveConnections(t, store, loginAs(t, store, user))
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		Connections []struct {
			CanCreateClip bool `json:"can_create_clip"`
			Expired       bool `json:"expired"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Connections, 1)
	require.False(t, body.Connections[0].CanCreateClip)
	require.True(t, body.Connections[0].Expired)
}

func TestConnections_EmptyWhenNoIdentities(t *testing.T) {
	store := newCryptoStore(t)
	user := seedUser(t, store, "fresh@example.com", testPassword, false)
	rr := serveConnections(t, store, loginAs(t, store, user))
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		Connections []json.RawMessage `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Empty(t, body.Connections)
}

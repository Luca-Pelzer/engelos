package handlers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedIdentity links a Twitch OAuth identity to user and returns its id.
func seedIdentity(t *testing.T, store auth.Store, user auth.User, providerUserID, login string) string {
	t.Helper()
	id, err := store.CreateOAuthIdentity(context.Background(), auth.OAuthIdentity{
		TenantID:       user.TenantID,
		UserID:         user.ID,
		Provider:       auth.ProviderTwitch,
		ProviderUserID: providerUserID,
		ProviderLogin:  login,
		Purpose:        auth.OAuthPurposeUser,
		AccessToken:    "atk",
		RefreshToken:   "rtk",
		Scopes:         []string{"user:read:email"},
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	})
	require.NoError(t, err)
	return id.ID
}

// serveDelete routes DELETE /api/v1/connections/{id} through a chi router so
// chi.URLParam resolves, behind the real session middleware.
func serveDelete(t *testing.T, store auth.Store, cookie *http.Cookie, id string) *httptest.ResponseRecorder {
	t.Helper()
	h := handlers.NewConnections(store, testTenant, discardLogger())
	r := chi.NewRouter()
	r.Use(apimw.SessionAuth(store, apimw.SessionCookieName, discardLogger()))
	r.With(apimw.RequireSession).Delete("/api/v1/connections/{id}", h.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/connections/"+id, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestConnections_Delete_OwnIdentity(t *testing.T) {
	store := newCryptoStore(t)
	user := seedUser(t, store, "owner@example.com", testPassword, false)
	id := seedIdentity(t, store, user, "tw-1", "engelswtf")

	rr := serveDelete(t, store, loginAs(t, store, user), id)
	require.Equal(t, http.StatusOK, rr.Code)

	left, err := store.GetOAuthIdentitiesByUser(context.Background(), testTenant, user.ID)
	require.NoError(t, err)
	require.Empty(t, left, "identity must be gone after unlink")
}

func TestConnections_Delete_OtherUsersIdentityIs404(t *testing.T) {
	store := newCryptoStore(t)
	owner := seedUser(t, store, "owner@example.com", testPassword, false)
	other := seedUser(t, store, "other@example.com", testPassword, false)
	victimID := seedIdentity(t, store, other, "tw-2", "victim")

	rr := serveDelete(t, store, loginAs(t, store, owner), victimID)
	require.Equal(t, http.StatusNotFound, rr.Code, "must not delete another user's identity")

	left, err := store.GetOAuthIdentitiesByUser(context.Background(), testTenant, other.ID)
	require.NoError(t, err)
	require.Len(t, left, 1, "other user's identity must be untouched")
}

func TestConnections_Delete_MissingIs404(t *testing.T) {
	store := newCryptoStore(t)
	user := seedUser(t, store, "owner@example.com", testPassword, false)
	rr := serveDelete(t, store, loginAs(t, store, user), "01hzzzzzzzzzzzzzzzzzzzzzzzz")
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestConnections_Delete_Unauthenticated(t *testing.T) {
	store := newCryptoStore(t)
	user := seedUser(t, store, "owner@example.com", testPassword, false)
	id := seedIdentity(t, store, user, "tw-3", "engelswtf")
	rr := serveDelete(t, store, nil, id)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

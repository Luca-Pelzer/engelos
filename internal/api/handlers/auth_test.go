package handlers_test

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

const (
	testTenant   = "local"
	testEmail    = "owner@example.com"
	testUsername = "owner"
	testPassword = "correct-horse-battery-staple"
)

func newTestStore(t *testing.T) auth.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:authtest-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := auth.OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedUser(t *testing.T, store auth.Store, email, password string, disabled bool) auth.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	u, err := store.CreateUser(context.Background(), auth.User{
		TenantID:     testTenant,
		Email:        email,
		Username:     strings.Split(email, "@")[0],
		PasswordHash: []byte(hash),
		Role:         auth.RoleOwner,
		Disabled:     disabled,
	})
	require.NoError(t, err)
	return u
}

func newAuth(t *testing.T, store auth.Store) *handlers.Auth {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return handlers.NewAuth(store, testTenant, logger).WithCookieSecure(false)
}

func doLogin(t *testing.T, h *handlers.Auth, email, password string) *http.Response {
	t.Helper()
	body := map[string]string{"email": email, "password": password}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)
	return w.Result()
}

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLogin_Success(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	user := seedUser(t, store, testEmail, testPassword, false)
	h := newAuth(t, store)

	resp := doLogin(t, h, testEmail, testPassword)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	c := findCookie(resp, apimw.SessionCookieName)
	require.NotNil(t, c, "expected session cookie")
	assert.NotEmpty(t, c.Value)
	assert.Equal(t, "/", c.Path)
	assert.True(t, c.HttpOnly)
	assert.False(t, c.Secure, "cookieSecure=false in tests")
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
	assert.Greater(t, c.MaxAge, 0)

	var body struct {
		User map[string]any `json:"user"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, user.ID, body.User["id"])
	assert.Equal(t, testEmail, body.User["email"])
	_, hasHash := body.User["password_hash"]
	_, hasHashCamel := body.User["PasswordHash"]
	_, hasTOTP := body.User["totp_secret"]
	_, hasTOTPCamel := body.User["TOTPSecret"]
	assert.False(t, hasHash, "password_hash must not be in response")
	assert.False(t, hasHashCamel, "PasswordHash must not be in response")
	assert.False(t, hasTOTP, "totp_secret must not be in response")
	assert.False(t, hasTOTPCamel, "TOTPSecret must not be in response")

	sess, err := store.GetSessionByTokenHash(context.Background(), auth.HashTokenString(c.Value))
	require.NoError(t, err)
	assert.Equal(t, user.ID, sess.UserID)
	assert.Equal(t, testTenant, sess.TenantID)

	reloaded, err := store.GetUserByID(context.Background(), testTenant, user.ID)
	require.NoError(t, err)
	assert.False(t, reloaded.LastLoginAt.IsZero(), "LastLoginAt should be set")
}

func TestLogin_WrongPassword(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	seedUser(t, store, testEmail, testPassword, false)
	h := newAuth(t, store)

	resp := doLogin(t, h, testEmail, "wrong-password")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Nil(t, findCookie(resp, apimw.SessionCookieName))

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "invalid_credentials", body["error"])
}

// countingStore counts how many times GetUserByEmail is called.
type countingStore struct {
	auth.Store
	emailLookups atomic.Int64
}

func (c *countingStore) GetUserByEmail(ctx context.Context, tenantID, email string) (auth.User, error) {
	c.emailLookups.Add(1)
	return c.Store.GetUserByEmail(ctx, tenantID, email)
}

func TestLogin_UnknownEmail_TimingEqualized(t *testing.T) {
	t.Parallel()
	base := newTestStore(t)
	store := &countingStore{Store: base}
	seedUser(t, base, testEmail, testPassword, false)
	h := newAuth(t, store)

	wrongStart := time.Now()
	wrongResp := doLogin(t, h, testEmail, "wrong-password")
	wrongDur := time.Since(wrongStart)
	wrongResp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, wrongResp.StatusCode)

	unknownStart := time.Now()
	unknownResp := doLogin(t, h, "nobody@example.com", "anything")
	unknownDur := time.Since(unknownStart)
	defer unknownResp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, unknownResp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(unknownResp.Body).Decode(&body))
	assert.Equal(t, "invalid_credentials", body["error"], "same error body for both failure modes")
	assert.Equal(t, int64(2), store.emailLookups.Load())

	// The unknown-email path MUST still run VerifyPassword against the
	// dummy hash. Argon2id with m=64MiB,t=3,p=4 takes tens of ms even on
	// fast hardware; a missing dummy-verify would return in microseconds.
	// Use a conservative threshold that still catches the no-equalization
	// bug without being flaky on slow CI.
	const minVerifyTime = 5 * time.Millisecond
	assert.GreaterOrEqual(t, unknownDur, minVerifyTime,
		"unknown-email path should run VerifyPassword (took %s)", unknownDur)
	// And the two durations should be within an order of magnitude of
	// each other (they are dominated by the same Argon2id computation).
	ratio := float64(unknownDur) / float64(wrongDur)
	if ratio < 1 {
		ratio = 1 / ratio
	}
	assert.Less(t, ratio, 10.0,
		"unknown(%s) vs wrong(%s) timings should be within 10x", unknownDur, wrongDur)
}

func TestLogin_DisabledUser(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	seedUser(t, store, testEmail, testPassword, true)
	h := newAuth(t, store)

	resp := doLogin(t, h, testEmail, testPassword)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Nil(t, findCookie(resp, apimw.SessionCookieName))

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "invalid_credentials", body["error"])
}

func TestLogin_MalformedJSON(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	h := newAuth(t, store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.Login(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "invalid_request")
}

func TestLogin_MissingFields(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	h := newAuth(t, store)

	cases := []struct {
		name string
		body string
	}{
		{"missing_email", `{"password":"x"}`},
		{"missing_password", `{"email":"a@b.c"}`},
		{"both_empty", `{"email":"","password":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
				strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			h.Login(w, req)
			resp := w.Result()
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestLogout_WithValidCookie(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	seedUser(t, store, testEmail, testPassword, false)
	h := newAuth(t, store)

	loginResp := doLogin(t, h, testEmail, testPassword)
	loginResp.Body.Close()
	c := findCookie(loginResp, apimw.SessionCookieName)
	require.NotNil(t, c)
	tokenHash := auth.HashTokenString(c.Value)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(c)
	w := httptest.NewRecorder()
	h.Logout(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	clear := findCookie(resp, apimw.SessionCookieName)
	require.NotNil(t, clear, "logout must emit a clearing Set-Cookie")
	assert.Equal(t, -1, clear.MaxAge)
	assert.Empty(t, clear.Value)

	_, err := store.GetSessionByTokenHash(context.Background(), tokenHash)
	assert.True(t, errors.Is(err, auth.ErrSessionNotFound),
		"session should be gone after logout, got %v", err)
}

func TestLogout_NoCookie(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	h := newAuth(t, store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()
	h.Logout(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.NotNil(t, findCookie(resp, apimw.SessionCookieName),
		"logout still clears the cookie even with no inbound cookie")
}

func TestLogout_InvalidCookieValue(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	h := newAuth(t, store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: apimw.SessionCookieName, Value: "garbage-no-such-session"})
	w := httptest.NewRecorder()
	h.Logout(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestMe_WithUser(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	user := seedUser(t, store, testEmail, testPassword, false)
	h := newAuth(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = req.WithContext(apimw.WithUser(req.Context(), user))
	w := httptest.NewRecorder()
	h.Me(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), user.ID)
	assert.NotContains(t, string(body), "password_hash")
	assert.NotContains(t, string(body), "PasswordHash")
	assert.NotContains(t, string(body), "totp_secret")
}

func TestMe_NoUser(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	h := newAuth(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	w := httptest.NewRecorder()
	h.Me(w, req)
	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "unauthorized", body["error"])
}

func TestNewAuth_NilStore_FallsBackTo501(t *testing.T) {
	t.Parallel()
	h := handlers.NewAuth(nil, "", nil)

	for _, tc := range []struct {
		name    string
		fn      func(w http.ResponseWriter, r *http.Request)
		method  string
		path    string
		hasBody bool
	}{
		{"login", h.Login, http.MethodPost, "/api/v1/auth/login", true},
		{"logout", h.Logout, http.MethodPost, "/api/v1/auth/logout", false},
		{"me", h.Me, http.MethodGet, "/api/v1/users/me", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var body io.Reader
			if tc.hasBody {
				body = strings.NewReader(`{"email":"a@b.c","password":"x"}`)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			w := httptest.NewRecorder()
			tc.fn(w, req)
			resp := w.Result()
			defer resp.Body.Close()
			assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
		})
	}
}

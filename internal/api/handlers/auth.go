package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

// Defaults applied by NewAuth when the caller does not override them.
const (
	// DefaultSessionTTL bounds how long a freshly-issued login session
	// remains valid before the user must re-authenticate.
	DefaultSessionTTL = 30 * 24 * time.Hour

	// DefaultCookieName is the cookie name the auth handlers issue on
	// successful login. It mirrors middleware.SessionCookieName.
	DefaultCookieName = middleware.SessionCookieName

	// dummyTimingPassword is the password we hash once at startup and
	// reuse on the unknown-email path so that VerifyPassword takes the
	// same wall-clock time as a wrong-password verification.
	dummyTimingPassword = "dummy-password-for-timing"
)

// Auth bundles the real authentication handlers (Login, Logout, Me)
// backed by an auth.Store. When the store is nil, every handler degrades
// to a 501 "not_implemented" response so that the router can still be
// constructed during early bootstrap or in router-only tests.
//
// Auth is safe for concurrent use; all mutable configuration is set at
// construction time.
type Auth struct {
	store        auth.Store
	tenantID     string
	logger       *slog.Logger
	sessionTTL   time.Duration
	cookieName   string
	cookieSecure bool

	// dummyHash is a pre-computed Argon2id hash used to equalize the
	// timing of the unknown-email path with the wrong-password path.
	// It is never compared against anything the attacker controls.
	dummyHash string
}

// NewAuth constructs the real auth handler bundle.
//
// store may be nil; in that case every handler returns 501 (this lets
// the router be built before the auth store is ready, and lets the
// router_test exercise the route table without a database). tenantID
// is the single-tenant identifier the daemon was started with - every
// authenticated request is scoped to it. A nil logger falls back to
// slog.Default.
//
// Defaults: sessionTTL = 30d, cookieName = "engelos_session",
// cookieSecure = true. Use the With* options to override.
func NewAuth(store auth.Store, tenantID string, logger *slog.Logger) *Auth {
	if logger == nil {
		logger = slog.Default()
	}
	a := &Auth{
		store:        store,
		tenantID:     strings.TrimSpace(tenantID),
		logger:       logger.With("component", "api.handlers.auth"),
		sessionTTL:   DefaultSessionTTL,
		cookieName:   DefaultCookieName,
		cookieSecure: true,
	}
	// Pre-compute a dummy hash so the unknown-email branch of Login can
	// still run the (expensive) VerifyPassword and thereby take the same
	// wall-clock time as the wrong-password branch. Failures here are
	// not fatal - if Argon2id cannot run at startup, login is already
	// broken in deeper ways.
	if h, err := auth.HashPassword(dummyTimingPassword); err == nil {
		a.dummyHash = h
	} else {
		a.logger.Warn("auth: precompute dummy hash failed; timing-equalization disabled",
			slog.Any("err", err))
	}
	return a
}

// WithSessionTTL overrides the default session lifetime. Non-positive
// values are ignored.
func (a *Auth) WithSessionTTL(d time.Duration) *Auth {
	if d > 0 {
		a.sessionTTL = d
	}
	return a
}

// WithCookieName overrides the name of the session cookie. Empty names
// are ignored.
func (a *Auth) WithCookieName(name string) *Auth {
	if strings.TrimSpace(name) != "" {
		a.cookieName = name
	}
	return a
}

// WithCookieSecure controls the Secure attribute on the session cookie.
// Tests typically pass false because httptest serves plain HTTP.
func (a *Auth) WithCookieSecure(secure bool) *Auth {
	a.cookieSecure = secure
	return a
}

// loginRequest is the shape of POST /api/v1/auth/login bodies.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// sanitizedUser is the wire-shape returned by /me and after login. It
// intentionally omits PasswordHash and TOTPSecret so they can never
// accidentally leak via JSON encoding.
type sanitizedUser struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Email       string    `json:"email"`
	Username    string    `json:"username"`
	Role        auth.Role `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastLoginAt time.Time `json:"last_login_at,omitempty"`
	Disabled    bool      `json:"disabled"`
}

func sanitize(u auth.User) sanitizedUser {
	return sanitizedUser{
		ID:          u.ID,
		TenantID:    u.TenantID,
		Email:       u.Email,
		Username:    u.Username,
		Role:        u.Role,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		LastLoginAt: u.LastLoginAt,
		Disabled:    u.Disabled,
	}
}

// Login handles POST /api/v1/auth/login.
//
// Body: {"email": "...", "password": "..."}.
//
// On success returns 200 with the sanitized user JSON and sets the
// session cookie (HttpOnly, Secure when cookieSecure=true, SameSite=Strict,
// Path=/, Max-Age=sessionTTL).
//
// On any authentication failure - unknown email, wrong password, or a
// disabled account - it returns 401 with body {"error":"invalid_credentials"}.
// Timing is equalized between the unknown-email and wrong-password paths
// so an attacker cannot enumerate accounts via response latency.
//
// 400 is returned only for malformed JSON or a missing email/password
// field.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		notImplemented(w)
		return
	}

	var req loginRequest
	// Cap body size at 4 KiB; legitimate credentials never approach that.
	dec := json.NewDecoder(io.LimitReader(r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid_request",
		})
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid_request",
		})
		return
	}

	ctx := r.Context()
	user, lookupErr := a.store.GetUserByEmail(ctx, a.tenantID, req.Email)

	if lookupErr != nil {
		// Timing-equalize: always run VerifyPassword against the dummy
		// hash so unknown-email takes ~ same time as wrong-password.
		if a.dummyHash != "" {
			_ = auth.VerifyPassword(req.Password, a.dummyHash)
		}
		if !errors.Is(lookupErr, auth.ErrUserNotFound) {
			a.logger.WarnContext(ctx, "login user lookup failed",
				slog.Any("err", lookupErr))
		}
		a.unauthorized(w)
		return
	}

	hash := string(user.PasswordHash)
	if err := auth.VerifyPassword(req.Password, hash); err != nil {
		a.unauthorized(w)
		return
	}
	if user.Disabled {
		a.unauthorized(w)
		return
	}

	token, sess, err := auth.NewSession(
		user.TenantID, user.ID,
		r.UserAgent(), r.RemoteAddr,
		a.sessionTTL,
	)
	if err != nil {
		a.logger.ErrorContext(ctx, "session mint failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error",
		})
		return
	}
	if err := a.store.CreateSession(ctx, sess); err != nil {
		a.logger.ErrorContext(ctx, "session persist failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error",
		})
		return
	}

	// Bump LastLoginAt. A failure here is non-fatal - the user is
	// already authenticated; we just lose an audit trail entry.
	user.LastLoginAt = time.Now().UTC()
	if err := a.store.UpdateUser(ctx, user); err != nil {
		a.logger.WarnContext(ctx, "update last_login_at failed", slog.Any("err", err))
	}

	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName,
		Value:    token,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		MaxAge:   int(a.sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"user": sanitize(user),
	})
}

// Logout handles POST /api/v1/auth/logout.
//
// It is fully idempotent: missing cookie, invalid cookie or
// already-deleted session all return 204. The session cookie is
// always cleared via Set-Cookie with Max-Age=-1.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		notImplemented(w)
		return
	}

	if c, err := r.Cookie(a.cookieName); err == nil && c != nil && c.Value != "" {
		hash := auth.HashTokenString(c.Value)
		if delErr := a.store.DeleteSession(r.Context(), hash); delErr != nil &&
			!errors.Is(delErr, auth.ErrSessionNotFound) {
			a.logger.WarnContext(r.Context(), "logout: delete session failed",
				slog.Any("err", delErr))
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /api/v1/users/me. It returns the sanitized current
// user when the SessionAuth middleware has injected one, or 401
// {"error":"unauthorized"} otherwise.
func (a *Auth) Me(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		notImplemented(w)
		return
	}
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized",
		})
		return
	}
	writeJSON(w, http.StatusOK, sanitize(user))
}

func (a *Auth) unauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{
		"error": "invalid_credentials",
	})
}

func notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "not_implemented",
	})
}

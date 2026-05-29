package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/auth"
)

// SessionCookieName is the default name for the engelOS session cookie.
const SessionCookieName = "engelos_session"

type sessionCtxKey struct{}

// SessionAuth resolves the session cookie into an auth.User and injects
// it into the request context. Missing, expired, malformed or unknown
// sessions are silently ignored; downstream handlers (or RequireSession)
// decide whether auth is required. An empty cookieName falls back to
// SessionCookieName.
func SessionAuth(store auth.Store, cookieName string, logger *slog.Logger) func(http.Handler) http.Handler {
	if cookieName == "" {
		cookieName = SessionCookieName
	}
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := resolveSession(r.Context(), store, cookieName, r, logger)
			if ok {
				ctx := context.WithValue(r.Context(), sessionCtxKey{}, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func resolveSession(ctx context.Context, store auth.Store, cookieName string, r *http.Request, logger *slog.Logger) (auth.User, bool) {
	if store == nil {
		return auth.User{}, false
	}
	c, err := r.Cookie(cookieName)
	if err != nil || c == nil || c.Value == "" {
		return auth.User{}, false
	}
	tokenHash := auth.HashTokenString(c.Value)

	sess, err := store.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return auth.User{}, false
		}
		logger.WarnContext(ctx, "session lookup failed",
			slog.String("token_hash", tokenHash),
			slog.Any("err", err),
		)
		return auth.User{}, false
	}

	if sess.Expired(time.Now().UTC()) {
		if delErr := store.DeleteSession(ctx, tokenHash); delErr != nil &&
			!errors.Is(delErr, auth.ErrSessionNotFound) {
			logger.WarnContext(ctx, "expired session delete failed",
				slog.String("token_hash", tokenHash),
				slog.Any("err", delErr),
			)
		}
		return auth.User{}, false
	}

	user, err := store.GetUserByID(ctx, sess.TenantID, sess.UserID)
	if err != nil {
		if !errors.Is(err, auth.ErrUserNotFound) {
			logger.WarnContext(ctx, "session user lookup failed",
				slog.String("user_id", sess.UserID),
				slog.Any("err", err),
			)
		}
		return auth.User{}, false
	}
	if user.Disabled {
		return auth.User{}, false
	}
	return user, true
}

// RequireSession responds 401 with JSON {"error":"unauthorized"} when
// no auth.User is present in the request context. Chain after SessionAuth.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFromContext(r.Context()); !ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UserFromContext returns the auth.User injected by SessionAuth, or
// ok=false if the request was unauthenticated.
func UserFromContext(ctx context.Context) (auth.User, bool) {
	v := ctx.Value(sessionCtxKey{})
	if v == nil {
		return auth.User{}, false
	}
	u, ok := v.(auth.User)
	return u, ok
}

// WithUser attaches u to ctx as if SessionAuth had resolved it. Intended
// for tests and alternative authenticators (e.g. API keys).
func WithUser(ctx context.Context, u auth.User) context.Context {
	return context.WithValue(ctx, sessionCtxKey{}, u)
}

package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/workspaces"
)

type workspaceCtxKey struct{}

// WorkspaceContext is what WorkspaceMiddleware injects: the resolved workspace
// and the current user's membership in it. Handlers under /channels/{slug} read
// it to authorize and to know which channel they operate on, instead of trusting
// a client-supplied channel parameter.
type WorkspaceContext struct {
	Workspace  workspaces.Workspace
	Membership workspaces.Membership
}

// WorkspaceMiddleware resolves the {channelSlug} URL parameter into a workspace
// and verifies that the session user has a membership in it. It MUST run after
// SessionAuth + RequireSession. A missing workspace yields 404; a logged-in user
// without a membership yields 403. This is the single chokepoint that stops any
// authenticated user from reading another channel's data: every channel-scoped
// route sits behind it.
func WorkspaceMiddleware(store workspaces.Store, tenantID string, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			slug := chi.URLParam(r, "channelSlug")
			if slug == "" {
				writeAuthError(w, http.StatusBadRequest, "missing_channel")
				return
			}
			ws, err := store.GetWorkspaceBySlug(r.Context(), tenantID, slug)
			if err != nil {
				if errors.Is(err, workspaces.ErrNotFound) {
					writeAuthError(w, http.StatusNotFound, "channel_not_found")
					return
				}
				logger.WarnContext(r.Context(), "workspace lookup failed",
					slog.String("slug", slug), slog.Any("err", err))
				writeAuthError(w, http.StatusInternalServerError, "store_error")
				return
			}
			member, err := store.GetMembership(r.Context(), ws.ID, user.ID)
			if err != nil {
				if errors.Is(err, workspaces.ErrNotFound) {
					writeAuthError(w, http.StatusForbidden, "not_a_member")
					return
				}
				logger.WarnContext(r.Context(), "membership lookup failed",
					slog.String("workspace", ws.ID), slog.Any("err", err))
				writeAuthError(w, http.StatusInternalServerError, "store_error")
				return
			}
			ctx := context.WithValue(r.Context(), workspaceCtxKey{},
				WorkspaceContext{Workspace: ws, Membership: member})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole responds 403 unless the request's workspace membership role is one
// of the allowed roles. Chain it after WorkspaceMiddleware on routes that need
// more than mere membership (for example owner-only settings).
func RequireRole(roles ...workspaces.Role) func(http.Handler) http.Handler {
	allowed := make(map[workspaces.Role]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wc, ok := WorkspaceFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusForbidden, "forbidden")
				return
			}
			if _, ok := allowed[wc.Membership.Role]; !ok {
				writeAuthError(w, http.StatusForbidden, "insufficient_role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WorkspaceFromContext returns the WorkspaceContext injected by
// WorkspaceMiddleware, or ok=false if the route was not workspace-scoped.
func WorkspaceFromContext(ctx context.Context) (WorkspaceContext, bool) {
	v := ctx.Value(workspaceCtxKey{})
	if v == nil {
		return WorkspaceContext{}, false
	}
	wc, ok := v.(WorkspaceContext)
	return wc, ok
}

// WithWorkspace attaches wc to ctx as if WorkspaceMiddleware had resolved it.
// Intended for tests.
func WithWorkspace(ctx context.Context, wc WorkspaceContext) context.Context {
	return context.WithValue(ctx, workspaceCtxKey{}, wc)
}

func writeAuthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `"}`))
}

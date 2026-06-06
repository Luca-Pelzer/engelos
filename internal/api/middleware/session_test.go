package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

func TestRequireGlobalOwner(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guarded := apimw.RequireGlobalOwner(ok)

	t.Run("unauthenticated is 401", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("viewer is 403", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/stats", nil)
		req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleViewer}))
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("mod is 403", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/stats", nil)
		req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleMod}))
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("owner passes", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/stats", nil)
		req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleOwner}))
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("admin passes", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/stats", nil)
		req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleAdmin}))
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

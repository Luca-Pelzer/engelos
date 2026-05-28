package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	apimw "github.com/engelos-bot/engelos/internal/api/middleware"
)

func TestCORS_Preflight(t *testing.T) {
	t.Parallel()

	handler := apimw.CORS(apimw.CORSOptions{
		AllowedOrigins:   []string{"https://example.com"},
		AllowCredentials: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("preflight should be terminated by middleware")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/anything", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	res := w.Result()
	defer res.Body.Close()
	assert.Equal(t, http.StatusNoContent, res.StatusCode)
	assert.Equal(t, "https://example.com", res.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", res.Header.Get("Access-Control-Allow-Credentials"))
	assert.Contains(t, res.Header.Get("Access-Control-Allow-Methods"), "POST")
	assert.Equal(t, "Content-Type", res.Header.Get("Access-Control-Allow-Headers"))
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	t.Parallel()

	called := false
	handler := apimw.CORS(apimw.CORSOptions{
		AllowedOrigins: []string{"https://allowed.example"},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called, "non-preflight requests must still be served")
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_Wildcard(t *testing.T) {
	t.Parallel()

	handler := apimw.CORS(apimw.CORSOptions{
		AllowedOrigins: []string{"*"},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://any.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	handler := apimw.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	h := w.Header()
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", h.Get("Referrer-Policy"))
	assert.NotEmpty(t, h.Get("Content-Security-Policy"))
}

func TestJSONContentType_OnlyForAPI(t *testing.T) {
	t.Parallel()

	apiHandler := apimw.JSONContentType(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/foo", nil)
	w := httptest.NewRecorder()
	apiHandler.ServeHTTP(w, req)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	w2 := httptest.NewRecorder()
	apiHandler.ServeHTTP(w2, req2)
	assert.NotContains(t, w2.Header().Get("Content-Type"), "application/json")
}

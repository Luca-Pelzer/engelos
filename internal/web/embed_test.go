package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/web"
)

func fallbackHandler(body string, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func TestAvailable_DevBuildHasOnlyGitkeep(t *testing.T) {
	t.Parallel()
	if web.Available() {
		t.Skip("build/ has been populated by `make web-build`; skipping dev-path assertion")
	}
	assert.False(t, web.Available())
}

func TestHandler_NilWhenUnavailable(t *testing.T) {
	t.Parallel()
	if web.Available() {
		t.Skip("UI is embedded; Handler will not return nil")
	}
	assert.Nil(t, web.Handler(fallbackHandler(`{}`, http.StatusNotFound)))
}

func TestHandler_ServesIndex(t *testing.T) {
	t.Parallel()
	if !web.Available() {
		t.Skip("no embedded UI; run `make web-build` to exercise this path")
	}

	h := web.Handler(fallbackHandler(`{"fallback":true}`, http.StatusNotFound))
	require.NotNil(t, h)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, res.Header.Get("Content-Type"), "text/html")
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))

	body, _ := io.ReadAll(res.Body)
	assert.Contains(t, strings.ToLower(string(body)), "<html")
}

func TestHandler_ExtensionlessRouteResolvesHTML(t *testing.T) {
	t.Parallel()
	if !web.Available() {
		t.Skip("no embedded UI")
	}

	h := web.Handler(fallbackHandler(`{}`, http.StatusNotFound))
	require.NotNil(t, h)

	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, res.Header.Get("Content-Type"), "text/html")
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
}

func TestHandler_FallbackForUnknownPath(t *testing.T) {
	t.Parallel()
	if !web.Available() {
		t.Skip("no embedded UI")
	}

	h := web.Handler(fallbackHandler(`{"fallback":true}`, http.StatusTeapot))
	require.NotNil(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/something", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusTeapot, res.StatusCode)
	assert.Contains(t, res.Header.Get("Content-Type"), "application/json")

	body, _ := io.ReadAll(res.Body)
	assert.Contains(t, string(body), "fallback")
}

func TestHandler_ImmutableCacheForAppAssets(t *testing.T) {
	t.Parallel()
	if !web.Available() {
		t.Skip("no embedded UI")
	}

	appPath := findFirstAppAsset(t)
	if appPath == "" {
		t.Skip("no _app/* asset found in embed")
	}

	h := web.Handler(fallbackHandler(`{}`, http.StatusNotFound))
	req := httptest.NewRequest(http.MethodGet, "/"+appPath, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t,
		"public, max-age=31536000, immutable",
		res.Header.Get("Cache-Control"),
		"hashed asset %s should be served with immutable cache header", appPath,
	)
}

func TestHandler_ContentTypeDetection(t *testing.T) {
	t.Parallel()
	if !web.Available() {
		t.Skip("no embedded UI")
	}

	h := web.Handler(fallbackHandler(`{}`, http.StatusNotFound))

	req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusOK {
		assert.Contains(t, res.Header.Get("Content-Type"), "image/svg")
	} else {
		t.Skipf("favicon.svg not present in this build (status %d)", res.StatusCode)
	}
}

func findFirstAppAsset(t *testing.T) string {
	t.Helper()
	var found string
	walk := func(p string) bool {
		f, err := web.FS.Open("build/" + p)
		if err != nil {
			return false
		}
		_ = f.Close()
		found = p
		return true
	}
	candidates := []string{
		"_app/version.json",
		"_app/env.js",
	}
	for _, c := range candidates {
		if walk(c) {
			return found
		}
	}
	entries, err := web.FS.ReadDir("build/_app")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			return "_app/" + e.Name()
		}
	}
	return ""
}

package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/engelos-bot/engelos/internal/api"
	"github.com/engelos-bot/engelos/internal/api/handlers"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	r := api.NewRouter(api.Deps{
		Version: handlers.Version{Version: "test", Phase: "0"},
	})
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts
}

func TestRouter_Healthz(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestRouter_Readyz(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/readyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ready", body["status"])
}

func TestRouter_Version(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/version")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "test", body["version"])
	assert.Equal(t, "0", body["phase"])
}

func TestRouter_Index(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "engelOS")
}

func TestRouter_AuthStubs_Return501(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)

	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodGet, "/api/v1/users/me"},
	} {
		req, err := http.NewRequest(route.method, ts.URL+route.path, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotImplemented, resp.StatusCode,
			"route %s %s", route.method, route.path)
		assert.Contains(t, string(body), "not_implemented",
			"route %s %s body", route.method, route.path)
	}
}

func TestRouter_RequestIDHeader(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.NotEmpty(t, resp.Header.Get("X-Request-Id"))
}

func TestRouter_NotFoundIsJSONForAPI(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/nonsense")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRouter_SSEHeartbeat(t *testing.T) {
	t.Parallel()
	r := api.NewRouter(api.Deps{
		Version:         handlers.Version{Version: "t", Phase: "0"},
		EventsHeartbeat: 50 * time.Millisecond,
	})
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	buf := make([]byte, 256)
	deadline := time.Now().Add(1500 * time.Millisecond)
	gotHeartbeat := false
	for time.Now().Before(deadline) && !gotHeartbeat {
		n, err := resp.Body.Read(buf)
		if n > 0 && strings.Contains(string(buf[:n]), "heartbeat") {
			gotHeartbeat = true
			break
		}
		if err != nil {
			break
		}
	}
	assert.True(t, gotHeartbeat, "expected at least one heartbeat event")
}

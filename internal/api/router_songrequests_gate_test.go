package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api"
	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/songrequests"
	"github.com/Luca-Pelzer/engelos/internal/songrequests/queue"
)

// TestRouter_SongRequests_Gated proves the RES-19 Option-A quarantine at the
// routing layer: the songrequests surface is mounted only when its store is
// wired (feature on), and returns 404 — not a mounted 501 — when the store is
// nil (feature off, ENGELOS_FEATURE_SONGREQUESTS unset). The store is nil-able,
// so this is a pure config gate: no handler or code is removed.
func TestRouter_SongRequests_Gated(t *testing.T) {
	t.Parallel()

	// Feature OFF: nil store => route not registered => 404.
	t.Run("off returns 404", func(t *testing.T) {
		t.Parallel()
		r := api.NewRouter(api.Deps{
			Version: handlers.Version{Version: "t", Phase: "0"},
			// SongRequestStore intentionally nil.
		})
		ts := httptest.NewServer(r)
		t.Cleanup(ts.Close)

		for _, path := range []string{
			"/api/v1/channels/demo/songrequests",
			"/api/v1/channels/demo/songrequests/",
			"/api/v1/songqueue/next?channel=x",
		} {
			resp, err := http.Get(ts.URL + path)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode,
				"feature off: %s must 404 (quarantined), not be a mounted 501", path)
		}
	})

	// Feature ON: real store => route mounted => reachable (not 404).
	t.Run("on mounts the route", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		srStore, err := songrequests.OpenSQLiteStore(ctx, "file:res19sr?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = srStore.Close() })
		sqStore, err := queue.OpenSQLiteStore(ctx, "file:res19sq?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = sqStore.Close() })

		r := api.NewRouter(api.Deps{
			Version:          handlers.Version{Version: "t", Phase: "0"},
			TenantID:         "t1",
			SongRequestStore: srStore,
			SongQueueStore:   sqStore,
		})
		ts := httptest.NewServer(r)
		t.Cleanup(ts.Close)

		// Per-channel config route (frontend reaches it via channelApi()).
		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/songrequests")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"feature on: /channels/{slug}/songrequests must be mounted and reachable")

		// Public overlay queue route.
		qresp, err := http.Get(ts.URL + "/api/v1/songqueue/next?channel=demo")
		require.NoError(t, err)
		_ = qresp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, qresp.StatusCode,
			"feature on: /songqueue/next must be mounted and reachable")
	})
}

// TestRouter_Capabilities proves the dashboard capability map reflects the same
// nil-check that gates the route, so the "Music Plugin" nav card is only shown
// when the backend is actually mounted.
func TestRouter_Capabilities(t *testing.T) {
	t.Parallel()

	get := func(t *testing.T, d api.Deps) map[string]bool {
		t.Helper()
		ts := httptest.NewServer(api.NewRouter(d))
		t.Cleanup(ts.Close)
		resp, err := http.Get(ts.URL + "/api/v1/capabilities")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		var got map[string]bool
		require.NoError(t, json.Unmarshal(body, &got))
		return got
	}

	t.Run("off reports songrequests false", func(t *testing.T) {
		t.Parallel()
		got := get(t, api.Deps{Version: handlers.Version{Version: "t", Phase: "0"}})
		assert.False(t, got["songrequests"], "nil store => capability false => card hidden")
	})

	t.Run("on reports songrequests true", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		srStore, err := songrequests.OpenSQLiteStore(ctx, "file:res19caps?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = srStore.Close() })
		got := get(t, api.Deps{
			Version:          handlers.Version{Version: "t", Phase: "0"},
			SongRequestStore: srStore,
		})
		assert.True(t, got["songrequests"], "wired store => capability true => card shown")
	})
}

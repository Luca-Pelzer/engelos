package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api"
	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/counters"
)

// TestRouter_Counters_PluginGated proves the counters pilot at the routing
// layer: disabled (nil store) => /counters routes 404 (unmounted); enabled =>
// mounted and reachable.
func TestRouter_Counters_PluginGated(t *testing.T) {
	t.Parallel()

	t.Run("disabled returns 404", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:  handlers.Version{Version: "t", Phase: "0"},
			TenantID: "local",
		}))
		t.Cleanup(ts.Close)
		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/counters?channel=demo")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"counters disabled: route must 404 (unmounted), not a mounted 501")
	})

	t.Run("enabled mounts the route", func(t *testing.T) {
		t.Parallel()
		store, err := counters.OpenSQLiteStore(context.Background(), "file:countersgate?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:      handlers.Version{Version: "t", Phase: "0"},
			TenantID:     "local",
			CounterStore: store,
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/counters?channel=demo")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"counters enabled: route must be mounted and reachable")
	})
}

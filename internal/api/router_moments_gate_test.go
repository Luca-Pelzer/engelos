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
	"github.com/Luca-Pelzer/engelos/internal/moments"
)

// TestRouter_Moments_PluginGated proves the moments pilot at the routing layer:
// disabled (nil store) => /moments routes 404 (unmounted); enabled => mounted
// and reachable (200 for a valid channel with no active moment).
func TestRouter_Moments_PluginGated(t *testing.T) {
	t.Parallel()

	t.Run("disabled returns 404", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:  handlers.Version{Version: "t", Phase: "0"},
			TenantID: "local",
		}))
		t.Cleanup(ts.Close)
		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/moments?channel=demo")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"moments disabled: route must 404 (unmounted), not a mounted 501")
	})

	t.Run("enabled mounts the route", func(t *testing.T) {
		t.Parallel()
		store, err := moments.OpenSQLiteStore(context.Background(), "file:momentsgate?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:      handlers.Version{Version: "t", Phase: "0"},
			TenantID:     "local",
			MomentsStore: store,
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/moments?channel=demo")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"moments enabled: route must be mounted and reachable")
	})
}

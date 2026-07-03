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
	"github.com/Luca-Pelzer/engelos/internal/wrapped"
)

// TestRouter_Wrapped_PluginGated proves the wrapped pilot at the routing layer.
// The request omits the channel param so the mounted handler answers 400
// ("channel is required"); an unmounted route answers a routing 404. This
// distinguishes "route gone" (disabled) from the handler's own 404 on a viewer
// with no recap data. The route is public, so no auth is involved.
func TestRouter_Wrapped_PluginGated(t *testing.T) {
	t.Parallel()

	t.Run("disabled returns 404", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:  handlers.Version{Version: "t", Phase: "0"},
			TenantID: "local",
		}))
		t.Cleanup(ts.Close)
		resp, err := http.Get(ts.URL + "/api/v1/wrapped")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"wrapped disabled: route must 404 (unmounted), not a mounted 501")
	})

	t.Run("enabled mounts the route", func(t *testing.T) {
		t.Parallel()
		store, err := wrapped.OpenSQLiteStore(context.Background(), "file:wrappedgate?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:      handlers.Version{Version: "t", Phase: "0"},
			TenantID:     "local",
			WrappedStore: store,
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/wrapped")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"wrapped enabled: mounted handler answers 400 (channel required), not a routing 404")
	})
}

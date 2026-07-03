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
	"github.com/Luca-Pelzer/engelos/internal/quotes"
)

// TestRouter_Quotes_PluginGated proves the quotes pilot at the routing layer:
// when the quotes plugin is disabled main passes a nil QuoteStore, so the routes
// are unmounted and 404 (like the songrequests RES-19 gate) rather than serving
// a mounted 501; when enabled the routes are mounted and reachable.
func TestRouter_Quotes_PluginGated(t *testing.T) {
	t.Parallel()

	t.Run("disabled returns 404", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:  handlers.Version{Version: "t", Phase: "0"},
			TenantID: "local",
			// QuoteStore intentionally nil (plugin disabled).
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/quotes?channel=demo")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"quotes disabled: route must 404 (unmounted), not a mounted 501")
	})

	t.Run("enabled mounts the route", func(t *testing.T) {
		t.Parallel()
		store, err := quotes.OpenSQLiteStore(context.Background(), "file:quotesgate?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:    handlers.Version{Version: "t", Phase: "0"},
			TenantID:   "local",
			QuoteStore: store,
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/quotes?channel=demo")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"quotes enabled: route must be mounted and reachable")
	})
}

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
	"github.com/Luca-Pelzer/engelos/internal/kb"
)

// TestRouter_KB_NilStoreGated proves the kb routes follow the plugin-gate
// convention: a nil KBStore leaves them unmounted (404), a wired store mounts
// them (auth-walled, so anything but 404 proves the mount).
func TestRouter_KB_NilStoreGated(t *testing.T) {
	t.Parallel()

	t.Run("nil store returns 404", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:  handlers.Version{Version: "t", Phase: "0"},
			TenantID: "local",
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/kb?channel=demo")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("wired store mounts the routes", func(t *testing.T) {
		t.Parallel()
		store, err := kb.OpenSQLiteStore(context.Background(), "file:kbgate?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:  handlers.Version{Version: "t", Phase: "0"},
			TenantID: "local",
			KBStore:  store,
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/channels/demo/kb?channel=demo")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode)
	})
}

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
	"github.com/Luca-Pelzer/engelos/internal/plugins"
)

type gatePlugin struct{ id string }

func (p gatePlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{ID: p.id, Name: p.id, Description: "test", Tier: plugins.TierLegacy, DefaultEnabled: true}
}

// TestRouter_Plugins_Gated proves the plugin catalog is mounted only when a
// registry is wired: nil registry => /api/v1/plugins 404 (unmounted, not a 501),
// wired registry (no AuthStore in the test, so the owner gate is inert) => 200.
func TestRouter_Plugins_Gated(t *testing.T) {
	t.Parallel()

	t.Run("nil registry 404", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version: handlers.Version{Version: "t", Phase: "0"},
		}))
		t.Cleanup(ts.Close)
		resp, err := http.Get(ts.URL + "/api/v1/plugins")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("wired registry lists", func(t *testing.T) {
		t.Parallel()
		reg := plugins.NewRegistry()
		require.NoError(t, reg.Register(gatePlugin{id: "quotes"}))
		store, err := plugins.OpenSQLiteStore(context.Background(), "file:pluginsgate?mode=memory&cache=shared", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:        handlers.Version{Version: "t", Phase: "0"},
			TenantID:       "local",
			PluginRegistry: reg,
			PluginState:    store,
			PluginsActive:  map[string]bool{"quotes": true},
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/plugins")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		var got []map[string]any
		require.NoError(t, json.Unmarshal(body, &got))
		require.Len(t, got, 1)
		assert.Equal(t, "quotes", got[0]["id"])
		assert.Equal(t, true, got[0]["enabled"])
	})
}

// TestRouter_Capabilities_KeysUnchanged locks the capabilities key set at the
// router layer: the plugin boundary must not add keys to the existing map that
// web/ consumers read.
func TestRouter_Capabilities_KeysUnchanged(t *testing.T) {
	t.Parallel()
	reg := plugins.NewRegistry()
	require.NoError(t, reg.Register(gatePlugin{id: "quotes"}))

	ts := httptest.NewServer(api.NewRouter(api.Deps{
		Version:        handlers.Version{Version: "t", Phase: "0"},
		TenantID:       "local",
		PluginRegistry: reg,
		PluginsActive:  map[string]bool{"quotes": true},
	}))
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v1/capabilities")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	var got map[string]bool
	require.NoError(t, json.Unmarshal(body, &got))
	_, hasSongrequests := got["songrequests"]
	assert.True(t, hasSongrequests, "songrequests key preserved")
	assert.Len(t, got, 1, "capabilities must not gain keys from the plugin boundary")
}

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/plugins"
)

// TestPlugins_ListsAllBuiltins proves GET /api/v1/plugins surfaces all nine
// builtins with the correct tier and default, so the dashboard renders the full
// toggle list. songrequests is the one default-off plugin (RES-19); every other
// builtin defaults on to preserve today's always-opened stores.
func TestPlugins_ListsAllBuiltins(t *testing.T) {
	reg := plugins.NewRegistry()
	for _, p := range []plugins.Plugin{
		plugins.Quotes(), plugins.Counters(), plugins.Loyalty(), plugins.Pity(), plugins.Streak(),
		plugins.Liveops(), plugins.Wrapped(), plugins.Moments(), plugins.Songrequests(),
	} {
		require.NoError(t, reg.Register(p))
	}
	store := newPluginTestStore(t)
	// The active snapshot mirrors the manifest defaults: everything on except
	// songrequests, which stays default-off per RES-19.
	active := map[string]bool{
		"quotes": true, "counters": true, "loyalty": true, "pity": true, "streak": true,
		"liveops": true, "wrapped": true, "moments": true, "songrequests": false,
	}
	h := NewPlugins(reg, store, active, "local", nil)

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/plugins", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var views []pluginView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &views))
	require.Len(t, views, 9, "all nine migrated plugins are listed")

	byID := make(map[string]pluginView, len(views))
	for _, v := range views {
		byID[v.ID] = v
	}
	want := map[string]struct {
		tier           string
		defaultEnabled bool
	}{
		"quotes":       {"legacy", true},
		"counters":     {"legacy", true},
		"loyalty":      {"legacy", true},
		"pity":         {"template", true},
		"streak":       {"template", true},
		"liveops":      {"template", true},
		"wrapped":      {"template", true},
		"moments":      {"template", true},
		"songrequests": {"legacy", false},
	}
	for id, w := range want {
		v, ok := byID[id]
		require.True(t, ok, "plugin %q listed", id)
		assert.Equal(t, w.tier, v.Tier, "plugin %q tier", id)
		assert.Equal(t, w.defaultEnabled, v.DefaultEnabled, "plugin %q default_enabled preserves today's behaviour", id)
		assert.Equal(t, w.defaultEnabled, v.Enabled, "plugin %q enabled matches the active snapshot", id)
	}
}

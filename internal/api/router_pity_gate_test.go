package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api"
	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
)

// TestRouter_Pity_PluginGated proves the pity pilot at the routing layer:
// disabled (nil system) => /pity routes 404 (unmounted); enabled => mounted and
// reachable. No AuthStore is wired, so the owner gate is inert and the route
// resolves to its handler.
func TestRouter_Pity_PluginGated(t *testing.T) {
	t.Parallel()

	t.Run("disabled returns 404", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:  handlers.Version{Version: "t", Phase: "0"},
			TenantID: "local",
		}))
		t.Cleanup(ts.Close)
		resp, err := http.Get(ts.URL + "/api/v1/pity/status?channel=demo&viewer=v1")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"pity disabled: route must 404 (unmounted), not a mounted 501")
	})

	t.Run("enabled mounts the route", func(t *testing.T) {
		t.Parallel()
		store, err := eventsourcing.OpenSQLite(context.Background(), "file:pitygate?mode=memory&cache=shared")
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		sys, err := pity.New(pity.DefaultConfig(), store, slog.New(slog.NewTextHandler(io.Discard, nil)))
		require.NoError(t, err)

		ts := httptest.NewServer(api.NewRouter(api.Deps{
			Version:  handlers.Version{Version: "t", Phase: "0"},
			TenantID: "local",
			Pity:     sys,
		}))
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/pity/status?channel=demo&viewer=v1")
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"pity enabled: route must be mounted and reachable")
	})
}

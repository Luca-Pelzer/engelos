package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/plugins"
)

type fixturePlugin struct {
	id  string
	def bool
}

func (f fixturePlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:             f.id,
		Name:           "Fixture " + f.id,
		Description:    "test plugin",
		Tier:           plugins.TierExperimental,
		DefaultEnabled: f.def,
	}
}

func newPluginTestStore(t *testing.T) plugins.StateStore {
	t.Helper()
	s, err := plugins.OpenSQLiteStore(context.Background(), "file::memory:?cache=shared", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// pluginTestReg registers a default-off fixture ("fixturecap") and a default-on
// pilot ("quotes"), mirroring the songrequests-style off and the quotes-style on.
func pluginTestReg(t *testing.T) *plugins.Registry {
	t.Helper()
	reg := plugins.NewRegistry()
	require.NoError(t, reg.Register(fixturePlugin{id: "fixturecap", def: false}))
	require.NoError(t, reg.Register(fixturePlugin{id: "quotes", def: true}))
	return reg
}

func decodePluginViews(t *testing.T, rec *httptest.ResponseRecorder) []pluginView {
	t.Helper()
	var out []pluginView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func TestPlugins_ListReportsDefaultCurrentDesired(t *testing.T) {
	reg := pluginTestReg(t)
	store := newPluginTestStore(t)
	// Startup snapshot: quotes mounted, fixturecap not.
	active := map[string]bool{"quotes": true, "fixturecap": false}
	h := NewPlugins(reg, store, active, "local", nil)

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/plugins", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	views := decodePluginViews(t, rec)
	require.Len(t, views, 2)
	// Stable order by id: fixturecap, quotes.
	assert.Equal(t, "fixturecap", views[0].ID)
	assert.False(t, views[0].DefaultEnabled)
	assert.False(t, views[0].Enabled)
	assert.False(t, views[0].Desired)
	assert.False(t, views[0].RestartRequired)

	assert.Equal(t, "quotes", views[1].ID)
	assert.True(t, views[1].DefaultEnabled)
	assert.True(t, views[1].Enabled)
	assert.True(t, views[1].Desired)
	assert.Equal(t, "experimental", views[1].Tier)
}

func TestPlugins_UpdatePersistsAndFlagsRestart(t *testing.T) {
	reg := pluginTestReg(t)
	store := newPluginTestStore(t)
	active := map[string]bool{"quotes": true, "fixturecap": false}
	h := NewPlugins(reg, store, active, "local", nil)

	// Enable fixturecap: it is not mounted this process, so restart_required.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/plugins/fixturecap", strings.NewReader(`{"enabled":true}`))
	req = withPluginURLParam(req, "fixturecap")
	h.Update(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var v pluginView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v))
	assert.True(t, v.Desired)
	assert.False(t, v.Enabled, "not mounted this process")
	assert.True(t, v.RestartRequired, "desired on but not mounted => restart required")

	// Persisted: a fresh List reflects desired=true.
	lrec := httptest.NewRecorder()
	h.List(lrec, httptest.NewRequest(http.MethodGet, "/plugins", nil))
	views := decodePluginViews(t, lrec)
	assert.True(t, views[0].Desired, "fixturecap desired persisted")
	assert.True(t, views[0].RestartRequired)

	// Disabling quotes (currently mounted) also flags restart.
	drec := httptest.NewRecorder()
	dreq := httptest.NewRequest(http.MethodPut, "/plugins/quotes", strings.NewReader(`{"enabled":false}`))
	dreq = withPluginURLParam(dreq, "quotes")
	h.Update(drec, dreq)
	require.Equal(t, http.StatusOK, drec.Code)
	require.NoError(t, json.Unmarshal(drec.Body.Bytes(), &v))
	assert.False(t, v.Desired)
	assert.True(t, v.Enabled, "still mounted until restart")
	assert.True(t, v.RestartRequired)
}

func TestPlugins_UpdateUnknownIs404(t *testing.T) {
	h := NewPlugins(pluginTestReg(t), newPluginTestStore(t), nil, "local", nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/plugins/nope", strings.NewReader(`{"enabled":true}`))
	req = withPluginURLParam(req, "nope")
	h.Update(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPlugins_UpdateMissingEnabledIs400(t *testing.T) {
	h := NewPlugins(pluginTestReg(t), newPluginTestStore(t), nil, "local", nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/plugins/quotes", strings.NewReader(`{}`))
	req = withPluginURLParam(req, "quotes")
	h.Update(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPlugins_NilDepsNotImplemented(t *testing.T) {
	lrec := httptest.NewRecorder()
	NewPlugins(nil, nil, nil, "local", nil).List(lrec, httptest.NewRequest(http.MethodGet, "/plugins", nil))
	assert.Equal(t, http.StatusNotImplemented, lrec.Code)

	urec := httptest.NewRecorder()
	ureq := withPluginURLParam(httptest.NewRequest(http.MethodPut, "/plugins/quotes", strings.NewReader(`{"enabled":true}`)), "quotes")
	NewPlugins(pluginTestReg(t), nil, nil, "local", nil).Update(urec, ureq)
	assert.Equal(t, http.StatusNotImplemented, urec.Code, "nil state => Update 501")
}

// TestPlugins_OwnerGate proves the exact middleware the router applies to
// /api/v1/plugins: unauthenticated is 401, a non-owner is 403, an owner is 200.
func TestPlugins_OwnerGate(t *testing.T) {
	h := NewPlugins(pluginTestReg(t), newPluginTestStore(t), nil, "local", nil)
	gated := apimw.RequireGlobalOwner(http.HandlerFunc(h.List))

	t.Run("unauthenticated 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plugins", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
	t.Run("non-owner 403", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/plugins", nil)
		req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleViewer}))
		gated.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
	t.Run("owner 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/plugins", nil)
		req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleOwner}))
		gated.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// TestPlugins_FixtureBoundary_RestartSimulation proves the restart-based
// boundary end to end: a fixture plugin's route and capability are gated by the
// Active snapshot a process captures at startup. Toggling via the API persists
// desired state; recomputing Active (as a restart would) flips whether the route
// mounts and the capability reports true.
func TestPlugins_FixtureBoundary_RestartSimulation(t *testing.T) {
	reg := plugins.NewRegistry()
	require.NoError(t, reg.Register(fixturePlugin{id: "fixturecap", def: false}))
	store := newPluginTestStore(t)
	ctx := context.Background()

	// buildProcess simulates one server boot: it resolves Active from persisted
	// state and mounts the fixture route + capability only when active.
	buildProcess := func() (map[string]bool, http.Handler) {
		active, err := reg.Active(ctx, store, "local")
		require.NoError(t, err)
		r := chi.NewRouter()
		if active["fixturecap"] {
			r.Get("/fixture/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		}
		r.Get("/caps", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]bool{"fixturecap": active["fixturecap"]})
		})
		return active, r
	}

	capOf := func(h http.Handler) bool {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/caps", nil))
		var m map[string]bool
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
		return m["fixturecap"]
	}
	routeStatus := func(h http.Handler) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fixture/ping", nil))
		return rec.Code
	}

	// Boot 1: default-off, nothing persisted => route 404, capability false.
	active, proc := buildProcess()
	assert.False(t, active["fixturecap"])
	assert.Equal(t, http.StatusNotFound, routeStatus(proc))
	assert.False(t, capOf(proc))

	// Toggle ON via the API handler (owner already gated at router layer).
	h := NewPlugins(reg, store, active, "local", nil)
	rec := httptest.NewRecorder()
	req := withPluginURLParam(httptest.NewRequest(http.MethodPut, "/plugins/fixturecap", strings.NewReader(`{"enabled":true}`)), "fixturecap")
	h.Update(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// Boot 2 (simulated restart): route mounts, capability true.
	_, proc2 := buildProcess()
	assert.Equal(t, http.StatusOK, routeStatus(proc2))
	assert.True(t, capOf(proc2))

	// Toggle OFF, restart again: route 404, capability false.
	rec2 := httptest.NewRecorder()
	req2 := withPluginURLParam(httptest.NewRequest(http.MethodPut, "/plugins/fixturecap", strings.NewReader(`{"enabled":false}`)), "fixturecap")
	h.Update(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	_, proc3 := buildProcess()
	assert.Equal(t, http.StatusNotFound, routeStatus(proc3))
	assert.False(t, capOf(proc3))
}

// TestCapabilities_ShapeUnchanged locks the capabilities endpoint contract: it
// must emit exactly the pre-plugin key set so existing web/ consumers keep
// working. The plugin boundary is a separate endpoint and must not leak in.
func TestCapabilities_ShapeUnchanged(t *testing.T) {
	rec := httptest.NewRecorder()
	Capabilities(CapabilityFlags{SongRequests: true}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]bool
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, map[string]bool{"songrequests": true}, got,
		"capabilities must keep exactly its pre-change keys")
}

func withPluginURLParam(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

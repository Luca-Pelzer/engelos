package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/integrations"
	"github.com/Luca-Pelzer/engelos/internal/integrations/builtins"
)

type fakeCreds struct {
	mu   sync.Mutex
	data map[string]map[string]string
}

func (f *fakeCreds) ListKeys(_ context.Context, _, integration string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.data[integration]))
	for k := range f.data[integration] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *fakeCreds) Put(_ context.Context, _, integration, key, plaintext string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data[integration] == nil {
		f.data[integration] = map[string]string{}
	}
	f.data[integration][key] = plaintext
	return nil
}

func (f *fakeCreds) Delete(_ context.Context, _, integration string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, integration)
	return nil
}

type fakeIntegration struct{ id string }

func (f fakeIntegration) Manifest() integrations.Manifest {
	return integrations.Manifest{
		ID: f.id, Name: "ElevenLabs", Description: "text-to-speech",
		Icon: "eleven", AuthKind: integrations.AuthAPIKey, SetupHref: "/integrations/" + f.id,
	}
}

func (fakeIntegration) RegisterNodes(*actions.Registry) error { return nil }

func setupIntegrations(t *testing.T) (*fakeCreds, http.Handler) {
	t.Helper()
	reg := integrations.NewRegistry()
	require.NoError(t, reg.Register(fakeIntegration{id: "elevenlabs"}))
	creds := &fakeCreds{data: map[string]map[string]string{}}
	h := NewIntegrations(reg, creds, "local", nil)
	r := chi.NewRouter()
	r.Get("/api/v1/integrations", h.List)
	r.Put("/api/v1/integrations/{id}/credentials", h.PutCredentials)
	r.Delete("/api/v1/integrations/{id}/credentials", h.DeleteCredentials)
	return creds, r
}

func putJSON(srv http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestIntegrations_ListReportsConnectedAndKeys(t *testing.T) {
	creds, srv := setupIntegrations(t)

	rec := doReq(srv, http.MethodGet, "/api/v1/integrations")
	require.Equal(t, http.StatusOK, rec.Code)
	var list []integrationView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, "elevenlabs", list[0].ID)
	assert.Equal(t, "apikey", list[0].AuthKind)
	assert.False(t, list[0].Connected)
	assert.Empty(t, list[0].CredentialKeys)

	creds.data["elevenlabs"] = map[string]string{"api_key": "sk-secret"}
	rec = doReq(srv, http.MethodGet, "/api/v1/integrations")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.True(t, list[0].Connected)
	assert.Equal(t, []string{"api_key"}, list[0].CredentialKeys)
}

func TestIntegrations_PutStoresAndMasks(t *testing.T) {
	creds, srv := setupIntegrations(t)

	rec := putJSON(srv, "/api/v1/integrations/elevenlabs/credentials", `{"values":{"api_key":"sk-supersecretvalue123"}}`)
	require.Equal(t, http.StatusOK, rec.Code)
	out := rec.Body.String()
	assert.NotContains(t, out, "sk-supersecretvalue123", "response must never echo the plaintext")
	assert.Contains(t, out, `"set":true`)
	assert.Contains(t, out, `"connected":true`)
	assert.Equal(t, "sk-supersecretvalue123", creds.data["elevenlabs"]["api_key"], "value is stored")
}

func TestIntegrations_PutUnknownIntegration404(t *testing.T) {
	_, srv := setupIntegrations(t)
	rec := putJSON(srv, "/api/v1/integrations/nope/credentials", `{"values":{"k":"v"}}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestIntegrations_DeleteRevokes(t *testing.T) {
	creds, srv := setupIntegrations(t)
	creds.data["elevenlabs"] = map[string]string{"api_key": "x"}

	rec := doReq(srv, http.MethodDelete, "/api/v1/integrations/elevenlabs/credentials")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"connected":false`)
	assert.Empty(t, creds.data["elevenlabs"])

	rec = doReq(srv, http.MethodGet, "/api/v1/integrations")
	var list []integrationView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.False(t, list[0].Connected)
}

func TestIntegrations_NilRegistryNotImplemented(t *testing.T) {
	h := NewIntegrations(nil, nil, "local", nil)
	r := chi.NewRouter()
	r.Get("/api/v1/integrations", h.List)
	rec := doReq(r, http.MethodGet, "/api/v1/integrations")
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestIntegrations_OwnerGating(t *testing.T) {
	reg := integrations.NewRegistry()
	require.NoError(t, reg.Register(fakeIntegration{id: "elevenlabs"}))
	h := NewIntegrations(reg, &fakeCreds{data: map[string]map[string]string{}}, "local", nil)
	r := chi.NewRouter()
	r.Route("/api/v1/integrations", func(r chi.Router) {
		r.Use(apimw.RequireGlobalOwner)
		r.Get("/", h.List)
	})

	// Unauthenticated -> 401.
	rec := doReq(r, http.MethodGet, "/api/v1/integrations/")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// Authenticated non-owner -> 403.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/", nil)
	req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleViewer}))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req)
	assert.Equal(t, http.StatusForbidden, rec2.Code)

	// Owner -> 200.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/integrations/", nil)
	req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleOwner}))
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req)
	assert.Equal(t, http.StatusOK, rec3.Code)
}

func listIntegrations(t *testing.T, reg *integrations.Registry, creds IntegrationCredentials) map[string]integrationView {
	t.Helper()
	h := NewIntegrations(reg, creds, "local", nil)
	r := chi.NewRouter()
	r.Get("/api/v1/integrations", h.List)
	rec := doReq(r, http.MethodGet, "/api/v1/integrations")
	require.Equal(t, http.StatusOK, rec.Code)
	var list []integrationView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	out := map[string]integrationView{}
	for _, v := range list {
		out[v.ID] = v
	}
	return out
}

func TestIntegrations_ConnectedReflectsProber(t *testing.T) {
	reg := integrations.NewRegistry()
	require.NoError(t, reg.Register(builtins.AIBackend(nil, func(context.Context, string) bool { return true })))
	require.NoError(t, reg.Register(builtins.ElevenLabs(nil, func(context.Context, string) bool { return false })))

	views := listIntegrations(t, reg, &fakeCreds{data: map[string]map[string]string{}})

	assert.True(t, views["ai-backend"].Connected, "ai-backend connected reflects its aiconfig probe, not stored creds")
	assert.Equal(t, "apikey", views["ai-backend"].AuthKind)
	assert.Equal(t, "/ai-mod/settings", views["ai-backend"].SetupHref)

	assert.False(t, views["elevenlabs"].Connected, "elevenlabs connected reflects its probe")
	assert.Equal(t, "apikey", views["elevenlabs"].AuthKind)
	assert.Equal(t, "/tts", views["elevenlabs"].SetupHref)
}

func TestIntegrations_ProberOverridesStoredKeys(t *testing.T) {
	reg := integrations.NewRegistry()
	require.NoError(t, reg.Register(builtins.AIBackend(nil, func(context.Context, string) bool { return false })))

	// A stored credential is present, but the prober (false) is authoritative.
	views := listIntegrations(t, reg, &fakeCreds{data: map[string]map[string]string{"ai-backend": {"api_key": "x"}}})

	assert.False(t, views["ai-backend"].Connected, "prober overrides credstore key presence")
	assert.Equal(t, []string{"api_key"}, views["ai-backend"].CredentialKeys, "keys are still listed by name")
}

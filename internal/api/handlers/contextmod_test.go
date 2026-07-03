package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/contextmod"
)

func newContextmodStore(t *testing.T) contextmod.Store {
	t.Helper()
	dsn := "file:cmodhandler-" + time.Now().Format("150405.000000000") + "?mode=memory&cache=shared"
	s, err := contextmod.OpenSQLiteStore(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newContextmodHandler(t *testing.T, store contextmod.Store) *handlers.ContextMod {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return handlers.NewContextMod(store, testTenant, logger)
}

func TestContextMod_GetDefault_DisabledUnknownChannel(t *testing.T) {
	h := newContextmodHandler(t, newContextmodStore(t))
	rr := httptest.NewRecorder()
	h.Get(rr, httptest.NewRequest(http.MethodGet, "/api/v1/contextmod?channel=engelswtf", nil))
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		Channel string `json:"channel"`
		Enabled bool   `json:"enabled"`
		Rules   string `json:"rules"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, "engelswtf", body.Channel)
	require.False(t, body.Enabled)
	require.Empty(t, body.Rules)
}

func TestContextMod_SetThenGetRoundtrip(t *testing.T) {
	h := newContextmodHandler(t, newContextmodStore(t))
	put := httptest.NewRequest(http.MethodPut, "/api/v1/contextmod",
		strings.NewReader(`{"channel":"engelswtf","enabled":true,"rules":"No slurs or threats."}`))
	putRR := httptest.NewRecorder()
	h.Set(putRR, put)
	require.Equal(t, http.StatusOK, putRR.Code)

	getRR := httptest.NewRecorder()
	h.Get(getRR, httptest.NewRequest(http.MethodGet, "/api/v1/contextmod?channel=engelswtf", nil))
	require.Equal(t, http.StatusOK, getRR.Code)

	var body struct {
		Enabled bool   `json:"enabled"`
		Rules   string `json:"rules"`
	}
	require.NoError(t, json.Unmarshal(getRR.Body.Bytes(), &body))
	require.True(t, body.Enabled)
	require.Equal(t, "No slurs or threats.", body.Rules)
}

func TestContextMod_SetPartialKeepsExisting(t *testing.T) {
	h := newContextmodHandler(t, newContextmodStore(t))
	h.Set(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/v1/contextmod",
		strings.NewReader(`{"channel":"ch","enabled":true,"rules":"keep me"}`)))

	// Only flip enabled; rules must persist.
	rr := httptest.NewRecorder()
	h.Set(rr, httptest.NewRequest(http.MethodPut, "/api/v1/contextmod",
		strings.NewReader(`{"channel":"ch","enabled":false}`)))
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		Enabled bool   `json:"enabled"`
		Rules   string `json:"rules"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.False(t, body.Enabled)
	require.Equal(t, "keep me", body.Rules, "omitted field must keep its value")
}

func TestContextMod_SetRejectsMissingChannel(t *testing.T) {
	h := newContextmodHandler(t, newContextmodStore(t))
	rr := httptest.NewRecorder()
	h.Set(rr, httptest.NewRequest(http.MethodPut, "/api/v1/contextmod", strings.NewReader(`{"enabled":true}`)))
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestContextMod_NilStoreReturns501(t *testing.T) {
	h := newContextmodHandler(t, nil)
	rr := httptest.NewRecorder()
	h.Get(rr, httptest.NewRequest(http.MethodGet, "/api/v1/contextmod?channel=x", nil))
	require.Equal(t, http.StatusNotImplemented, rr.Code)
}

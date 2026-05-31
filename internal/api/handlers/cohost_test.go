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
	"github.com/Luca-Pelzer/engelos/internal/cohost"
)

func newCohostStore(t *testing.T) cohost.Store {
	t.Helper()
	dsn := "file:cohosthandler-" + time.Now().Format("150405.000000000") + "?mode=memory&cache=shared"
	s, err := cohost.OpenSQLiteStore(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newCohostHandler(t *testing.T, store cohost.Store) *handlers.CoHost {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return handlers.NewCoHost(store, testTenant, logger)
}

func TestCoHost_GetDefault_DisabledUnknownChannel(t *testing.T) {
	h := newCohostHandler(t, newCohostStore(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cohost?channel=engelswtf", nil)
	rr := httptest.NewRecorder()
	h.Get(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		Channel string `json:"channel"`
		Enabled bool   `json:"enabled"`
		BotName string `json:"bot_name"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, "engelswtf", body.Channel)
	require.False(t, body.Enabled)
}

func TestCoHost_SetThenGetRoundtrip(t *testing.T) {
	h := newCohostHandler(t, newCohostStore(t))
	put := httptest.NewRequest(http.MethodPut, "/api/v1/cohost",
		strings.NewReader(`{"channel":"engelswtf","enabled":true,"bot_name":"engel","persona":"a witty co-host","max_reply_len":200}`))
	putRR := httptest.NewRecorder()
	h.Set(putRR, put)
	require.Equal(t, http.StatusOK, putRR.Code)

	get := httptest.NewRequest(http.MethodGet, "/api/v1/cohost?channel=engelswtf", nil)
	getRR := httptest.NewRecorder()
	h.Get(getRR, get)
	require.Equal(t, http.StatusOK, getRR.Code)

	var body struct {
		Enabled     bool   `json:"enabled"`
		BotName     string `json:"bot_name"`
		Persona     string `json:"persona"`
		MaxReplyLen int    `json:"max_reply_len"`
	}
	require.NoError(t, json.Unmarshal(getRR.Body.Bytes(), &body))
	require.True(t, body.Enabled)
	require.Equal(t, "engel", body.BotName)
	require.Equal(t, "a witty co-host", body.Persona)
	require.Equal(t, 200, body.MaxReplyLen)
}

func TestCoHost_SetPartialKeepsExisting(t *testing.T) {
	h := newCohostHandler(t, newCohostStore(t))
	first := httptest.NewRequest(http.MethodPut, "/api/v1/cohost",
		strings.NewReader(`{"channel":"ch","enabled":true,"bot_name":"alpha","persona":"witty"}`))
	h.Set(httptest.NewRecorder(), first)

	// Only flip enabled; bot_name and persona must persist.
	second := httptest.NewRequest(http.MethodPut, "/api/v1/cohost",
		strings.NewReader(`{"channel":"ch","enabled":false}`))
	secondRR := httptest.NewRecorder()
	h.Set(secondRR, second)
	require.Equal(t, http.StatusOK, secondRR.Code)

	var body struct {
		Enabled bool   `json:"enabled"`
		BotName string `json:"bot_name"`
		Persona string `json:"persona"`
	}
	require.NoError(t, json.Unmarshal(secondRR.Body.Bytes(), &body))
	require.False(t, body.Enabled)
	require.Equal(t, "alpha", body.BotName, "omitted field must keep its value")
	require.Equal(t, "witty", body.Persona)
}

func TestCoHost_SetRejectsMissingChannel(t *testing.T) {
	h := newCohostHandler(t, newCohostStore(t))
	put := httptest.NewRequest(http.MethodPut, "/api/v1/cohost", strings.NewReader(`{"enabled":true}`))
	rr := httptest.NewRecorder()
	h.Set(rr, put)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCoHost_GetListsConfigured(t *testing.T) {
	h := newCohostHandler(t, newCohostStore(t))
	for _, ch := range []string{"alpha", "beta"} {
		h.Set(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/v1/cohost",
			strings.NewReader(`{"channel":"`+ch+`","enabled":true}`)))
	}
	rr := httptest.NewRecorder()
	h.Get(rr, httptest.NewRequest(http.MethodGet, "/api/v1/cohost", nil))
	require.Equal(t, http.StatusOK, rr.Code)

	var body struct {
		Configs []map[string]any `json:"configs"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Configs, 2)
}

func TestCoHost_NilStoreReturns501(t *testing.T) {
	h := newCohostHandler(t, nil)
	rr := httptest.NewRecorder()
	h.Get(rr, httptest.NewRequest(http.MethodGet, "/api/v1/cohost?channel=x", nil))
	require.Equal(t, http.StatusNotImplemented, rr.Code)
}

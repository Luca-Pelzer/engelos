package handlers_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
)

type fakeStatsProvider struct{ payload any }

func (f fakeStatsProvider) Snapshot() any { return f.payload }

func TestStats_Get_WithProvider(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	prov := fakeStatsProvider{payload: map[string]int{"messages": 42}}
	h := handlers.NewStats(handlers.Version{Version: "1.2.3", Phase: "1B"}, prov, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/stats", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	res := rr.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.Equal(t, "1.2.3", got["version"])
	assert.Equal(t, "1B", got["phase"])
	disp, ok := got["dispatcher"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 42, disp["messages"])
}

func TestStats_Get_WithoutProvider(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.NewStats(handlers.Version{Version: "1.2.3", Phase: "1B"}, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	rr := httptest.NewRecorder()
	h.Get(rr, req)

	res := rr.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.Equal(t, "1.2.3", got["version"])
	_, hasDisp := got["dispatcher"]
	assert.False(t, hasDisp, "dispatcher omitted when provider nil")
}

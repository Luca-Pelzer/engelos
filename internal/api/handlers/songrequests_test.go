package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/songrequests"
)

func newSRStore(t *testing.T) songrequests.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:srhandler-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := songrequests.OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newSRHandler(t *testing.T, store songrequests.Store) *SongRequests {
	t.Helper()
	return NewSongRequests(store, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestSongRequests_SetThenGet(t *testing.T) {
	h := newSRHandler(t, newSRStore(t))

	body := `{"channel":"#EngelsWTF","provider":"spotify","spotify_playlist_id":"PL123","max_duration_sec":360,"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/songrequests", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Set(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var saved map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &saved))
	assert.Equal(t, "engelswtf", saved["channel"]) // normalized
	assert.Equal(t, "spotify", saved["provider"])
	assert.Equal(t, "PL123", saved["spotify_playlist_id"])
	assert.Equal(t, float64(360), saved["max_duration_sec"])
	assert.Equal(t, true, saved["enabled"])

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/songrequests?channel=engelswtf", nil)
	getRec := httptest.NewRecorder()
	h.Get(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	assert.Equal(t, "spotify", got["provider"])
	assert.Equal(t, "PL123", got["spotify_playlist_id"])
}

func TestSongRequests_GetDefaultWhenMissing(t *testing.T) {
	h := newSRHandler(t, newSRStore(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/songrequests?channel=nobody", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "", got["provider"])
	assert.Equal(t, false, got["enabled"])
}

func TestSongRequests_List(t *testing.T) {
	store := newSRStore(t)
	h := newSRHandler(t, store)
	for _, ch := range []string{"alpha", "bravo"} {
		_, err := store.Set(context.Background(), songrequests.Config{
			TenantID: "local", Channel: ch, Provider: "spotify", Enabled: true,
		})
		require.NoError(t, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/songrequests", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Configs []map[string]any `json:"configs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Len(t, got.Configs, 2)
}

func TestSongRequests_SetInvalidProvider(t *testing.T) {
	h := newSRHandler(t, newSRStore(t))
	body := `{"channel":"x","provider":"soundcloud","enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/songrequests", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Set(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSongRequests_SetMissingChannel(t *testing.T) {
	h := newSRHandler(t, newSRStore(t))
	body := `{"provider":"spotify","enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/songrequests", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Set(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSongRequests_Disabled(t *testing.T) {
	h := NewSongRequests(nil, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/songrequests?channel=x", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

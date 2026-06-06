package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
)

const pityTenant = "local"

func newPityHandler(t *testing.T) (*handlers.Pity, *pity.System) {
	t.Helper()
	dsn := fmt.Sprintf("file:pityapi-%d?mode=memory&cache=shared", time.Now().UnixNano())
	store, err := eventsourcing.OpenSQLite(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := pity.DefaultConfig()
	cfg.HardPityThreshold = 5
	cfg.SoftPityFraction = 0.6
	cfg.PointsPerMessage = 1
	cfg.MaxPointsPerWindow = 100

	sys, err := pity.New(cfg, store, logger)
	require.NoError(t, err)
	return handlers.NewPity(sys, pityTenant, logger), sys
}

func postJSON(t *testing.T, h http.Handler, path string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Result()
}

func TestPity_Grant_Happy(t *testing.T) {
	t.Parallel()
	h, _ := newPityHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/grant", h.Grant)

	res := postJSON(t, mux, "/api/v1/pity/grant", map[string]any{
		"channel":   "engelswtf",
		"viewer_id": "viewer-1",
		"username":  "alice",
		"amount":    3,
		"reason":    "test",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.EqualValues(t, 3, got["granted"])
	assert.EqualValues(t, 3, got["total"])
}

func TestPity_Grant_DefaultsAmountFromConfig(t *testing.T) {
	t.Parallel()
	h, _ := newPityHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/grant", h.Grant)

	res := postJSON(t, mux, "/api/v1/pity/grant", map[string]any{
		"channel":   "engelswtf",
		"viewer_id": "viewer-default",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)
	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.EqualValues(t, 1, got["granted"])
}

func TestPity_Grant_ValidationErrors(t *testing.T) {
	t.Parallel()
	h, _ := newPityHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/grant", h.Grant)

	cases := []struct {
		name string
		body any
	}{
		{"missing channel", map[string]any{"viewer_id": "v1"}},
		{"missing viewer_id", map[string]any{"channel": "engelswtf"}},
		{"empty body", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := postJSON(t, mux, "/api/v1/pity/grant", tc.body)
			assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		})
	}
}

func TestPity_Roll_Lose(t *testing.T) {
	t.Parallel()
	h, sys := newPityHandler(t)
	sys.WithSeed(42)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/roll", h.Roll)

	res := postJSON(t, mux, "/api/v1/pity/roll", map[string]any{
		"channel":   "engelswtf",
		"viewer_id": "loser",
		"username":  "loser",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)
	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	_, hasWon := got["won"]
	assert.True(t, hasWon)
	assert.Contains(t, got, "effective_chance")
}

func TestPity_Roll_HardPityGuaranteesWin(t *testing.T) {
	t.Parallel()
	h, sys := newPityHandler(t)

	for i := 0; i < 5; i++ {
		_, err := sys.GrantPoints(context.Background(), pityTenant,
			"engelswtf", "lucky", "lucky", "preseed", 1)
		require.NoError(t, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/roll", h.Roll)

	res := postJSON(t, mux, "/api/v1/pity/roll", map[string]any{
		"channel":   "engelswtf",
		"viewer_id": "lucky",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)
	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.True(t, got["won"].(bool), "hard-pity must guarantee a win")
	assert.True(t, got["was_guaranteed"].(bool))
	assert.EqualValues(t, 0, got["points_after"], "win resets points")
}

func TestPity_Status_Query(t *testing.T) {
	t.Parallel()
	h, sys := newPityHandler(t)

	_, err := sys.GrantPoints(context.Background(), pityTenant,
		"engelswtf", "viewer-status", "v", "preseed", 4)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/status", h.Status)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/pity/status?channel=engelswtf&viewer_id=viewer-status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	res := rr.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.EqualValues(t, 4, got["points"])
	assert.True(t, got["soft_pity_hit"].(bool), "4 of 5 is past soft-pity")
	assert.EqualValues(t, 5, got["hard_pity_threshold"])
}

func TestPity_Status_MissingParams(t *testing.T) {
	t.Parallel()
	h, _ := newPityHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pity/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Result().StatusCode)
}

func TestPity_Reset_ClearsPoints(t *testing.T) {
	t.Parallel()
	h, sys := newPityHandler(t)

	_, err := sys.GrantPoints(context.Background(), pityTenant,
		"engelswtf", "viewer-reset", "v", "preseed", 3)
	require.NoError(t, err)
	require.Equal(t, 3, sys.Status(pityTenant, "engelswtf", "viewer-reset").Points)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/reset", h.Reset)

	res := postJSON(t, mux, "/api/v1/pity/reset", map[string]any{
		"channel":   "engelswtf",
		"viewer_id": "viewer-reset",
		"reason":    "test-cleanup",
	})
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	assert.Equal(t, 0, sys.Status(pityTenant, "engelswtf", "viewer-reset").Points)
}

func TestPity_Leaderboard_DefaultLimit(t *testing.T) {
	t.Parallel()
	h, sys := newPityHandler(t)

	for i := 0; i < 3; i++ {
		_, err := sys.GrantPoints(context.Background(), pityTenant,
			"engelswtf", fmt.Sprintf("viewer-%d", i), fmt.Sprintf("v%d", i),
			"preseed", i+1)
		require.NoError(t, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/leaderboard", h.Leaderboard)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/pity/leaderboard?channel=engelswtf", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	res := rr.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got struct {
		Channel string           `json:"channel"`
		Limit   int              `json:"limit"`
		Entries []map[string]any `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.Equal(t, "engelswtf", got.Channel)
	assert.Equal(t, 10, got.Limit)
	require.Len(t, got.Entries, 3)
	assert.EqualValues(t, 3, got.Entries[0]["points"], "top entry has highest points")
	assert.Equal(t, "viewer-2", got.Entries[0]["viewer_id"])
}

func TestPity_Leaderboard_BadLimit(t *testing.T) {
	t.Parallel()
	h, _ := newPityHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/leaderboard", h.Leaderboard)

	for _, bad := range []string{"0", "-1", "abc", "101"} {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/pity/leaderboard?limit="+bad, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Result().StatusCode, "limit=%s", bad)
	}
}

func TestPity_DisabledWhenNilSystem(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.NewPity(nil, pityTenant, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pity/grant", h.Grant)
	mux.HandleFunc("/api/v1/pity/roll", h.Roll)
	mux.HandleFunc("/api/v1/pity/status", h.Status)
	mux.HandleFunc("/api/v1/pity/leaderboard", h.Leaderboard)
	mux.HandleFunc("/api/v1/pity/reset", h.Reset)

	for _, p := range []string{"/api/v1/pity/grant", "/api/v1/pity/roll", "/api/v1/pity/reset"} {
		res := postJSON(t, mux, p, map[string]any{"channel": "c", "viewer_id": "v"})
		assert.Equal(t, http.StatusNotImplemented, res.StatusCode, p)
	}
	for _, p := range []string{"/api/v1/pity/status?channel=c&viewer_id=v", "/api/v1/pity/leaderboard"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotImplemented, rr.Result().StatusCode, p)
	}
}

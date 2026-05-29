package handlers_test

import (
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
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
)

const streakTenant = "local"

func newStreakHandler(t *testing.T) (*handlers.Streak, *streak.System) {
	t.Helper()
	dsn := fmt.Sprintf("file:streakapi-%d?mode=memory&cache=shared", time.Now().UnixNano())
	store, err := eventsourcing.OpenSQLite(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := streak.DefaultConfig()
	cfg.Logger = logger

	sys, err := streak.New(cfg, store, logger)
	require.NoError(t, err)
	return handlers.NewStreak(sys, streakTenant, logger), sys
}

func TestStreak_Tick_FirstActivity(t *testing.T) {
	t.Parallel()
	h, _ := newStreakHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/streak/tick", h.Tick)

	res := postJSON(t, mux, "/api/v1/streak/tick", map[string]any{
		"channel":   "engelswtf",
		"viewer_id": "alice",
		"username":  "alice",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.EqualValues(t, 1, got["days_current"])
	assert.EqualValues(t, 1, got["days_longest"])
	assert.Equal(t, false, got["same_day_retick"])
}

func TestStreak_Tick_ValidationErrors(t *testing.T) {
	t.Parallel()
	h, _ := newStreakHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/streak/tick", h.Tick)

	cases := []struct {
		name string
		body any
	}{
		{"missing channel", map[string]any{"viewer_id": "v1"}},
		{"missing viewer_id", map[string]any{"channel": "c"}},
		{"empty body", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := postJSON(t, mux, "/api/v1/streak/tick", tc.body)
			assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		})
	}
}

func TestStreak_Status_Query(t *testing.T) {
	t.Parallel()
	h, sys := newStreakHandler(t)

	_, err := sys.Tick(context.Background(), streakTenant,
		"engelswtf", "viewer-1", "v1")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/streak/status", h.Status)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/streak/status?channel=engelswtf&viewer_id=viewer-1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	res := rr.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.EqualValues(t, 1, got["days_current"])
}

func TestStreak_Status_MissingParams(t *testing.T) {
	t.Parallel()
	h, _ := newStreakHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/streak/status", h.Status)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/streak/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Result().StatusCode)
}

func TestStreak_Leaderboard_DefaultLimit(t *testing.T) {
	t.Parallel()
	h, sys := newStreakHandler(t)

	for i := 0; i < 3; i++ {
		_, err := sys.Tick(context.Background(), streakTenant,
			"engelswtf", fmt.Sprintf("viewer-%d", i), fmt.Sprintf("v%d", i))
		require.NoError(t, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/streak/leaderboard", h.Leaderboard)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/streak/leaderboard?channel=engelswtf", nil)
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
	assert.Len(t, got.Entries, 3)
}

func TestStreak_Leaderboard_BadLimit(t *testing.T) {
	t.Parallel()
	h, _ := newStreakHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/streak/leaderboard", h.Leaderboard)

	for _, bad := range []string{"0", "-1", "abc", "101"} {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/streak/leaderboard?limit="+bad, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Result().StatusCode, "limit=%s", bad)
	}
}

func TestStreak_Reset_ClearsState(t *testing.T) {
	t.Parallel()
	h, sys := newStreakHandler(t)

	_, err := sys.Tick(context.Background(), streakTenant,
		"engelswtf", "to-reset", "tr")
	require.NoError(t, err)
	require.Equal(t, 1, sys.Status(streakTenant, "engelswtf", "to-reset").DaysCurrent)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/streak/reset", h.Reset)

	res := postJSON(t, mux, "/api/v1/streak/reset", map[string]any{
		"channel":   "engelswtf",
		"viewer_id": "to-reset",
		"reason":    "test-cleanup",
	})
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	assert.Equal(t, 0, sys.Status(streakTenant, "engelswtf", "to-reset").DaysCurrent)
}

func TestStreak_DisabledWhenNilSystem(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.NewStreak(nil, streakTenant, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/streak/tick", h.Tick)
	mux.HandleFunc("/api/v1/streak/freeze", h.UseFreeze)
	mux.HandleFunc("/api/v1/streak/status", h.Status)
	mux.HandleFunc("/api/v1/streak/leaderboard", h.Leaderboard)
	mux.HandleFunc("/api/v1/streak/reset", h.Reset)

	for _, p := range []string{"/api/v1/streak/tick", "/api/v1/streak/freeze", "/api/v1/streak/reset"} {
		res := postJSON(t, mux, p, map[string]any{"channel": "c", "viewer_id": "v"})
		assert.Equal(t, http.StatusNotImplemented, res.StatusCode, p)
	}
	for _, p := range []string{"/api/v1/streak/status?channel=c&viewer_id=v", "/api/v1/streak/leaderboard"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotImplemented, rr.Result().StatusCode, p)
	}
}

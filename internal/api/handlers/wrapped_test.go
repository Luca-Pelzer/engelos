package handlers

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

	"github.com/Luca-Pelzer/engelos/internal/wrapped"
)

func newWrappedStore(t *testing.T) wrapped.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:wrappedh-%d?mode=memory&cache=shared", time.Now().UnixNano())
	s, err := wrapped.OpenSQLiteStore(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

type stubRanker struct {
	points  int64
	longest int
}

func (s stubRanker) Points(context.Context, string, string) int64      { return s.points }
func (s stubRanker) LongestStreak(context.Context, string, string) int { return s.longest }

func newWrappedHandler(t *testing.T, store wrapped.Store, ranker WrappedRanker) *Wrapped {
	t.Helper()
	return NewWrapped(store, ranker, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestWrapped_Disabled(t *testing.T) {
	h := NewWrapped(nil, nil, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/wrapped?channel=x", nil))
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestWrapped_MissingChannel(t *testing.T) {
	h := newWrappedHandler(t, newWrappedStore(t), nil)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/wrapped", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWrapped_ViewerCard(t *testing.T) {
	store := newWrappedStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		require.NoError(t, store.IncrementMessage(ctx, "local", "engelswtf", "v1", "Alice", "all"))
	}
	require.NoError(t, store.IncrementMessage(ctx, "local", "engelswtf", "v2", "Bob", "all"))

	h := newWrappedHandler(t, store, stubRanker{points: 1234, longest: 9})
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/wrapped?channel=engelswtf&viewer=v1", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var d map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &d))
	assert.Equal(t, "viewer", d["kind"])
	assert.Equal(t, "Alice", d["username"])
	assert.Equal(t, float64(5), d["messages"])
	assert.Equal(t, float64(1), d["rank"]) // most messages
	assert.Equal(t, float64(1234), d["points"])
	assert.Equal(t, float64(9), d["longest_streak"])
}

func TestWrapped_ViewerNotFound(t *testing.T) {
	h := newWrappedHandler(t, newWrappedStore(t), nil)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/wrapped?channel=engelswtf&viewer=ghost", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWrapped_ChannelCard(t *testing.T) {
	store := newWrappedStore(t)
	ctx := context.Background()
	require.NoError(t, store.IncrementMessage(ctx, "local", "engelswtf", "v1", "Alice", "all"))
	require.NoError(t, store.IncrementMessage(ctx, "local", "engelswtf", "v2", "Bob", "all"))
	require.NoError(t, store.IncrementSub(ctx, "local", "engelswtf", "v2", "Bob", "all"))

	h := newWrappedHandler(t, store, nil)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/wrapped?channel=engelswtf", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var d map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &d))
	assert.Equal(t, "channel", d["kind"])
	assert.Equal(t, float64(2), d["total_messages"])
	assert.Equal(t, float64(1), d["total_subs"])
	assert.Equal(t, float64(2), d["total_viewers"])
	top, ok := d["top_chatters"].([]any)
	require.True(t, ok)
	assert.Len(t, top, 2)
}

func TestWrapped_InvalidPeriod(t *testing.T) {
	h := newWrappedHandler(t, newWrappedStore(t), nil)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/wrapped?channel=engelswtf&viewer=v1&period=badperiod", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

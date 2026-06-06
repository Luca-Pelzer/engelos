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

	"github.com/Luca-Pelzer/engelos/internal/songrequests/queue"
)

func newQueueStore(t *testing.T) queue.Store {
	t.Helper()
	dsn := fmt.Sprintf("file:sqhandler-%d?mode=memory&cache=shared", time.Now().UnixNano())
	s, err := queue.OpenSQLiteStore(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newSongQueueHandler(t *testing.T, store queue.Store) *SongQueue {
	t.Helper()
	return NewSongQueue(store, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestSongQueue_NextEmptyReturns204(t *testing.T) {
	h := newSongQueueHandler(t, newQueueStore(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/songqueue/next?channel=engelswtf", nil)
	rec := httptest.NewRecorder()
	h.Next(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestSongQueue_NextReturnsAndAdvances(t *testing.T) {
	store := newQueueStore(t)
	h := newSongQueueHandler(t, store)
	for _, v := range []struct{ id, title string }{{"aaaaaaaaaaa", "First"}, {"bbbbbbbbbbb", "Second"}} {
		_, err := store.Enqueue(context.Background(), queue.Item{
			TenantID: "local", Channel: "engelswtf", VideoID: v.id, Title: v.title,
		})
		require.NoError(t, err)
	}

	// First call: no current yet, promotes "First".
	rec := httptest.NewRecorder()
	h.Next(rec, httptest.NewRequest(http.MethodGet, "/api/v1/songqueue/next?channel=engelswtf", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var first map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	assert.Equal(t, "aaaaaaaaaaa", first["video_id"])
	assert.Equal(t, "First", first["title"])

	// Second call: retires "First" (marks played), promotes "Second".
	rec2 := httptest.NewRecorder()
	h.Next(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/songqueue/next?channel=engelswtf", nil))
	require.Equal(t, http.StatusOK, rec2.Code)
	var second map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &second))
	assert.Equal(t, "bbbbbbbbbbb", second["video_id"])

	// Third call: queue exhausted -> 204.
	rec3 := httptest.NewRecorder()
	h.Next(rec3, httptest.NewRequest(http.MethodGet, "/api/v1/songqueue/next?channel=engelswtf", nil))
	assert.Equal(t, http.StatusNoContent, rec3.Code)
}

func TestSongQueue_MissingChannel(t *testing.T) {
	h := newSongQueueHandler(t, newQueueStore(t))
	rec := httptest.NewRecorder()
	h.Next(rec, httptest.NewRequest(http.MethodGet, "/api/v1/songqueue/next", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSongQueue_Disabled(t *testing.T) {
	h := NewSongQueue(nil, "local", slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.Next(rec, httptest.NewRequest(http.MethodGet, "/api/v1/songqueue/next?channel=x", nil))
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

package handlers_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/counters"
)

const countersTenant = "local"

func newCountersHandler(t *testing.T) (*handlers.Counters, counters.Store) {
	t.Helper()
	dsn := fmt.Sprintf("file:countersapi-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := counters.OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return handlers.NewCounters(store, countersTenant, logger), store
}

func countersRouter(h *handlers.Counters) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1/counters", func(r chi.Router) {
		r.Get("/", h.List)
		r.Put("/{name}", h.Set)
		r.Post("/{name}/add", h.Add)
		r.Delete("/{name}", h.Delete)
	})
	return r
}

func TestCounters_DisabledWhenNilStore(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.NewCounters(nil, countersTenant, logger)
	router := countersRouter(h)

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/counters?channel=c", nil},
		{http.MethodPut, "/api/v1/counters/deaths", map[string]any{"channel": "c", "value": 1}},
		{http.MethodPost, "/api/v1/counters/deaths/add", map[string]any{"channel": "c", "delta": 1}},
		{http.MethodDelete, "/api/v1/counters/deaths?channel=c", nil},
	}
	for _, tc := range cases {
		res := doJSON(t, router, tc.method, tc.path, tc.body)
		assert.Equal(t, http.StatusNotImplemented, res.StatusCode, "%s %s", tc.method, tc.path)
	}
}

func TestCounters_Set_CreatesAndUpdates(t *testing.T) {
	t.Parallel()
	h, _ := newCountersHandler(t)
	router := countersRouter(h)

	create := doJSON(t, router, http.MethodPut, "/api/v1/counters/deaths", map[string]any{
		"channel": "engelswtf",
		"value":   7,
	})
	require.Equal(t, http.StatusOK, create.StatusCode)
	got := decodeBody(t, create)
	assert.Equal(t, "deaths", got["name"])
	assert.Equal(t, float64(7), got["value"])
	assert.NotEmpty(t, got["id"])
	assert.NotEmpty(t, got["updated_at"])

	listRes := doJSON(t, router, http.MethodGet, "/api/v1/counters?channel=engelswtf", nil)
	require.Equal(t, http.StatusOK, listRes.StatusCode)
	listed := decodeBody(t, listRes)
	list, ok := listed["counters"].([]any)
	require.True(t, ok)
	require.Len(t, list, 1)

	update := doJSON(t, router, http.MethodPut, "/api/v1/counters/deaths", map[string]any{
		"channel": "engelswtf",
		"value":   42,
	})
	require.Equal(t, http.StatusOK, update.StatusCode)
	assert.Equal(t, float64(42), decodeBody(t, update)["value"])
}

func TestCounters_Add_IncrementsAndDecrements(t *testing.T) {
	t.Parallel()
	h, _ := newCountersHandler(t)
	router := countersRouter(h)

	first := doJSON(t, router, http.MethodPost, "/api/v1/counters/hugs/add", map[string]any{
		"channel": "engelswtf",
		"delta":   3,
	})
	require.Equal(t, http.StatusOK, first.StatusCode)
	assert.Equal(t, float64(3), decodeBody(t, first)["value"], "Add upserts at delta from 0")

	second := doJSON(t, router, http.MethodPost, "/api/v1/counters/hugs/add", map[string]any{
		"channel": "engelswtf",
		"delta":   5,
	})
	require.Equal(t, http.StatusOK, second.StatusCode)
	assert.Equal(t, float64(8), decodeBody(t, second)["value"], "Add returns the new running total")

	down := doJSON(t, router, http.MethodPost, "/api/v1/counters/hugs/add", map[string]any{
		"channel": "engelswtf",
		"delta":   -2,
	})
	require.Equal(t, http.StatusOK, down.StatusCode)
	assert.Equal(t, float64(6), decodeBody(t, down)["value"], "negative delta decrements")
}

func TestCounters_Set_MissingValue(t *testing.T) {
	t.Parallel()
	h, _ := newCountersHandler(t)
	router := countersRouter(h)

	res := doJSON(t, router, http.MethodPut, "/api/v1/counters/deaths", map[string]any{
		"channel": "engelswtf",
	})
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestCounters_Add_MissingDelta(t *testing.T) {
	t.Parallel()
	h, _ := newCountersHandler(t)
	router := countersRouter(h)

	res := doJSON(t, router, http.MethodPost, "/api/v1/counters/deaths/add", map[string]any{
		"channel": "engelswtf",
	})
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func TestCounters_MissingChannel(t *testing.T) {
	t.Parallel()
	h, _ := newCountersHandler(t)
	router := countersRouter(h)

	list := doJSON(t, router, http.MethodGet, "/api/v1/counters", nil)
	assert.Equal(t, http.StatusBadRequest, list.StatusCode)

	set := doJSON(t, router, http.MethodPut, "/api/v1/counters/deaths", map[string]any{"value": 1})
	assert.Equal(t, http.StatusBadRequest, set.StatusCode)

	add := doJSON(t, router, http.MethodPost, "/api/v1/counters/deaths/add", map[string]any{"delta": 1})
	assert.Equal(t, http.StatusBadRequest, add.StatusCode)

	del := doJSON(t, router, http.MethodDelete, "/api/v1/counters/deaths", nil)
	assert.Equal(t, http.StatusBadRequest, del.StatusCode)
}

func TestCounters_Delete(t *testing.T) {
	t.Parallel()
	h, _ := newCountersHandler(t)
	router := countersRouter(h)

	create := doJSON(t, router, http.MethodPut, "/api/v1/counters/gone", map[string]any{
		"channel": "engelswtf",
		"value":   1,
	})
	require.Equal(t, http.StatusOK, create.StatusCode)

	del := doJSON(t, router, http.MethodDelete, "/api/v1/counters/gone?channel=engelswtf", nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)

	listRes := doJSON(t, router, http.MethodGet, "/api/v1/counters?channel=engelswtf", nil)
	listed := decodeBody(t, listRes)
	list := listed["counters"].([]any)
	assert.Len(t, list, 0)

	notFound := doJSON(t, router, http.MethodDelete, "/api/v1/counters/gone?channel=engelswtf", nil)
	assert.Equal(t, http.StatusNotFound, notFound.StatusCode)
}

func TestCounters_InvalidName(t *testing.T) {
	t.Parallel()
	h, _ := newCountersHandler(t)
	router := countersRouter(h)

	res := doJSON(t, router, http.MethodPut, "/api/v1/counters/Bad%20Name!", map[string]any{
		"channel": "engelswtf",
		"value":   1,
	})
	assert.Equal(t, http.StatusBadRequest, res.StatusCode, "store rejects names outside [a-z0-9_]+")
}

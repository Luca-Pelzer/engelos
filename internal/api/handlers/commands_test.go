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
	"github.com/Luca-Pelzer/engelos/internal/customcommands"
)

const commandsTenant = "local"

func newCommandsHandler(t *testing.T) (*handlers.Commands, customcommands.Store) {
	t.Helper()
	dsn := fmt.Sprintf("file:commandsapi-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := customcommands.OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return handlers.NewCommands(store, commandsTenant, logger), store
}

func commandsRouter(h *handlers.Commands) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1/commands", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Put("/{name}", h.Update)
		r.Delete("/{name}", h.Delete)
	})
	return r
}

func TestCommands_DisabledWhenNilStore(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.NewCommands(nil, commandsTenant, logger)
	router := commandsRouter(h)

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/commands?channel=c", nil},
		{http.MethodPost, "/api/v1/commands", map[string]any{"channel": "c", "name": "hi", "response": "yo"}},
		{http.MethodPut, "/api/v1/commands/hi", map[string]any{"channel": "c", "response": "yo"}},
		{http.MethodDelete, "/api/v1/commands/hi?channel=c", nil},
	}
	for _, tc := range cases {
		res := doJSON(t, router, tc.method, tc.path, tc.body)
		assert.Equal(t, http.StatusNotImplemented, res.StatusCode, "%s %s", tc.method, tc.path)
	}
}

func TestCommands_Create_Happy(t *testing.T) {
	t.Parallel()
	h, _ := newCommandsHandler(t)
	router := commandsRouter(h)

	res := doJSON(t, router, http.MethodPost, "/api/v1/commands", map[string]any{
		"channel":    "engelswtf",
		"name":       "hello",
		"response":   "Hi $user!",
		"created_by": "broadcaster-1",
	})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	got := decodeBody(t, res)
	assert.Equal(t, "hello", got["name"])
	assert.Equal(t, "Hi $user!", got["response"])
	assert.Equal(t, "everyone", got["min_role"], "min_role defaults to everyone when omitted")
	assert.Equal(t, "broadcaster-1", got["created_by"])
	assert.NotEmpty(t, got["id"])
	assert.NotEmpty(t, got["created_at"])

	listRes := doJSON(t, router, http.MethodGet, "/api/v1/commands?channel=engelswtf", nil)
	require.Equal(t, http.StatusOK, listRes.StatusCode)
	listed := decodeBody(t, listRes)
	commands, ok := listed["commands"].([]any)
	require.True(t, ok)
	require.Len(t, commands, 1)
}

func TestCommands_Create_Errors(t *testing.T) {
	t.Parallel()
	h, _ := newCommandsHandler(t)
	router := commandsRouter(h)

	missingChannel := doJSON(t, router, http.MethodPost, "/api/v1/commands", map[string]any{
		"name":     "hi",
		"response": "yo",
	})
	assert.Equal(t, http.StatusBadRequest, missingChannel.StatusCode)

	badRole := doJSON(t, router, http.MethodPost, "/api/v1/commands", map[string]any{
		"channel":  "engelswtf",
		"name":     "hi",
		"response": "yo",
		"min_role": "wizard",
	})
	assert.Equal(t, http.StatusBadRequest, badRole.StatusCode)

	first := doJSON(t, router, http.MethodPost, "/api/v1/commands", map[string]any{
		"channel":  "engelswtf",
		"name":     "dup",
		"response": "yo",
	})
	require.Equal(t, http.StatusCreated, first.StatusCode)
	dup := doJSON(t, router, http.MethodPost, "/api/v1/commands", map[string]any{
		"channel":  "engelswtf",
		"name":     "dup",
		"response": "yo again",
	})
	assert.Equal(t, http.StatusConflict, dup.StatusCode)
}

func TestCommands_List_MissingChannelAndEmpty(t *testing.T) {
	t.Parallel()
	h, _ := newCommandsHandler(t)
	router := commandsRouter(h)

	missing := doJSON(t, router, http.MethodGet, "/api/v1/commands", nil)
	assert.Equal(t, http.StatusBadRequest, missing.StatusCode)

	empty := doJSON(t, router, http.MethodGet, "/api/v1/commands?channel=engelswtf", nil)
	require.Equal(t, http.StatusOK, empty.StatusCode)
	got := decodeBody(t, empty)
	commands, ok := got["commands"].([]any)
	require.True(t, ok, "commands must be a JSON array, not null")
	assert.Len(t, commands, 0)
}

func TestCommands_Update(t *testing.T) {
	t.Parallel()
	h, _ := newCommandsHandler(t)
	router := commandsRouter(h)

	create := doJSON(t, router, http.MethodPost, "/api/v1/commands", map[string]any{
		"channel":  "engelswtf",
		"name":     "edit",
		"response": "old",
	})
	require.Equal(t, http.StatusCreated, create.StatusCode)

	upd := doJSON(t, router, http.MethodPut, "/api/v1/commands/edit", map[string]any{
		"channel":  "engelswtf",
		"response": "new",
		"min_role": "moderator",
	})
	require.Equal(t, http.StatusOK, upd.StatusCode)
	got := decodeBody(t, upd)
	assert.Equal(t, "new", got["response"])
	assert.Equal(t, "moderator", got["min_role"])

	missing := doJSON(t, router, http.MethodPut, "/api/v1/commands/ghost", map[string]any{
		"channel":  "engelswtf",
		"response": "nope",
	})
	assert.Equal(t, http.StatusNotFound, missing.StatusCode)
}

func TestCommands_Delete(t *testing.T) {
	t.Parallel()
	h, _ := newCommandsHandler(t)
	router := commandsRouter(h)

	create := doJSON(t, router, http.MethodPost, "/api/v1/commands", map[string]any{
		"channel":  "engelswtf",
		"name":     "gone",
		"response": "bye",
	})
	require.Equal(t, http.StatusCreated, create.StatusCode)

	del := doJSON(t, router, http.MethodDelete, "/api/v1/commands/gone?channel=engelswtf", nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)

	listRes := doJSON(t, router, http.MethodGet, "/api/v1/commands?channel=engelswtf", nil)
	listed := decodeBody(t, listRes)
	commands := listed["commands"].([]any)
	assert.Len(t, commands, 0)

	notFound := doJSON(t, router, http.MethodDelete, "/api/v1/commands/gone?channel=engelswtf", nil)
	assert.Equal(t, http.StatusNotFound, notFound.StatusCode)

	missingChannel := doJSON(t, router, http.MethodDelete, "/api/v1/commands/gone", nil)
	assert.Equal(t, http.StatusBadRequest, missingChannel.StatusCode)
}

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

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/redemptions"
)

const redemptionsTenant = "local"

func newRedemptionsHandler(t *testing.T) (*handlers.Redemptions, redemptions.Store) {
	t.Helper()
	dsn := fmt.Sprintf("file:redemptionsapi-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := redemptions.OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return handlers.NewRedemptions(store, redemptionsTenant, logger), store
}

// redemptionsRouter mounts the handler the same way router.go will, so chi URL
// params (rewardID) and query params resolve in tests.
func redemptionsRouter(h *handlers.Redemptions) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1/redemptions", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Put("/{rewardID}", h.Update)
		r.Post("/{rewardID}/enabled", h.SetEnabled)
		r.Delete("/{rewardID}", h.Delete)
	})
	return r
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Result()
}

func decodeBody(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	var got map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	return got
}

func TestRedemptions_DisabledWhenNilStore(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handlers.NewRedemptions(nil, redemptionsTenant, logger)
	router := redemptionsRouter(h)

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/redemptions?channel=c", nil},
		{http.MethodPost, "/api/v1/redemptions", map[string]any{"channel": "c", "reward_id": "r", "action_type": "none"}},
		{http.MethodPut, "/api/v1/redemptions/r", map[string]any{"channel": "c", "action_type": "none"}},
		{http.MethodPost, "/api/v1/redemptions/r/enabled", map[string]any{"channel": "c", "enabled": false}},
		{http.MethodDelete, "/api/v1/redemptions/r?channel=c", nil},
	}
	for _, tc := range cases {
		res := doJSON(t, router, tc.method, tc.path, tc.body)
		assert.Equal(t, http.StatusNotImplemented, res.StatusCode, "%s %s", tc.method, tc.path)
	}
}

func TestRedemptions_Create_Happy(t *testing.T) {
	t.Parallel()
	h, _ := newRedemptionsHandler(t)
	router := redemptionsRouter(h)

	res := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":      "engelswtf",
		"reward_id":    "reward-1",
		"reward_title": "Hydrate",
		"action_type":  redemptions.ActionChatMessage,
		"action_param": "$user redeemed $reward!",
	})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	got := decodeBody(t, res)
	assert.Equal(t, "reward-1", got["reward_id"])
	assert.Equal(t, "Hydrate", got["reward_title"])
	assert.Equal(t, redemptions.ActionChatMessage, got["action_type"])
	assert.Equal(t, "$user redeemed $reward!", got["action_param"])
	assert.Equal(t, true, got["enabled"], "enabled defaults true when omitted")
	assert.Equal(t, false, got["auto_fulfill"])
	assert.NotEmpty(t, got["created_at"])

	listRes := doJSON(t, router, http.MethodGet, "/api/v1/redemptions?channel=engelswtf", nil)
	require.Equal(t, http.StatusOK, listRes.StatusCode)
	listed := decodeBody(t, listRes)
	bindings, ok := listed["bindings"].([]any)
	require.True(t, ok)
	require.Len(t, bindings, 1)
}

func TestRedemptions_Create_ChannelNormalised(t *testing.T) {
	t.Parallel()
	h, _ := newRedemptionsHandler(t)
	router := redemptionsRouter(h)

	res := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":     "MyChannel",
		"reward_id":   "reward-x",
		"action_type": redemptions.ActionNone,
	})
	require.Equal(t, http.StatusCreated, res.StatusCode)

	listRes := doJSON(t, router, http.MethodGet, "/api/v1/redemptions?channel=mychannel", nil)
	require.Equal(t, http.StatusOK, listRes.StatusCode)
	listed := decodeBody(t, listRes)
	bindings, ok := listed["bindings"].([]any)
	require.True(t, ok)
	require.Len(t, bindings, 1, "binding created under MixedCase must list under lower-case")
}

func TestRedemptions_Create_Errors(t *testing.T) {
	t.Parallel()
	h, _ := newRedemptionsHandler(t)
	router := redemptionsRouter(h)

	missingChannel := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"reward_id":   "r1",
		"action_type": redemptions.ActionNone,
	})
	assert.Equal(t, http.StatusBadRequest, missingChannel.StatusCode)

	badAction := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":     "engelswtf",
		"reward_id":   "r1",
		"action_type": "not_a_real_action",
	})
	assert.Equal(t, http.StatusBadRequest, badAction.StatusCode)

	missingAction := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":   "engelswtf",
		"reward_id": "r1",
	})
	assert.Equal(t, http.StatusBadRequest, missingAction.StatusCode)

	first := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":     "engelswtf",
		"reward_id":   "dup",
		"action_type": redemptions.ActionNone,
	})
	require.Equal(t, http.StatusCreated, first.StatusCode)
	dup := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":     "engelswtf",
		"reward_id":   "dup",
		"action_type": redemptions.ActionNone,
	})
	assert.Equal(t, http.StatusConflict, dup.StatusCode)
}

func TestRedemptions_List_MissingChannelAndEmpty(t *testing.T) {
	t.Parallel()
	h, _ := newRedemptionsHandler(t)
	router := redemptionsRouter(h)

	missing := doJSON(t, router, http.MethodGet, "/api/v1/redemptions", nil)
	assert.Equal(t, http.StatusBadRequest, missing.StatusCode)

	empty := doJSON(t, router, http.MethodGet, "/api/v1/redemptions?channel=engelswtf", nil)
	require.Equal(t, http.StatusOK, empty.StatusCode)
	got := decodeBody(t, empty)
	bindings, ok := got["bindings"].([]any)
	require.True(t, ok, "bindings must be a JSON array, not null")
	assert.Len(t, bindings, 0)
}

func TestRedemptions_Update(t *testing.T) {
	t.Parallel()
	h, _ := newRedemptionsHandler(t)
	router := redemptionsRouter(h)

	create := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":      "engelswtf",
		"reward_id":    "reward-u",
		"reward_title": "Old",
		"action_type":  redemptions.ActionNone,
	})
	require.Equal(t, http.StatusCreated, create.StatusCode)

	upd := doJSON(t, router, http.MethodPut, "/api/v1/redemptions/reward-u", map[string]any{
		"channel":      "engelswtf",
		"reward_title": "New",
		"action_type":  redemptions.ActionCounterIncr,
		"action_param": "hugs",
	})
	require.Equal(t, http.StatusOK, upd.StatusCode)
	got := decodeBody(t, upd)
	assert.Equal(t, "New", got["reward_title"])
	assert.Equal(t, redemptions.ActionCounterIncr, got["action_type"])
	assert.Equal(t, "hugs", got["action_param"])
	assert.Equal(t, true, got["enabled"], "omitted enabled defaults true on Update")

	missing := doJSON(t, router, http.MethodPut, "/api/v1/redemptions/nope", map[string]any{
		"channel":     "engelswtf",
		"action_type": redemptions.ActionNone,
	})
	assert.Equal(t, http.StatusNotFound, missing.StatusCode)
}

func TestRedemptions_SetEnabled(t *testing.T) {
	t.Parallel()
	h, _ := newRedemptionsHandler(t)
	router := redemptionsRouter(h)

	create := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":     "engelswtf",
		"reward_id":   "reward-e",
		"action_type": redemptions.ActionNone,
		"enabled":     true,
	})
	require.Equal(t, http.StatusCreated, create.StatusCode)

	toggle := doJSON(t, router, http.MethodPost, "/api/v1/redemptions/reward-e/enabled", map[string]any{
		"channel": "engelswtf",
		"enabled": false,
	})
	require.Equal(t, http.StatusOK, toggle.StatusCode)
	got := decodeBody(t, toggle)
	assert.Equal(t, false, got["enabled"])

	listRes := doJSON(t, router, http.MethodGet, "/api/v1/redemptions?channel=engelswtf", nil)
	listed := decodeBody(t, listRes)
	bindings := listed["bindings"].([]any)
	require.Len(t, bindings, 1)
	first := bindings[0].(map[string]any)
	assert.Equal(t, false, first["enabled"])

	missing := doJSON(t, router, http.MethodPost, "/api/v1/redemptions/reward-e/enabled", map[string]any{
		"channel": "engelswtf",
	})
	assert.Equal(t, http.StatusBadRequest, missing.StatusCode)

	notFound := doJSON(t, router, http.MethodPost, "/api/v1/redemptions/ghost/enabled", map[string]any{
		"channel": "engelswtf",
		"enabled": true,
	})
	assert.Equal(t, http.StatusNotFound, notFound.StatusCode)
}

func TestRedemptions_Delete(t *testing.T) {
	t.Parallel()
	h, _ := newRedemptionsHandler(t)
	router := redemptionsRouter(h)

	create := doJSON(t, router, http.MethodPost, "/api/v1/redemptions", map[string]any{
		"channel":     "engelswtf",
		"reward_id":   "reward-d",
		"action_type": redemptions.ActionNone,
	})
	require.Equal(t, http.StatusCreated, create.StatusCode)

	del := doJSON(t, router, http.MethodDelete, "/api/v1/redemptions/reward-d?channel=engelswtf", nil)
	require.Equal(t, http.StatusNoContent, del.StatusCode)

	listRes := doJSON(t, router, http.MethodGet, "/api/v1/redemptions?channel=engelswtf", nil)
	listed := decodeBody(t, listRes)
	bindings := listed["bindings"].([]any)
	assert.Len(t, bindings, 0)

	notFound := doJSON(t, router, http.MethodDelete, "/api/v1/redemptions/reward-d?channel=engelswtf", nil)
	assert.Equal(t, http.StatusNotFound, notFound.StatusCode)

	missingChannel := doJSON(t, router, http.MethodDelete, "/api/v1/redemptions/reward-d", nil)
	assert.Equal(t, http.StatusBadRequest, missingChannel.StatusCode)
}

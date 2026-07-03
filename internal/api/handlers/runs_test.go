package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

type fakeRunSource struct {
	runs map[string][]actions.RunTrace
}

func crKey(channel, rule string) string { return channel + "/" + rule }

func (f *fakeRunSource) ListRuns(_ context.Context, _, channel, rule string, limit int) ([]actions.RunTrace, error) {
	got := f.runs[crKey(channel, rule)]
	if limit >= 0 && limit < len(got) {
		got = got[:limit]
	}
	return got, nil
}

func (f *fakeRunSource) GetRun(_ context.Context, _, channel, rule, runID string) (actions.RunTrace, error) {
	for _, r := range f.runs[crKey(channel, rule)] {
		if r.ID == runID {
			return r, nil
		}
	}
	return actions.RunTrace{}, actions.ErrRunNotFound
}

func (f *fakeRunSource) DeleteRuns(_ context.Context, _, channel, rule string) (int, error) {
	n := len(f.runs[crKey(channel, rule)])
	delete(f.runs, crKey(channel, rule))
	return n, nil
}

func mountRuns(h *Runs) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/channels/{channelSlug}/actions/{name}/runs", h.List)
	r.Get("/api/v1/channels/{channelSlug}/actions/{name}/runs/{runID}", h.Detail)
	r.Delete("/api/v1/channels/{channelSlug}/actions/{name}/runs", h.Delete)
	return r
}

func doReq(srv http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestRuns_ListNewestFirst(t *testing.T) {
	src := &fakeRunSource{runs: map[string][]actions.RunTrace{
		crKey("chan", "deploy"): {
			{ID: "run-2", Status: "ok"},
			{ID: "run-1", Status: "partial"},
		},
	}}
	srv := mountRuns(NewRuns(src, "local", nil))
	rec := doReq(srv, http.MethodGet, "/api/v1/channels/chan/actions/deploy/runs?channel=chan")
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Runs []actions.RunTrace `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Runs, 2)
	assert.Equal(t, "run-2", resp.Runs[0].ID)
	assert.Equal(t, "run-1", resp.Runs[1].ID)
}

func TestRuns_DetailWithOrderedNodes(t *testing.T) {
	src := &fakeRunSource{runs: map[string][]actions.RunTrace{
		crKey("chan", "deploy"): {
			{ID: "run-1", Status: "partial", Nodes: []actions.RunNode{
				{Seq: 0, NodeKind: "action", TypeID: "builtin:log", Status: "ok"},
				{Seq: 1, NodeKind: "action", TypeID: "http:request", Status: "fail", Error: "boom"},
			}},
		},
	}}
	srv := mountRuns(NewRuns(src, "local", nil))
	rec := doReq(srv, http.MethodGet, "/api/v1/channels/chan/actions/deploy/runs/run-1?channel=chan")
	require.Equal(t, http.StatusOK, rec.Code)

	var run actions.RunTrace
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &run))
	require.Len(t, run.Nodes, 2)
	assert.Equal(t, "builtin:log", run.Nodes[0].TypeID)
	assert.Equal(t, "boom", run.Nodes[1].Error)
}

func TestRuns_DetailNotFound(t *testing.T) {
	srv := mountRuns(NewRuns(&fakeRunSource{runs: map[string][]actions.RunTrace{}}, "local", nil))
	rec := doReq(srv, http.MethodGet, "/api/v1/channels/chan/actions/deploy/runs/nope?channel=chan")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRuns_DeleteClears(t *testing.T) {
	src := &fakeRunSource{runs: map[string][]actions.RunTrace{
		crKey("chan", "deploy"): {{ID: "run-1"}, {ID: "run-2"}},
	}}
	srv := mountRuns(NewRuns(src, "local", nil))
	rec := doReq(srv, http.MethodDelete, "/api/v1/channels/chan/actions/deploy/runs?channel=chan")
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]int
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp["deleted"])
	assert.Empty(t, src.runs[crKey("chan", "deploy")])
}

func TestRuns_ForeignChannelIsolation(t *testing.T) {
	src := &fakeRunSource{runs: map[string][]actions.RunTrace{
		crKey("chanA", "deploy"): {{ID: "run-a", Status: "ok"}},
	}}
	srv := mountRuns(NewRuns(src, "local", nil))

	rec := doReq(srv, http.MethodGet, "/api/v1/channels/chanB/actions/deploy/runs?channel=chanB")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Runs []actions.RunTrace `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Runs, "channel B must not see channel A's runs")

	rec = doReq(srv, http.MethodGet, "/api/v1/channels/chanB/actions/deploy/runs/run-a?channel=chanB")
	assert.Equal(t, http.StatusNotFound, rec.Code, "channel B must not fetch channel A's run by id")
}

func TestRuns_NilSourceNotImplemented(t *testing.T) {
	srv := mountRuns(NewRuns(nil, "local", nil))
	rec := doReq(srv, http.MethodGet, "/api/v1/channels/chan/actions/deploy/runs?channel=chan")
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestRuns_Unauthenticated401(t *testing.T) {
	h := NewRuns(&fakeRunSource{runs: map[string][]actions.RunTrace{}}, "local", nil)
	r := chi.NewRouter()
	r.Use(apimw.RequireSession)
	r.Get("/api/v1/channels/{channelSlug}/actions/{name}/runs", h.List)

	rec := doReq(r, http.MethodGet, "/api/v1/channels/chan/actions/deploy/runs?channel=chan")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// With a session, the same request passes the gate and reaches the handler.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/channels/chan/actions/deploy/runs?channel=chan", nil)
	req = req.WithContext(apimw.WithUser(req.Context(), auth.User{Role: auth.RoleOwner}))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

func TestParseRunLimit(t *testing.T) {
	assert.Equal(t, runsDefaultLimit, parseRunLimit(""))
	assert.Equal(t, runsDefaultLimit, parseRunLimit("abc"))
	assert.Equal(t, runsDefaultLimit, parseRunLimit("0"))
	assert.Equal(t, 25, parseRunLimit("25"))
	assert.Equal(t, runsMaxLimit, parseRunLimit("99999"))
}

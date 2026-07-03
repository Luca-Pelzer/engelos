package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/kb"
	"github.com/Luca-Pelzer/engelos/internal/workspaces"
)

// fakeKBStore is an in-memory kb.Store for handler tests. It records the last
// search arguments and enforces the same validation the SQLite store does, so
// the enum/cap 400s are exercised without a database.
type fakeKBStore struct {
	entries      map[string]kb.Entry
	seq          int
	lastQuery    string
	lastCategory string
	lastLimit    int
	searchHits   []kb.Entry
}

func newFakeKBStore() *fakeKBStore {
	return &fakeKBStore{entries: map[string]kb.Entry{}}
}

var validCategory = map[string]bool{
	"rules": true, "schedule": true, "games": true, "faq": true,
	"lore": true, "commands": true, "discord": true, "other": true,
}

func validateFake(e kb.Entry) error {
	if strings.TrimSpace(e.TenantID) == "" || strings.TrimSpace(e.Channel) == "" {
		return fmt.Errorf("%w: tenant/channel required", kb.ErrInvalid)
	}
	if strings.TrimSpace(e.Title) == "" || strings.TrimSpace(e.Content) == "" {
		return fmt.Errorf("%w: title/content required", kb.ErrInvalid)
	}
	if e.Category != "" && !validCategory[e.Category] {
		return fmt.Errorf("%w: bad category", kb.ErrInvalid)
	}
	if len(e.Content) > 4096 {
		return fmt.Errorf("%w: content too big", kb.ErrInvalid)
	}
	return nil
}

func (f *fakeKBStore) Create(_ context.Context, e kb.Entry) (kb.Entry, error) {
	if err := validateFake(e); err != nil {
		return kb.Entry{}, err
	}
	f.seq++
	e.ID = fmt.Sprintf("id-%d", f.seq)
	if e.Category == "" {
		e.Category = "other"
	}
	f.entries[e.ID] = e
	return e, nil
}

func (f *fakeKBStore) Update(_ context.Context, e kb.Entry) (kb.Entry, error) {
	if err := validateFake(e); err != nil {
		return kb.Entry{}, err
	}
	if _, ok := f.entries[e.ID]; !ok {
		return kb.Entry{}, kb.ErrNotFound
	}
	f.entries[e.ID] = e
	return e, nil
}

func (f *fakeKBStore) Get(_ context.Context, _, _, id string) (kb.Entry, error) {
	e, ok := f.entries[id]
	if !ok {
		return kb.Entry{}, kb.ErrNotFound
	}
	return e, nil
}

func (f *fakeKBStore) Delete(_ context.Context, _, _, id string) error {
	if _, ok := f.entries[id]; !ok {
		return kb.ErrNotFound
	}
	delete(f.entries, id)
	return nil
}

func (f *fakeKBStore) List(_ context.Context, _, _, category string, limit int) ([]kb.Entry, error) {
	var out []kb.Entry
	for _, e := range f.entries {
		if category == "" || e.Category == category {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeKBStore) Search(_ context.Context, _, _, query, category string, limit int) ([]kb.Entry, error) {
	f.lastQuery = query
	f.lastCategory = category
	f.lastLimit = limit
	return f.searchHits, nil
}

func (f *fakeKBStore) Close() error { return nil }

func kbBody(v map[string]any) *strings.Reader {
	b, _ := json.Marshal(v)
	return strings.NewReader(string(b))
}

// mountKB builds a router mirroring the real kb mount in router.go: reads are
// gated to owner+mod, writes to owner only, all under a workspace slug. role is
// the membership role injected as if WorkspaceMiddleware had resolved it.
func mountKB(h *KB, role workspaces.Role) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := middleware.WithWorkspace(req.Context(), middleware.WorkspaceContext{
				Workspace:  workspaces.Workspace{Slug: "chan"},
				Membership: workspaces.Membership{Role: role},
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Route("/api/v1/channels/{channelSlug}/kb", func(r chi.Router) {
		r.With(middleware.RequireRole(workspaces.RoleOwner, workspaces.RoleMod)).Get("/", h.List)
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireRole(workspaces.RoleOwner))
			r.Post("/", h.Create)
			r.Put("/{id}", h.Update)
			r.Delete("/{id}", h.Delete)
		})
	})
	return r
}

func kbReq(method, path string, body *strings.Reader) *http.Request {
	if body == nil {
		return httptest.NewRequest(method, path, nil)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func doKB(srv http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestKBHandler_CreateListDelete(t *testing.T) {
	store := newFakeKBStore()
	srv := mountKB(NewKB(store, "local", nil), workspaces.RoleOwner)

	rec := doKB(srv, kbReq(http.MethodPost, "/api/v1/channels/chan/kb",
		kbBody(map[string]any{"category": "rules", "title": "No spam", "content": "be nice"})))
	require.Equal(t, http.StatusCreated, rec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	id := created["id"].(string)
	assert.Equal(t, "rules", created["category"])
	assert.Equal(t, true, created["enabled"])

	rec = doKB(srv, kbReq(http.MethodGet, "/api/v1/channels/chan/kb", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var listed struct {
		Entries []map[string]any `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed.Entries, 1)

	rec = doKB(srv, kbReq(http.MethodDelete, "/api/v1/channels/chan/kb/"+id, nil))
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = doKB(srv, kbReq(http.MethodDelete, "/api/v1/channels/chan/kb/"+id, nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestKBHandler_SearchPassesQuery(t *testing.T) {
	store := newFakeKBStore()
	store.searchHits = []kb.Entry{{ID: "x", Category: "faq", Title: "hit", Content: "body"}}
	srv := mountKB(NewKB(store, "local", nil), workspaces.RoleOwner)

	rec := doKB(srv, kbReq(http.MethodGet, "/api/v1/channels/chan/kb?query=schedule&category=faq&limit=7", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "schedule", store.lastQuery)
	assert.Equal(t, "faq", store.lastCategory)
	assert.Equal(t, 7, store.lastLimit)

	var body struct {
		Query   string           `json:"query"`
		Entries []map[string]any `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "schedule", body.Query)
	require.Len(t, body.Entries, 1)
	assert.Equal(t, "hit", body.Entries[0]["title"])
}

func TestKBHandler_UpdateNotFound(t *testing.T) {
	store := newFakeKBStore()
	srv := mountKB(NewKB(store, "local", nil), workspaces.RoleOwner)

	rec := doKB(srv, kbReq(http.MethodPut, "/api/v1/channels/chan/kb/missing",
		kbBody(map[string]any{"category": "rules", "title": "t", "content": "c"})))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestKBHandler_ValidationErrors(t *testing.T) {
	store := newFakeKBStore()
	srv := mountKB(NewKB(store, "local", nil), workspaces.RoleOwner)

	cases := []map[string]any{
		{"category": "bogus", "title": "t", "content": "c"},
		{"category": "rules", "title": "t", "content": strings.Repeat("a", 4097)},
		{"category": "rules", "content": "no title"},
	}
	for _, body := range cases {
		rec := doKB(srv, kbReq(http.MethodPost, "/api/v1/channels/chan/kb", kbBody(body)))
		assert.Equal(t, http.StatusBadRequest, rec.Code, "body %v", body)
	}
}

func TestKBHandler_ModCanReadCannotWrite(t *testing.T) {
	store := newFakeKBStore()
	_, _ = store.Create(context.Background(), kb.Entry{
		TenantID: "local", Channel: "chan", Category: "rules", Title: "t", Content: "c", Enabled: true,
	})
	srv := mountKB(NewKB(store, "local", nil), workspaces.RoleMod)

	// Mod may read.
	rec := doKB(srv, kbReq(http.MethodGet, "/api/v1/channels/chan/kb", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	// Mod may NOT create, update or delete.
	rec = doKB(srv, kbReq(http.MethodPost, "/api/v1/channels/chan/kb",
		kbBody(map[string]any{"category": "rules", "title": "t", "content": "c"})))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = doKB(srv, kbReq(http.MethodPut, "/api/v1/channels/chan/kb/id-1",
		kbBody(map[string]any{"category": "rules", "title": "t", "content": "c"})))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = doKB(srv, kbReq(http.MethodDelete, "/api/v1/channels/chan/kb/id-1", nil))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestKBHandler_NilStoreNotImplemented(t *testing.T) {
	srv := mountKB(NewKB(nil, "local", nil), workspaces.RoleOwner)
	rec := doKB(srv, kbReq(http.MethodGet, "/api/v1/channels/chan/kb", nil))
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

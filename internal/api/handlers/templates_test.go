package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/workspaces"
)

type fakeTemplateStore struct {
	created []actions.Rule
	dup     bool
}

func (f *fakeTemplateStore) Create(_ context.Context, r actions.Rule) (actions.Rule, error) {
	if f.dup {
		return actions.Rule{}, actions.ErrAlreadyExists
	}
	f.created = append(f.created, r)
	return r, nil
}
func (f *fakeTemplateStore) Update(_ context.Context, r actions.Rule) (actions.Rule, error) {
	return r, nil
}
func (f *fakeTemplateStore) Get(context.Context, string, string, string) (actions.Rule, error) {
	return actions.Rule{}, actions.ErrNotFound
}
func (f *fakeTemplateStore) Delete(context.Context, string, string, string) error { return nil }
func (f *fakeTemplateStore) List(context.Context, string, string) ([]actions.Rule, error) {
	return nil, nil
}
func (f *fakeTemplateStore) ListEnabled(context.Context, string, string) ([]actions.Rule, error) {
	return nil, nil
}
func (f *fakeTemplateStore) ListTimerRules(context.Context, string) ([]actions.Rule, error) {
	return nil, nil
}
func (f *fakeTemplateStore) ListEventChannels(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (f *fakeTemplateStore) SetEnabled(context.Context, string, string, string, bool) error {
	return nil
}
func (f *fakeTemplateStore) Close() error { return nil }

func applyRequest(templateID, slug string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/apply", strings.NewReader(""))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", templateID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if slug != "" {
		ctx = middleware.WithWorkspace(ctx, middleware.WorkspaceContext{
			Workspace: workspaces.Workspace{Slug: slug},
		})
	}
	return req.WithContext(ctx)
}

func TestTemplatesListReturnsCatalog(t *testing.T) {
	h := NewTemplates(&fakeTemplateStore{}, "local", nil)
	rec := httptest.NewRecorder()

	h.List(rec, httptest.NewRequest(http.MethodGet, "/templates", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Templates []actions.Template `json:"templates"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body.Templates)
	assert.Equal(t, "donation-tts-thanks", body.Templates[0].ID)
}

func TestTemplatesApplyCreatesRuleInChannel(t *testing.T) {
	store := &fakeTemplateStore{}
	h := NewTemplates(store, "local", nil)
	rec := httptest.NewRecorder()

	h.Apply(rec, applyRequest("donation-tts-thanks", "streamer1"))

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, store.created, 1)
	assert.Equal(t, "streamer1", store.created[0].Channel)
	assert.Equal(t, "local", store.created[0].TenantID)
	assert.Equal(t, "donation-tts-thanks", store.created[0].Name)
	assert.Equal(t, actions.TriggerEvent, store.created[0].TriggerKind)
}

func TestTemplatesApplyDuplicateIsConflict(t *testing.T) {
	h := NewTemplates(&fakeTemplateStore{dup: true}, "local", nil)
	rec := httptest.NewRecorder()

	h.Apply(rec, applyRequest("donation-tts-thanks", "streamer1"))

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestTemplatesApplyUnknownTemplateIsNotFound(t *testing.T) {
	h := NewTemplates(&fakeTemplateStore{}, "local", nil)
	rec := httptest.NewRecorder()

	h.Apply(rec, applyRequest("does-not-exist", "streamer1"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTemplatesApplyMissingChannelIsBadRequest(t *testing.T) {
	h := NewTemplates(&fakeTemplateStore{}, "local", nil)
	rec := httptest.NewRecorder()

	h.Apply(rec, applyRequest("donation-tts-thanks", ""))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTemplatesApplyNilStoreNotImplemented(t *testing.T) {
	h := NewTemplates(nil, "local", nil)
	rec := httptest.NewRecorder()

	h.Apply(rec, applyRequest("donation-tts-thanks", "streamer1"))

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

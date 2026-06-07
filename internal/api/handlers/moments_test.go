package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Luca-Pelzer/engelos/internal/moments"
)

type capturingBroadcaster struct {
	events []string
}

func (c *capturingBroadcaster) Broadcast(eventType string, _ any) {
	c.events = append(c.events, eventType)
}

func newMomentsHandler(t *testing.T) (*Moments, *capturingBroadcaster) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "moments.db")
	st, err := moments.OpenSQLiteStore(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bc := &capturingBroadcaster{}
	return NewMoments(st, bc, "tenant1", slog.New(slog.NewTextHandler(io.Discard, nil))), bc
}

func TestMomentsOpenGetEnd(t *testing.T) {
	h, bc := newMomentsHandler(t)

	openReq := httptest.NewRequest(http.MethodPost, "/api/v1/moments?channel=chan",
		strings.NewReader(`{"channel":"chan","title":"GG","window_sec":60}`))
	openRec := httptest.NewRecorder()
	h.Open(openRec, openReq)
	if openRec.Code != http.StatusOK {
		t.Fatalf("open got %d want 200: %s", openRec.Code, openRec.Body.String())
	}
	var opened map[string]any
	_ = json.Unmarshal(openRec.Body.Bytes(), &opened)
	if opened["title"] != "GG" || opened["status"] != "open" {
		t.Fatalf("bad opened moment: %+v", opened)
	}

	getRec := httptest.NewRecorder()
	h.Get(getRec, httptest.NewRequest(http.MethodGet, "/api/v1/moments?channel=chan", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("get got %d want 200", getRec.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(getRec.Body.Bytes(), &got)
	if got["active"] == nil {
		t.Fatalf("expected active moment, got %+v", got)
	}

	endRec := httptest.NewRecorder()
	h.End(endRec, httptest.NewRequest(http.MethodPost, "/api/v1/moments/end?channel=chan",
		strings.NewReader(`{"channel":"chan"}`)))
	if endRec.Code != http.StatusOK {
		t.Fatalf("end got %d want 200: %s", endRec.Code, endRec.Body.String())
	}
	var ended map[string]any
	_ = json.Unmarshal(endRec.Body.Bytes(), &ended)
	if ended["status"] != "closed" || ended["rarity"] != "common" {
		t.Fatalf("bad ended moment: %+v", ended)
	}

	if len(bc.events) != 2 || bc.events[0] != "moment.opened" || bc.events[1] != "moment.closed" {
		t.Fatalf("expected opened+closed broadcasts, got %v", bc.events)
	}
}

func TestMomentsOpenConflict(t *testing.T) {
	h, _ := newMomentsHandler(t)
	body := `{"channel":"chan","title":"first"}`
	h.Open(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/moments?channel=chan", strings.NewReader(body)))

	rec := httptest.NewRecorder()
	h.Open(rec, httptest.NewRequest(http.MethodPost, "/api/v1/moments?channel=chan",
		strings.NewReader(`{"channel":"chan","title":"second"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d want 409", rec.Code)
	}
}

func TestMomentsEndNoActive(t *testing.T) {
	h, _ := newMomentsHandler(t)
	rec := httptest.NewRecorder()
	h.End(rec, httptest.NewRequest(http.MethodPost, "/api/v1/moments/end?channel=chan",
		strings.NewReader(`{"channel":"chan"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d want 404", rec.Code)
	}
}

func TestMomentsGetMissingChannel(t *testing.T) {
	h, _ := newMomentsHandler(t)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/moments", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d want 400", rec.Code)
	}
}

func TestMomentsParticipants(t *testing.T) {
	h, _ := newMomentsHandler(t)
	openRec := httptest.NewRecorder()
	h.Open(openRec, httptest.NewRequest(http.MethodPost, "/api/v1/moments?channel=chan",
		strings.NewReader(`{"channel":"chan","title":"GG"}`)))
	var opened map[string]any
	_ = json.Unmarshal(openRec.Body.Bytes(), &opened)
	id, _ := opened["id"].(string)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moments/"+id+"/participants?channel=chan", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("momentID", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Participants(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if _, ok := body["participants"]; !ok {
		t.Fatalf("expected participants key: %s", rec.Body.String())
	}
}

func TestMomentsDisabledWhenNoStore(t *testing.T) {
	h := NewMoments(nil, nil, "tenant1", slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/moments?channel=chan", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("got %d want 501", rec.Code)
	}
}

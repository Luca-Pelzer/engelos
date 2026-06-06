package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/tts"
)

type reversibleSecrets struct{}

func (reversibleSecrets) EncryptString(s string) ([]byte, error) {
	return []byte("enc:" + s), nil
}

func (reversibleSecrets) DecryptString(blob []byte) (string, error) {
	return strings.TrimPrefix(string(blob), "enc:"), nil
}

func newTTSHandler(t *testing.T) (*TTS, tts.Store) {
	t.Helper()
	st, err := tts.OpenSQLiteStore(context.Background(), "file:tts_handler_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewTTS(st, reversibleSecrets{}, "tenant1", logger), st
}

func TestTTSGetDefault(t *testing.T) {
	h, _ := newTTSHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tts?channel=chan", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["enabled"] != false || body["has_api_key"] != false {
		t.Fatalf("bad default: %+v", body)
	}
}

func TestTTSSetEncryptsKeyAndHidesIt(t *testing.T) {
	h, st := newTTSHandler(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tts",
		strings.NewReader(`{"channel":"chan","enabled":true,"voice_id":"v1","api_key":"sk-secret"}`))
	rec := httptest.NewRecorder()
	h.Set(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["has_api_key"] != true {
		t.Fatalf("has_api_key should be true: %+v", body)
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatal("plaintext key must never appear in response")
	}

	cfg, err := st.Get(context.Background(), "tenant1", "chan")
	if err != nil {
		t.Fatalf("store get: %v", err)
	}
	if string(cfg.APIKeyCiphertext) != "enc:sk-secret" {
		t.Fatalf("key not encrypted in store: %q", cfg.APIKeyCiphertext)
	}
	if !cfg.Enabled || cfg.VoiceID != "v1" {
		t.Fatalf("config not persisted: %+v", cfg)
	}
}

func TestTTSSetPreservesKeyWhenOmitted(t *testing.T) {
	h, st := newTTSHandler(t)
	ctx := context.Background()
	if _, err := st.Set(ctx, tts.Config{
		TenantID: "tenant1", Channel: "chan", Enabled: true, VoiceID: "v1",
		APIKeyCiphertext: []byte("enc:keep"),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tts",
		strings.NewReader(`{"channel":"chan","enabled":false}`))
	rec := httptest.NewRecorder()
	h.Set(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d want 200", rec.Code)
	}
	cfg, _ := st.Get(ctx, "tenant1", "chan")
	if string(cfg.APIKeyCiphertext) != "enc:keep" {
		t.Fatalf("omitted key must be preserved, got %q", cfg.APIKeyCiphertext)
	}
	if cfg.Enabled {
		t.Fatal("enabled should be false now")
	}
}

func TestTTSSetClearsKeyOnEmptyString(t *testing.T) {
	h, st := newTTSHandler(t)
	ctx := context.Background()
	if _, err := st.Set(ctx, tts.Config{
		TenantID: "tenant1", Channel: "chan", VoiceID: "v1",
		APIKeyCiphertext: []byte("enc:old"),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tts",
		strings.NewReader(`{"channel":"chan","api_key":""}`))
	rec := httptest.NewRecorder()
	h.Set(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d want 200", rec.Code)
	}
	cfg, _ := st.Get(ctx, "tenant1", "chan")
	if len(cfg.APIKeyCiphertext) != 0 {
		t.Fatalf("key should be cleared, got %q", cfg.APIKeyCiphertext)
	}
}

func TestTTSSetMissingChannel(t *testing.T) {
	h, _ := newTTSHandler(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tts", strings.NewReader(`{"enabled":true}`))
	rec := httptest.NewRecorder()
	h.Set(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d want 400", rec.Code)
	}
}

func TestTTSDisabledWhenNoSecrets(t *testing.T) {
	st, err := tts.OpenSQLiteStore(context.Background(), "file:tts_nosecrets?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := NewTTS(st, nil, "tenant1", slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tts?channel=c", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("got %d want 501", rec.Code)
	}
}

func TestTTSVoicesNoKey(t *testing.T) {
	h, _ := newTTSHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tts/voices?channel=chan", nil)
	rec := httptest.NewRecorder()
	h.Voices(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d want 400 (no_api_key)", rec.Code)
	}
}

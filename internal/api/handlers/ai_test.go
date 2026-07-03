package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/aibackend/aiconfig"
	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/secrets"
)

func aiDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func aiClearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ENGELOS_AI_PROVIDER", "ENGELOS_AI_BASE_URL", "ENGELOS_AI_API_KEY", "ENGELOS_AI_MODEL",
		"ENGELOS_TRANSLATE_BASE_URL", "ENGELOS_ANTHROPIC_API_KEY", "ENGELOS_TRANSLATE_MODEL",
	} {
		t.Setenv(k, "")
	}
}

func aiRealBox(t *testing.T) *secrets.Box {
	t.Helper()
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	box, err := secrets.NewBox(key)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	return box
}

// newAIHandler builds an AI handler backed by a real file store and manager.
// box may be nil to exercise the no-secrets-key path. It returns the handler
// and the directory holding the SQLite files (for on-disk leak scans).
func newAIHandler(t *testing.T, box aiconfig.Crypto) (*AI, string) {
	t.Helper()
	aiClearEnv(t)
	dir := t.TempDir()
	store, err := aiconfig.OpenSQLiteStore(context.Background(), filepath.Join(dir, "ai.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mgr, err := aiconfig.NewManager(context.Background(), store, box, "default", aiDiscardLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return NewAI(mgr, aiDiscardLogger()), dir
}

func aiOKServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"`+reply+`"}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type aiSnap struct {
	Provider   string `json:"provider"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	APIKeySet  bool   `json:"api_key_set"`
	APIKeyHint string `json:"api_key_hint"`
	Source     string `json:"source"`
}

func aiPut(t *testing.T, h *AI, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/ai/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.PutConfig(rec, req)
	return rec
}

func aiGet(t *testing.T, h *AI) (*httptest.ResponseRecorder, aiSnap) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/config", nil)
	rec := httptest.NewRecorder()
	h.GetConfig(rec, req)
	var snap aiSnap
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
			t.Fatalf("decode snapshot: %v", err)
		}
	}
	return rec, snap
}

func aiScanDirNoPlaintext(t *testing.T, dir, needle string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if bytes.Contains(b, []byte(needle)) {
			t.Fatalf("plaintext %q leaked to disk in %s", needle, e.Name())
		}
	}
}

func TestAI_PutThenGetMasksKeyAndNeverLeaksPlaintext(t *testing.T) {
	const key = "sk-test-123"
	h, dir := newAIHandler(t, aiRealBox(t))

	putRec := aiPut(t, h, `{"provider":"openai","model":"gpt-4o-mini","api_key":"`+key+`"}`)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", putRec.Code, putRec.Body.String())
	}
	if strings.Contains(putRec.Body.String(), key) {
		t.Fatalf("PUT response leaked the key: %s", putRec.Body.String())
	}

	getRec, snap := aiGet(t, h)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", getRec.Code)
	}
	if snap.Source != "db" {
		t.Errorf("source = %q want db", snap.Source)
	}
	if snap.Provider != "openai" {
		t.Errorf("provider = %q want openai", snap.Provider)
	}
	if !snap.APIKeySet {
		t.Error("api_key_set should be true")
	}
	if snap.APIKeyHint == "" || strings.Contains(snap.APIKeyHint, key) {
		t.Errorf("hint %q must be present and must not contain the key", snap.APIKeyHint)
	}
	if strings.Contains(getRec.Body.String(), key) {
		t.Fatalf("GET response leaked the key: %s", getRec.Body.String())
	}

	aiScanDirNoPlaintext(t, dir, key)
}

func TestAI_PutWithoutSecretsKeyFails4xx(t *testing.T) {
	h, _ := newAIHandler(t, nil) // no secrets box

	rec := aiPut(t, h, `{"provider":"openai","api_key":"sk-nope"}`)
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("want a 4xx, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "secrets_key_required") {
		t.Errorf("expected a clear missing-secrets-key error, got %s", rec.Body.String())
	}
}

func TestAI_PutKeepsThenClearsKey(t *testing.T) {
	h, _ := newAIHandler(t, aiRealBox(t))

	if rec := aiPut(t, h, `{"provider":"openai","api_key":"sk-keep-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("initial put: %d", rec.Code)
	}
	// Keep: no api_key field, change the model only.
	if rec := aiPut(t, h, `{"provider":"openai","model":"gpt-4o"}`); rec.Code != http.StatusOK {
		t.Fatalf("keep put: %d", rec.Code)
	}
	if _, snap := aiGet(t, h); !snap.APIKeySet || snap.Model != "gpt-4o" {
		t.Fatalf("keep failed: %+v", snap)
	}
	// Clear via flag.
	if rec := aiPut(t, h, `{"provider":"openai","clear_api_key":true}`); rec.Code != http.StatusOK {
		t.Fatalf("clear put: %d", rec.Code)
	}
	if _, snap := aiGet(t, h); snap.APIKeySet {
		t.Fatalf("key should be cleared: %+v", snap)
	}
}

func TestAI_TestEndpointOK(t *testing.T) {
	srv := aiOKServer(t, "OK")
	h, _ := newAIHandler(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/test",
		strings.NewReader(`{"provider":"openai","base_url":"`+srv.URL+`"}`))
	rec := httptest.NewRecorder()
	h.TestConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var res struct {
		OK       bool   `json:"ok"`
		Provider string `json:"provider"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK {
		t.Fatalf("test not ok: %s", rec.Body.String())
	}
	if res.Provider != "openai" {
		t.Errorf("provider = %q", res.Provider)
	}
}

func TestAI_TestEndpointStructuredFailureNo500(t *testing.T) {
	const key = "sk-secret-xyz-987"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid key `+key+`"}}`)
	}))
	t.Cleanup(srv.Close)

	h, _ := newAIHandler(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/test",
		strings.NewReader(`{"provider":"openai","base_url":"`+srv.URL+`","api_key":"`+key+`"}`))
	rec := httptest.NewRecorder()
	h.TestConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("failure must still be 200 (structured), got %d", rec.Code)
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.OK {
		t.Fatal("expected failure")
	}
	if res.Error == "" {
		t.Fatal("expected a structured error message")
	}
	if strings.Contains(rec.Body.String(), key) {
		t.Errorf("error response leaked the key: %s", rec.Body.String())
	}
}

func TestAI_ProvidersCatalog(t *testing.T) {
	h, _ := newAIHandler(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/providers", nil)
	rec := httptest.NewRecorder()
	h.Providers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Providers) < 5 {
		t.Fatalf("want at least 5 providers, got %d", len(body.Providers))
	}
}

func TestAI_UsageEndpoint(t *testing.T) {
	h, _ := newAIHandler(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/usage", nil)
	rec := httptest.NewRecorder()
	h.Usage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Calls      int64          `json:"calls"`
		Errors     int64          `json:"errors"`
		TokensIn   int64          `json:"tokens_in"`
		ByConsumer map[string]any `json:"by_consumer"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ByConsumer == nil {
		t.Fatal("by_consumer must be present (empty-state renders 'No AI calls yet')")
	}
	if body.Calls != 0 {
		t.Fatalf("fresh manager should report 0 calls, got %d", body.Calls)
	}
}

func TestAI_UsageGatedReturns401Unauth(t *testing.T) {
	h, _ := newAIHandler(t, nil)
	// RequireGlobalOwner without an authenticated user in context -> 401, the
	// same gate the router applies to every /ai route.
	handler := apimw.RequireGlobalOwner(http.HandlerFunc(h.Usage))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/usage", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated usage request should be 401, got %d", rec.Code)
	}
}

func TestAI_DisabledWhenNoManager(t *testing.T) {
	h := NewAI(nil, aiDiscardLogger())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/config", nil)
	rec := httptest.NewRecorder()
	h.GetConfig(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("nil manager should report 501, got %d", rec.Code)
	}
}

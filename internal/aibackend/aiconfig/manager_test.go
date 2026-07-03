package aiconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/aibackend"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/usage"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeStore is an in-memory Store for deterministic manager tests.
type fakeStore struct {
	mu  sync.Mutex
	cfg *StoredConfig
}

func (f *fakeStore) set(c StoredConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := c
	f.cfg = &cp
}

func (f *fakeStore) snapshot() (StoredConfig, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cfg == nil {
		return StoredConfig{}, false
	}
	return *f.cfg, true
}

func (f *fakeStore) Get(_ context.Context, _ string) (StoredConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cfg == nil {
		return StoredConfig{}, ErrNotFound
	}
	return *f.cfg, nil
}

func (f *fakeStore) Set(_ context.Context, c StoredConfig) (StoredConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := c
	f.cfg = &cp
	return cp, nil
}

func (f *fakeStore) Close() error { return nil }

// reversibleCrypto is a non-cryptographic reversible codec: it exercises the
// manager's encrypt/decrypt round trip without needing a real key.
type reversibleCrypto struct{}

func (reversibleCrypto) EncryptString(s string) ([]byte, error) { return []byte("enc:" + s), nil }
func (reversibleCrypto) DecryptString(b []byte) (string, error) {
	return strings.TrimPrefix(string(b), "enc:"), nil
}

// clearAIEnv blanks every variable LoadConfig reads so a test starts clean.
func clearAIEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ENGELOS_AI_PROVIDER", "ENGELOS_AI_BASE_URL", "ENGELOS_AI_API_KEY", "ENGELOS_AI_MODEL",
		"ENGELOS_TRANSLATE_BASE_URL", "ENGELOS_ANTHROPIC_API_KEY", "ENGELOS_TRANSLATE_MODEL",
	} {
		t.Setenv(k, "")
	}
}

// okOpenAIServer answers the OpenAI chat-completions wire with a fixed reply.
func okOpenAIServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"`+reply+`"}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestManager_PrecedenceDBoverEnv(t *testing.T) {
	clearAIEnv(t)
	t.Setenv("ENGELOS_AI_PROVIDER", "openai") // env says openai
	store := &fakeStore{}
	store.set(StoredConfig{TenantID: "default", Provider: "anthropic"}) // db says anthropic

	m, err := NewManager(context.Background(), store, nil, "default", quietLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	snap := m.Snapshot()
	if snap.Source != sourceDB {
		t.Errorf("source = %q, want db (db must win over env)", snap.Source)
	}
	if snap.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic (from db)", snap.Provider)
	}
}

func TestManager_EnvWhenNoDB(t *testing.T) {
	clearAIEnv(t)
	t.Setenv("ENGELOS_AI_PROVIDER", "openai")
	store := &fakeStore{}

	m, err := NewManager(context.Background(), store, nil, "default", quietLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	snap := m.Snapshot()
	if snap.Source != sourceEnv {
		t.Errorf("source = %q, want env", snap.Source)
	}
	if snap.Provider != "openai" {
		t.Errorf("provider = %q, want openai (from env)", snap.Provider)
	}
}

func TestManager_DefaultWhenNoDBNoEnv(t *testing.T) {
	clearAIEnv(t)
	store := &fakeStore{}

	m, err := NewManager(context.Background(), store, nil, "default", quietLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	snap := m.Snapshot()
	if snap.Source != sourceDefault {
		t.Errorf("source = %q, want default", snap.Source)
	}
	if snap.Provider != aibackend.ProviderAnthropic {
		t.Errorf("provider = %q, want anthropic default", snap.Provider)
	}
	if snap.APIKeySet {
		t.Error("default config should carry no key")
	}
}

// TestManager_ConcurrentCompleteAndReload hammers Complete from many goroutines
// while another goroutine swaps the backend via Reload. Run with -race it
// proves the atomic swap never tears and every call hits a live backend.
func TestManager_ConcurrentCompleteAndReload(t *testing.T) {
	clearAIEnv(t)
	srvA := okOpenAIServer(t, "OK")
	srvB := okOpenAIServer(t, "OK")
	store := &fakeStore{}
	store.set(StoredConfig{TenantID: "default", Provider: "openai", BaseURL: srvA.URL})

	m, err := NewManager(context.Background(), store, nil, "default", quietLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			url := srvA.URL
			if i%2 == 0 {
				url = srvB.URL
			}
			store.set(StoredConfig{TenantID: "default", Provider: "openai", BaseURL: url})
			if err := m.Reload(ctx); err != nil {
				t.Errorf("reload: %v", err)
				return
			}
		}
	}()

	const workers = 6
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				out, err := m.Complete(ctx, "sys", "hi")
				if err != nil {
					errs <- err
					return
				}
				if out != "OK" {
					errs <- fmt.Errorf("got %q want OK", out)
					return
				}
			}
			errs <- nil
		}()
	}

	for w := 0; w < workers; w++ {
		if err := <-errs; err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("concurrent complete failed: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}

func TestManager_UpdateConfigMasksKey(t *testing.T) {
	clearAIEnv(t)
	store := &fakeStore{}
	ctx := context.Background()
	m, err := NewManager(ctx, store, reversibleCrypto{}, "default", quietLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	const key = "sk-test-123"
	if err := m.UpdateConfig(ctx, ConfigUpdate{Provider: "openai", Model: "gpt-4o-mini", SetAPIKey: true, APIKey: key}); err != nil {
		t.Fatalf("update: %v", err)
	}

	snap := m.Snapshot()
	if snap.Source != sourceDB {
		t.Errorf("source = %q want db", snap.Source)
	}
	if snap.Provider != "openai" {
		t.Errorf("provider = %q want openai", snap.Provider)
	}
	if !snap.APIKeySet {
		t.Error("api_key_set should be true")
	}
	if snap.APIKeyHint == "" || snap.APIKeyHint == key || strings.Contains(snap.APIKeyHint, key) {
		t.Errorf("hint %q must be present and must not reveal the full key", snap.APIKeyHint)
	}

	stored, ok := store.snapshot()
	if !ok || string(stored.APIKeyCiphertext) == key {
		t.Errorf("stored key must be ciphertext, got %q", string(stored.APIKeyCiphertext))
	}
}

func TestManager_UpdateConfigKeepsKeyOnKeep(t *testing.T) {
	clearAIEnv(t)
	store := &fakeStore{}
	ctx := context.Background()
	m, _ := NewManager(ctx, store, reversibleCrypto{}, "default", quietLogger())

	if err := m.UpdateConfig(ctx, ConfigUpdate{Provider: "openai", SetAPIKey: true, APIKey: "sk-keep-999"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := m.UpdateConfig(ctx, ConfigUpdate{Provider: "openai", Model: "gpt-4o"}); err != nil {
		t.Fatalf("keep update: %v", err)
	}
	snap := m.Snapshot()
	if !snap.APIKeySet {
		t.Error("key should be retained across a keep update")
	}
	if snap.Model != "gpt-4o" {
		t.Errorf("model = %q want gpt-4o", snap.Model)
	}
}

func TestManager_UpdateConfigClearsKey(t *testing.T) {
	clearAIEnv(t)
	store := &fakeStore{}
	ctx := context.Background()
	m, _ := NewManager(ctx, store, reversibleCrypto{}, "default", quietLogger())

	if err := m.UpdateConfig(ctx, ConfigUpdate{Provider: "openai", SetAPIKey: true, APIKey: "sk-clear-me"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := m.UpdateConfig(ctx, ConfigUpdate{Provider: "openai", ClearAPIKey: true}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	snap := m.Snapshot()
	if snap.APIKeySet {
		t.Error("key should be cleared")
	}
	if stored, _ := store.snapshot(); stored.APIKeyCiphertext != nil {
		t.Errorf("ciphertext should be nil after clear, got %v", stored.APIKeyCiphertext)
	}
}

func TestManager_UpdateConfigNoSecretsKey(t *testing.T) {
	clearAIEnv(t)
	store := &fakeStore{}
	m, _ := NewManager(context.Background(), store, nil, "default", quietLogger()) // box nil

	err := m.UpdateConfig(context.Background(), ConfigUpdate{Provider: "openai", SetAPIKey: true, APIKey: "sk-nope"})
	if !errors.Is(err, ErrNoSecretsKey) {
		t.Fatalf("got %v want ErrNoSecretsKey", err)
	}
}

func TestManager_SnapshotNoKeyWhenBoxNil(t *testing.T) {
	clearAIEnv(t)
	store := &fakeStore{}
	store.set(StoredConfig{TenantID: "default", Provider: "openai", APIKeyCiphertext: []byte("enc:sk-unreadable")})

	m, err := NewManager(context.Background(), store, nil, "default", quietLogger()) // no box
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	snap := m.Snapshot()
	if snap.Source != sourceDB {
		t.Errorf("source = %q want db", snap.Source)
	}
	if snap.APIKeySet {
		t.Error("key must report unset when it cannot be decrypted (no secrets key)")
	}
	if snap.APIKeyHint != "" {
		t.Error("no hint without a usable key")
	}
}

func TestManager_TestActiveOK(t *testing.T) {
	clearAIEnv(t)
	srv := okOpenAIServer(t, "OK")
	store := &fakeStore{}
	store.set(StoredConfig{TenantID: "default", Provider: "openai", BaseURL: srv.URL, Model: "gpt-4o-mini"})
	m, _ := NewManager(context.Background(), store, nil, "default", quietLogger())

	res := m.Test(context.Background(), nil)
	if !res.OK {
		t.Fatalf("test not ok: %+v", res)
	}
	if res.Provider != "openai" {
		t.Errorf("provider = %q want openai", res.Provider)
	}
	if res.Model != "gpt-4o-mini" {
		t.Errorf("model = %q want gpt-4o-mini", res.Model)
	}
	if res.Error != "" {
		t.Errorf("unexpected error: %q", res.Error)
	}
}

func TestManager_TestLabelsUsage(t *testing.T) {
	clearAIEnv(t)
	srv := okOpenAIServer(t, "OK")
	store := &fakeStore{}
	store.set(StoredConfig{TenantID: "default", Provider: "openai", BaseURL: srv.URL})
	reg := usage.NewRegistry()
	m, err := NewManager(context.Background(), store, nil, "default", quietLogger(), WithUsageRegistry(reg))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if res := m.Test(context.Background(), nil); !res.OK {
		t.Fatalf("test not ok: %+v", res)
	}

	snap := m.UsageSnapshot()
	if snap.Calls != 1 {
		t.Fatalf("total calls = %d want 1", snap.Calls)
	}
	if got := snap.ByConsumer["test"]; got.Calls != 1 || got.OK != 1 {
		t.Fatalf("test-label breakdown wrong: %+v", got)
	}
}

func TestManager_TestSubmittedFailureScrubsKey(t *testing.T) {
	clearAIEnv(t)
	const key = "sk-secret-xyz-987"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid key `+key+`"}}`)
	}))
	t.Cleanup(srv.Close)

	store := &fakeStore{}
	m, _ := NewManager(context.Background(), store, nil, "default", quietLogger())

	res := m.Test(context.Background(), &aibackend.Config{Provider: "openai", BaseURL: srv.URL, APIKey: key})
	if res.OK {
		t.Fatal("test should have failed against a 400 endpoint")
	}
	if res.Error == "" {
		t.Fatal("expected a structured error message")
	}
	if strings.Contains(res.Error, key) {
		t.Errorf("error must not leak the api key: %q", res.Error)
	}
}

func TestHintKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc", "…"},
		{"abcdef", "…"},
		{"abcdefg", "abc…efg"},
		{"sk-test-123", "sk-…123"},
	}
	for _, c := range cases {
		if got := hintKey(c.in); got != c.want {
			t.Errorf("hintKey(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestScrubError(t *testing.T) {
	if got := scrubError(nil, "k"); got != "" {
		t.Errorf("nil error should scrub to empty, got %q", got)
	}
	got := scrubError(errors.New("boom key=sk-secret tail"), "sk-secret")
	if strings.Contains(got, "sk-secret") {
		t.Errorf("key not scrubbed: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Errorf("expected redaction marker: %q", got)
	}
}

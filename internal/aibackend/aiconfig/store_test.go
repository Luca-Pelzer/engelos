package aiconfig

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/secrets"
)

func newFileStore(t *testing.T) Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "ai.db")
	st, err := OpenSQLiteStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestStore_GetNotFound(t *testing.T) {
	st := newFileStore(t)
	if _, err := st.Get(context.Background(), "default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v want ErrNotFound", err)
	}
}

func TestStore_SetGetRoundTrip(t *testing.T) {
	st := newFileStore(t)
	ctx := context.Background()
	cipher := []byte{0x01, 0xAA, 0xBB, 0x00, 0xFF}

	saved, err := st.Set(ctx, StoredConfig{
		TenantID:         "default",
		Provider:         "openai",
		BaseURL:          "https://x.example",
		Model:            "gpt-4o-mini",
		APIKeyCiphertext: cipher,
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if saved.UpdatedAt.IsZero() {
		t.Fatal("updated_at not populated on save")
	}

	got, err := st.Get(ctx, "default")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Provider != "openai" || got.BaseURL != "https://x.example" || got.Model != "gpt-4o-mini" {
		t.Fatalf("bad round trip: %+v", got)
	}
	if !bytes.Equal(got.APIKeyCiphertext, cipher) {
		t.Fatalf("ciphertext not preserved: %v", got.APIKeyCiphertext)
	}
}

func TestStore_UpsertSingleRow(t *testing.T) {
	st := newFileStore(t)
	ctx := context.Background()
	if _, err := st.Set(ctx, StoredConfig{TenantID: "default", Provider: "anthropic"}); err != nil {
		t.Fatalf("set1: %v", err)
	}
	if _, err := st.Set(ctx, StoredConfig{TenantID: "default", Provider: "openai"}); err != nil {
		t.Fatalf("set2: %v", err)
	}
	got, err := st.Get(ctx, "default")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Provider != "openai" {
		t.Fatalf("upsert did not overwrite: %q", got.Provider)
	}
}

func TestStore_ValidationRequiresTenant(t *testing.T) {
	st := newFileStore(t)
	if _, err := st.Set(context.Background(), StoredConfig{Provider: "openai"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing tenant: got %v want ErrInvalid", err)
	}
}

// TestStore_CiphertextOnDiskNotPlaintext proves the store persists only the
// opaque ciphertext: after writing an encrypted key, the plaintext must not
// appear anywhere in the on-disk database files (main, -wal, -shm).
func TestStore_CiphertextOnDiskNotPlaintext(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "ai.db")
	ctx := context.Background()

	st, err := OpenSQLiteStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	box, err := secrets.NewBox(key)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}

	const plaintext = "sk-super-secret-value-1234567890"
	ct, err := box.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := st.Set(ctx, StoredConfig{TenantID: "default", Provider: "openai", APIKeyCiphertext: ct}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	needle := []byte(plaintext)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if bytes.Contains(b, needle) {
			t.Fatalf("plaintext key leaked to disk in %s", e.Name())
		}
	}
}

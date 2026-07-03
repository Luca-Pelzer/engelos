package credstore

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/secrets"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newBox(t *testing.T) *secrets.Box {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	box, err := secrets.NewBox(key)
	require.NoError(t, err)
	return box
}

func TestStore_Roundtrip(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(ctx, filepath.Join(t.TempDir(), "integrations.db"), newBox(t), discardLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	require.NoError(t, s.Put(ctx, "local", "elevenlabs", "api_key", "sk-secret-value"))
	got, err := s.Get(ctx, "local", "elevenlabs", "api_key")
	require.NoError(t, err)
	assert.Equal(t, "sk-secret-value", got)

	keys, err := s.ListKeys(ctx, "local", "elevenlabs")
	require.NoError(t, err)
	assert.Equal(t, []string{"api_key"}, keys)

	// Revoke removes every credential for the integration.
	require.NoError(t, s.Delete(ctx, "local", "elevenlabs"))
	_, err = s.Get(ctx, "local", "elevenlabs", "api_key")
	assert.ErrorIs(t, err, ErrNotFound)
	keys, err = s.ListKeys(ctx, "local", "elevenlabs")
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestStore_UpsertOverwrites(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(ctx, filepath.Join(t.TempDir(), "integrations.db"), newBox(t), discardLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	require.NoError(t, s.Put(ctx, "local", "obs", "password", "first"))
	require.NoError(t, s.Put(ctx, "local", "obs", "password", "second"))
	got, err := s.Get(ctx, "local", "obs", "password")
	require.NoError(t, err)
	assert.Equal(t, "second", got)
	keys, _ := s.ListKeys(ctx, "local", "obs")
	assert.Equal(t, []string{"password"}, keys)
}

func TestStore_CiphertextOnlyOnDisk(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "integrations.db")
	s, err := OpenStore(ctx, dsn, newBox(t), discardLogger())
	require.NoError(t, err)

	const needle = "sk-PLAINTEXT-NEEDLE-9f3a1c"
	require.NoError(t, s.Put(ctx, "local", "elevenlabs", "api_key", needle))
	require.NoError(t, s.Close()) // checkpoints the WAL into the main file

	// The plaintext must appear in NONE of the on-disk files.
	found := false
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, rerr := os.ReadFile(dsn + suffix)
		if rerr != nil {
			continue
		}
		if len(data) > 0 {
			assert.NotContainsf(t, string(data), needle, "plaintext leaked into %s", dsn+suffix)
			// Sanity: the encrypted key name may be plaintext (it is), but the
			// value must not be. Confirm the file is non-trivial.
			found = true
		}
	}
	require.True(t, found, "expected a non-empty database file to inspect")
}

func TestStore_PutWithoutCryptoRefuses(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(ctx, filepath.Join(t.TempDir(), "integrations.db"), nil, discardLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	err = s.Put(ctx, "local", "elevenlabs", "api_key", "x")
	assert.ErrorIs(t, err, ErrNoCrypto)

	// ListKeys/Delete stay usable even without a key.
	keys, lerr := s.ListKeys(ctx, "local", "elevenlabs")
	require.NoError(t, lerr)
	assert.Empty(t, keys)
	assert.NoError(t, s.Delete(ctx, "local", "elevenlabs"))
}

func TestStore_ScopeIsolation(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(ctx, filepath.Join(t.TempDir(), "integrations.db"), newBox(t), discardLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	require.NoError(t, s.Put(ctx, "local", "elevenlabs", "api_key", "a"))
	require.NoError(t, s.Put(ctx, "local", "obs", "password", "b"))

	// Deleting one integration never touches another.
	require.NoError(t, s.Delete(ctx, "local", "elevenlabs"))
	got, err := s.Get(ctx, "local", "obs", "password")
	require.NoError(t, err)
	assert.Equal(t, "b", got)
}

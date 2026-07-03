package plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore opens a fresh in-memory plugin state store per test.
func newTestStore(t *testing.T) StateStore {
	t.Helper()
	store, err := OpenSQLiteStore(context.Background(), "file::memory:?cache=shared", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestStore_SetGetRoundtrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	enabled, found, err := store.Get(ctx, "local", "quotes")
	require.NoError(t, err)
	assert.False(t, found, "no row before Set")
	assert.False(t, enabled)

	require.NoError(t, store.Set(ctx, "local", "quotes", true))
	enabled, found, err = store.Get(ctx, "local", "quotes")
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, enabled)

	// Upsert overwrites, never duplicates.
	require.NoError(t, store.Set(ctx, "local", "quotes", false))
	enabled, found, err = store.Get(ctx, "local", "quotes")
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, enabled)
}

func TestStore_GetOrDefaultPreservesDefault(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// No row: the caller's default (the manifest default) is returned verbatim,
	// so today's per-feature defaults survive - default-on and default-off both.
	on, err := store.GetOrDefault(ctx, "local", "quotes", true)
	require.NoError(t, err)
	assert.True(t, on, "default-on preserved when unset")

	off, err := store.GetOrDefault(ctx, "local", "music", false)
	require.NoError(t, err)
	assert.False(t, off, "songrequests-style default-off preserved when unset")

	// Once persisted, the stored value wins over the default.
	require.NoError(t, store.Set(ctx, "local", "music", true))
	got, err := store.GetOrDefault(ctx, "local", "music", false)
	require.NoError(t, err)
	assert.True(t, got, "stored value overrides default")
}

func TestStore_ListOrdersByPluginID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "local", "zeta", true))
	require.NoError(t, store.Set(ctx, "local", "alpha", false))
	require.NoError(t, store.Set(ctx, "local", "mike", true))

	list, err := store.List(ctx, "local")
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, "alpha", list[0].PluginID)
	assert.Equal(t, "mike", list[1].PluginID)
	assert.Equal(t, "zeta", list[2].PluginID)
	assert.False(t, list[0].Enabled)
	assert.True(t, list[2].Enabled)
}

func TestStore_TenantIsolation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "tenantA", "quotes", true))

	_, found, err := store.Get(ctx, "tenantB", "quotes")
	require.NoError(t, err)
	assert.False(t, found, "tenantB sees no tenantA state")
}

func TestStore_ValidationRejectsEmptyKeys(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	assert.ErrorIs(t, store.Set(ctx, "", "quotes", true), ErrInvalid)
	assert.ErrorIs(t, store.Set(ctx, "local", "  ", true), ErrInvalid)
}

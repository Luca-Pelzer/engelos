package translate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore opens an in-memory SQLite store with a unique shared cache so
// each test gets an isolated database that still survives multiple connections.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	st, err := OpenSQLiteStore(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, st.Close())
	})
	return st
}

func TestSet_ThenGet(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	in := Config{
		TenantID:     "tenant-1",
		Channel:      "#MyChannel",
		Enabled:      true,
		TargetLang:   "EN",
		OutputMode:   "reply",
		MinWordCount: 3,
	}
	stored, err := st.Set(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "mychannel", stored.Channel)
	assert.True(t, stored.Enabled)
	assert.Equal(t, "en", stored.TargetLang)
	assert.Equal(t, "reply", stored.OutputMode)
	assert.Equal(t, 3, stored.MinWordCount)
	assert.False(t, stored.UpdatedAt.IsZero())

	got, err := st.Get(ctx, "tenant-1", "mychannel")
	require.NoError(t, err)
	assert.Equal(t, stored, got)
}

func TestGet_NotFound(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Get(ctx, "tenant-1", "nope")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetOrDefault_DisabledWhenMissing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	c, err := st.GetOrDefault(ctx, "tenant-1", "#Fresh")
	require.NoError(t, err)
	assert.Equal(t, "tenant-1", c.TenantID)
	assert.Equal(t, "fresh", c.Channel)
	assert.False(t, c.Enabled)
	assert.Equal(t, "en", c.TargetLang)
	assert.Equal(t, "chat", c.OutputMode)
	assert.True(t, c.UpdatedAt.IsZero())
}

func TestSet_NormalizesChannel(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Set(ctx, Config{TenantID: "t", Channel: "  #HELLO  ", TargetLang: "en"})
	require.NoError(t, err)

	got, err := st.Get(ctx, "t", "hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", got.Channel)
}

func TestSet_InvalidTargetLang(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Set(ctx, Config{TenantID: "t", Channel: "c", TargetLang: "english"})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestSet_InvalidOutputMode(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Set(ctx, Config{TenantID: "t", Channel: "c", OutputMode: "whisper"})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestSet_NegativeMinWordCount(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Set(ctx, Config{TenantID: "t", Channel: "c", MinWordCount: -1})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestSet_EmptyTenant(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Set(ctx, Config{TenantID: "  ", Channel: "c"})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestList_OrderedByChannel(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	for _, ch := range []string{"zebra", "alpha", "mike"} {
		_, err := st.Set(ctx, Config{TenantID: "t", Channel: ch, TargetLang: "en"})
		require.NoError(t, err)
	}
	// A different tenant must not leak into the list.
	_, err := st.Set(ctx, Config{TenantID: "other", Channel: "shouldnotappear", TargetLang: "en"})
	require.NoError(t, err)

	got, err := st.List(ctx, "t")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "alpha", got[0].Channel)
	assert.Equal(t, "mike", got[1].Channel)
	assert.Equal(t, "zebra", got[2].Channel)
}

func TestSet_UpsertOverwrites(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, err := st.Set(ctx, Config{TenantID: "t", Channel: "c", Enabled: true, TargetLang: "en", MinWordCount: 5})
	require.NoError(t, err)
	_, err = st.Set(ctx, Config{TenantID: "t", Channel: "c", Enabled: false, TargetLang: "de", MinWordCount: 9})
	require.NoError(t, err)

	got, err := st.Get(ctx, "t", "c")
	require.NoError(t, err)
	assert.False(t, got.Enabled)
	assert.Equal(t, "de", got.TargetLang)
	assert.Equal(t, 9, got.MinWordCount)

	all, err := st.List(ctx, "t")
	require.NoError(t, err)
	assert.Len(t, all, 1, "upsert must not create duplicate rows")
}

func TestSet_TargetLangDefaultsToEn(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	stored, err := st.Set(ctx, Config{TenantID: "t", Channel: "c"})
	require.NoError(t, err)
	assert.Equal(t, "en", stored.TargetLang)
	assert.Equal(t, "chat", stored.OutputMode)
}

// errUnused keeps the errors import meaningful if tests are trimmed.
var _ = errors.Is

package cohost

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := fmt.Sprintf("file:cohost-%d?mode=memory&cache=shared", time.Now().UnixNano())
	s, err := OpenSQLiteStore(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSet_ThenGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := Config{
		TenantID:    "local",
		Channel:     "chan-A",
		Enabled:     true,
		BotName:     "Engel",
		Persona:     "a witty gremlin",
		MaxReplyLen: 200,
	}
	stored, err := s.Set(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "local", stored.TenantID)
	assert.Equal(t, "chan-a", stored.Channel)
	assert.True(t, stored.Enabled)
	assert.Equal(t, "Engel", stored.BotName)
	assert.Equal(t, "a witty gremlin", stored.Persona)
	assert.Equal(t, 200, stored.MaxReplyLen)
	assert.False(t, stored.UpdatedAt.IsZero())

	got, err := s.Get(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, "chan-a", got.Channel)
	assert.Equal(t, "Engel", got.BotName)
	assert.Equal(t, 200, got.MaxReplyLen)
	assert.Equal(t, time.UTC, got.UpdatedAt.Location())
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "local", "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetOrDefault_DisabledWhenMissing(t *testing.T) {
	s := newTestStore(t)
	cfg, err := s.GetOrDefault(context.Background(), "local", "#Missing")
	require.NoError(t, err)
	assert.Equal(t, "local", cfg.TenantID)
	assert.Equal(t, "missing", cfg.Channel)
	assert.False(t, cfg.Enabled)
	assert.Equal(t, defaultBotName, cfg.BotName)
	assert.Equal(t, defaultPersona, cfg.Persona)
	assert.Equal(t, defaultMaxReplyLen, cfg.MaxReplyLen)
}

func TestSet_DefaultsAppliedWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	stored, err := s.Set(context.Background(), Config{TenantID: "local", Channel: "c"})
	require.NoError(t, err)
	assert.Equal(t, defaultBotName, stored.BotName)
	assert.Equal(t, defaultPersona, stored.Persona)
	assert.Equal(t, defaultMaxReplyLen, stored.MaxReplyLen)
}

func TestSet_Validation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Set(ctx, Config{Channel: "c"})
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Set(ctx, Config{TenantID: "local"})
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Set(ctx, Config{TenantID: "local", Channel: "c", MaxReplyLen: -1})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestSet_Upsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.Set(ctx, Config{TenantID: "local", Channel: "c", BotName: "a"})
	require.NoError(t, err)
	_, err = s.Set(ctx, Config{TenantID: "local", Channel: "c", BotName: "b", Enabled: true})
	require.NoError(t, err)
	got, err := s.Get(ctx, "local", "c")
	require.NoError(t, err)
	assert.Equal(t, "b", got.BotName)
	assert.True(t, got.Enabled)
}

func TestList_OrderedByChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _ = s.Set(ctx, Config{TenantID: "local", Channel: "zeta"})
	_, _ = s.Set(ctx, Config{TenantID: "local", Channel: "alpha"})
	_, _ = s.Set(ctx, Config{TenantID: "other", Channel: "beta"})
	list, err := s.List(ctx, "local")
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "alpha", list[0].Channel)
	assert.Equal(t, "zeta", list[1].Channel)
}

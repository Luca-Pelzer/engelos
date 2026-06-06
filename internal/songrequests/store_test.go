package songrequests

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := fmt.Sprintf("file:songreq-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSet_ThenGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := Config{
		TenantID:          "local",
		Channel:           "chan-A",
		Provider:          "spotify",
		SpotifyPlaylistID: "playlist123",
		MaxDurationSec:    300,
		Enabled:           true,
	}
	stored, err := s.Set(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "local", stored.TenantID)
	assert.Equal(t, "chan-a", stored.Channel) // normalised
	assert.Equal(t, "spotify", stored.Provider)
	assert.Equal(t, "playlist123", stored.SpotifyPlaylistID)
	assert.Equal(t, 300, stored.MaxDurationSec)
	assert.True(t, stored.Enabled)
	assert.False(t, stored.UpdatedAt.IsZero())

	got, err := s.Get(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, "local", got.TenantID)
	assert.Equal(t, "chan-a", got.Channel)
	assert.Equal(t, "spotify", got.Provider)
	assert.Equal(t, "playlist123", got.SpotifyPlaylistID)
	assert.Equal(t, 300, got.MaxDurationSec)
	assert.True(t, got.Enabled)
	assert.Equal(t, time.UTC, got.UpdatedAt.Location())
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "local", "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetOrDefault_DisabledWhenMissing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cfg, err := s.GetOrDefault(ctx, "local", "#Missing")
	require.NoError(t, err)
	assert.Equal(t, "local", cfg.TenantID)
	assert.Equal(t, "missing", cfg.Channel) // normalised
	assert.Equal(t, "", cfg.Provider)
	assert.False(t, cfg.Enabled)
	assert.Equal(t, 0, cfg.MaxDurationSec)
	assert.True(t, cfg.UpdatedAt.IsZero())
}

func TestGetOrDefault_ReturnsStored(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Set(ctx, Config{
		TenantID: "local", Channel: "chan-A",
		Provider: "youtube", Enabled: true, MaxDurationSec: 120,
	})
	require.NoError(t, err)

	cfg, err := s.GetOrDefault(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, "youtube", cfg.Provider)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 120, cfg.MaxDurationSec)
}

func TestSet_UpsertOverwrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Set(ctx, Config{
		TenantID: "local", Channel: "chan-A",
		Provider: "spotify", SpotifyPlaylistID: "first",
		MaxDurationSec: 100, Enabled: true,
	})
	require.NoError(t, err)

	stored, err := s.Set(ctx, Config{
		TenantID: "local", Channel: "chan-A",
		Provider: "youtube", SpotifyPlaylistID: "second",
		MaxDurationSec: 200, Enabled: false,
	})
	require.NoError(t, err)
	assert.Equal(t, "youtube", stored.Provider)
	assert.Equal(t, "second", stored.SpotifyPlaylistID)
	assert.Equal(t, 200, stored.MaxDurationSec)
	assert.False(t, stored.Enabled)

	// Second write wins on read-back.
	got, err := s.Get(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, "youtube", got.Provider)
	assert.Equal(t, "second", got.SpotifyPlaylistID)
	assert.Equal(t, 200, got.MaxDurationSec)
	assert.False(t, got.Enabled)

	// Exactly one row survives the upsert.
	all, err := s.List(ctx, "local")
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestSet_ValidProviders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i, p := range []string{"", "spotify", "youtube"} {
		ch := fmt.Sprintf("chan-%d", i)
		stored, err := s.Set(ctx, Config{TenantID: "local", Channel: ch, Provider: p})
		require.NoError(t, err, "provider %q must be accepted", p)
		assert.Equal(t, p, stored.Provider)
	}
}

func TestSet_InvalidProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Set(ctx, Config{TenantID: "local", Channel: "chan-A", Provider: "soundcloud"})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestSet_NegativeMaxDuration(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Set(ctx, Config{TenantID: "local", Channel: "chan-A", MaxDurationSec: -1})
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestSet_EmptyTenantOrChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name            string
		tenant, channel string
	}{
		{"empty tenant", "", "chan-A"},
		{"empty channel", "local", ""},
		{"whitespace channel", "local", "   "},
		{"only hash channel", "local", "#"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Set(ctx, Config{TenantID: tc.tenant, Channel: tc.channel})
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestGet_InvalidArgs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "", "chan-A")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Get(ctx, "local", "")
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestList_OrderedByChannelASC(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, mustSet(s, ctx, "local", "games", "spotify"))
	require.NoError(t, mustSet(s, ctx, "local", "alpha", "youtube"))
	require.NoError(t, mustSet(s, ctx, "local", "mid", ""))

	// Other tenant must not leak.
	require.NoError(t, mustSet(s, ctx, "other", "alpha", "spotify"))

	configs, err := s.List(ctx, "local")
	require.NoError(t, err)
	require.Len(t, configs, 3)
	assert.Equal(t, "alpha", configs[0].Channel)
	assert.Equal(t, "games", configs[1].Channel)
	assert.Equal(t, "mid", configs[2].Channel)
}

func TestList_Empty(t *testing.T) {
	s := newTestStore(t)
	configs, err := s.List(context.Background(), "local")
	require.NoError(t, err)
	assert.Empty(t, configs)
}

func TestList_InvalidTenant(t *testing.T) {
	s := newTestStore(t)
	_, err := s.List(context.Background(), "")
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestChannel_Normalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// "#Foo" and "foo" must map to the same row.
	_, err := s.Set(ctx, Config{TenantID: "local", Channel: "#Foo", Provider: "spotify"})
	require.NoError(t, err)
	_, err = s.Set(ctx, Config{TenantID: "local", Channel: "foo", Provider: "youtube"})
	require.NoError(t, err)

	all, err := s.List(ctx, "local")
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "foo", all[0].Channel)
	assert.Equal(t, "youtube", all[0].Provider) // second write wins

	got, err := s.Get(ctx, "local", "#FOO")
	require.NoError(t, err)
	assert.Equal(t, "foo", got.Channel)
}

func TestUpdatedAt_RoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Second)
	stored, err := s.Set(ctx, Config{TenantID: "local", Channel: "chan-A", Provider: "spotify"})
	require.NoError(t, err)
	after := time.Now().UTC().Add(time.Second)

	got, err := s.Get(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, stored.UpdatedAt.UnixNano(), got.UpdatedAt.UnixNano())
	assert.Equal(t, time.UTC, got.UpdatedAt.Location())
	assert.False(t, got.UpdatedAt.Before(before))
	assert.False(t, got.UpdatedAt.After(after))
}

// mustSet is a test helper that upserts a minimal valid Config.
func mustSet(s Store, ctx context.Context, tenant, channel, provider string) error {
	_, err := s.Set(ctx, Config{TenantID: tenant, Channel: channel, Provider: provider})
	return err
}

package featureflags

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := fmt.Sprintf("file:flags-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSet_ThenGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "local", "chan-A", "economy", true))

	enabled, found, err := s.Get(ctx, "local", "chan-A", "economy")
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, enabled)
}

func TestGet_UnsetNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	enabled, found, err := s.Get(ctx, "local", "chan-A", "economy")
	require.NoError(t, err)
	assert.False(t, found)
	assert.False(t, enabled)
}

func TestGetOrDefault(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Unset: returns the supplied default, both polarities.
	v, err := s.GetOrDefault(ctx, "local", "chan-A", "economy", true)
	require.NoError(t, err)
	assert.True(t, v)

	v, err = s.GetOrDefault(ctx, "local", "chan-A", "economy", false)
	require.NoError(t, err)
	assert.False(t, v)

	// Stored false overrides a true default.
	require.NoError(t, s.Set(ctx, "local", "chan-A", "economy", false))
	v, err = s.GetOrDefault(ctx, "local", "chan-A", "economy", true)
	require.NoError(t, err)
	assert.False(t, v)

	// Stored true overrides a false default.
	require.NoError(t, s.Set(ctx, "local", "chan-A", "games", true))
	v, err = s.GetOrDefault(ctx, "local", "chan-A", "games", false)
	require.NoError(t, err)
	assert.True(t, v)
}

func TestSet_UpsertFlipsValue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "local", "chan-A", "economy", true))
	enabled, found, err := s.Get(ctx, "local", "chan-A", "economy")
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, enabled)

	require.NoError(t, s.Set(ctx, "local", "chan-A", "economy", false))
	enabled, found, err = s.Get(ctx, "local", "chan-A", "economy")
	require.NoError(t, err)
	require.True(t, found)
	assert.False(t, enabled)

	// Exactly one row survives the upsert.
	flags, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Len(t, flags, 1)
}

func TestChannel_Normalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "local", "#Chan-A", "economy", true))

	enabled, found, err := s.Get(ctx, "local", "chan-a", "economy")
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, enabled)
}

func TestFeature_Normalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "local", "chan-A", "  Economy  ", true))

	enabled, found, err := s.Get(ctx, "local", "chan-A", "economy")
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, enabled)
}

func TestInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name                     string
		tenant, channel, feature string
	}{
		{"empty tenant", "", "chan-A", "economy"},
		{"empty channel", "local", "", "economy"},
		{"whitespace channel", "local", "   ", "economy"},
		{"empty feature", "local", "chan-A", ""},
		{"bad feature spaces", "local", "chan-A", "Bad Feature"},
		{"bad feature chars", "local", "chan-A", "bad!feature"},
		{"feature too long", "local", "chan-A", strings.Repeat("x", maxFeatureLen+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Set(ctx, tc.tenant, tc.channel, tc.feature, true)
			assert.ErrorIs(t, err, ErrInvalid)

			_, _, err = s.Get(ctx, tc.tenant, tc.channel, tc.feature)
			assert.ErrorIs(t, err, ErrInvalid)

			_, err = s.GetOrDefault(ctx, tc.tenant, tc.channel, tc.feature, true)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestList_OrderedByFeatureASC(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "local", "chan-A", "games", true))
	require.NoError(t, s.Set(ctx, "local", "chan-A", "economy", false))
	require.NoError(t, s.Set(ctx, "local", "chan-A", "polls", true))

	// Scoping: other channel / tenant must not leak.
	require.NoError(t, s.Set(ctx, "local", "chan-B", "economy", true))
	require.NoError(t, s.Set(ctx, "other", "chan-A", "economy", true))

	flags, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, flags, 3)
	assert.Equal(t, "economy", flags[0].Feature)
	assert.Equal(t, "games", flags[1].Feature)
	assert.Equal(t, "polls", flags[2].Feature)
	assert.False(t, flags[0].Enabled)
	assert.True(t, flags[1].Enabled)
	assert.Equal(t, "local", flags[0].TenantID)
	assert.Equal(t, "chan-a", flags[0].Channel)
}

func TestList_Empty(t *testing.T) {
	s := newTestStore(t)
	flags, err := s.List(context.Background(), "local", "chan-empty")
	require.NoError(t, err)
	assert.Empty(t, flags)
}

func TestList_Invalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.List(ctx, "", "chan-A")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.List(ctx, "local", "")
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestUpdatedAt_RoundTripsToSecond(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	before := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.Set(ctx, "local", "chan-A", "economy", true))
	after := time.Now().UTC()

	flags, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, flags, 1)

	ts := flags[0].UpdatedAt
	assert.Equal(t, ts, ts.Truncate(time.Second), "must be second-resolution")
	assert.Equal(t, time.UTC, ts.Location())
	assert.False(t, ts.Before(before), "updated_at %v before %v", ts, before)
	assert.False(t, ts.After(after), "updated_at %v after %v", ts, after)
}

func TestConcurrent_SetAndGetNoRace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup

	// 50 writers alternating true/false on the same flag.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			assert.NoError(t, s.Set(ctx, "local", "chan-A", "economy", i%2 == 0))
		}(i)
	}
	// Concurrent readers during the writes.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.Get(ctx, "local", "chan-A", "economy")
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	// Store ends in a valid state: exactly one row, readable.
	flags, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, flags, 1)
	_, found, err := s.Get(ctx, "local", "chan-A", "economy")
	require.NoError(t, err)
	assert.True(t, found)
}

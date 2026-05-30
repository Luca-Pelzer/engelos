package counters

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAdd_CreatesAndIncrements(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.Add(ctx, "local", "chan-A", "deaths", 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), c.Value)
	assert.Equal(t, "deaths", c.Name)
	assert.NotEmpty(t, c.ID)
	assert.False(t, c.UpdatedAt.IsZero())

	c, err = s.Add(ctx, "local", "chan-A", "deaths", 1)
	require.NoError(t, err)
	assert.Equal(t, int64(2), c.Value)

	c, err = s.Add(ctx, "local", "chan-A", "deaths", -1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), c.Value)

	c, err = s.Add(ctx, "local", "chan-A", "deaths", 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), c.Value)
}

func TestSet_NegativeAndAddBelowZero(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.Set(ctx, "local", "chan-A", "score", -5)
	require.NoError(t, err)
	assert.Equal(t, int64(-5), c.Value)

	c, err = s.Add(ctx, "local", "chan-A", "score", -3)
	require.NoError(t, err)
	assert.Equal(t, int64(-8), c.Value)
}

func TestGet_RoundTripAndNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "local", "chan-A", "deaths")
	assert.ErrorIs(t, err, ErrNotFound)

	added, err := s.Add(ctx, "local", "chan-A", "deaths", 7)
	require.NoError(t, err)

	got, err := s.Get(ctx, "local", "chan-A", "deaths")
	require.NoError(t, err)
	assert.Equal(t, added.ID, got.ID)
	assert.Equal(t, int64(7), got.Value)
}

func TestSet_CreatesAndOverwrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.Set(ctx, "local", "chan-A", "deaths", 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), c.Value)
	first := c.UpdatedAt

	c, err = s.Set(ctx, "local", "chan-A", "deaths", 100)
	require.NoError(t, err)
	assert.Equal(t, int64(100), c.Value)
	assert.False(t, c.UpdatedAt.Before(first))

	got, err := s.Get(ctx, "local", "chan-A", "deaths")
	require.NoError(t, err)
	assert.Equal(t, int64(100), got.Value)
}

func TestDelete_RemovesAndMissing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Add(ctx, "local", "chan-A", "deaths", 1)
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, "local", "chan-A", "deaths"))
	_, err = s.Get(ctx, "local", "chan-A", "deaths")
	assert.ErrorIs(t, err, ErrNotFound)

	assert.ErrorIs(t, s.Delete(ctx, "local", "chan-A", "deaths"), ErrNotFound)
}

func TestList_OrderedAndScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Set(ctx, "local", "chan-A", "wins", 1)
	require.NoError(t, err)
	_, err = s.Set(ctx, "local", "chan-A", "deaths", 2)
	require.NoError(t, err)
	_, err = s.Set(ctx, "local", "chan-A", "attempts", 3)
	require.NoError(t, err)

	_, err = s.Set(ctx, "local", "chan-B", "deaths", 9)
	require.NoError(t, err)
	_, err = s.Set(ctx, "other-tenant", "chan-A", "deaths", 99)
	require.NoError(t, err)

	got, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "attempts", got[0].Name)
	assert.Equal(t, "deaths", got[1].Name)
	assert.Equal(t, "wins", got[2].Name)

	gotB, err := s.List(ctx, "local", "chan-B")
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, int64(9), gotB[0].Value)
}

func TestList_EmptyChannel(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List(context.Background(), "local", "chan-empty")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestName_Normalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Add(ctx, "local", "chan-A", "!Deaths", 5)
	require.NoError(t, err)

	got, err := s.Get(ctx, "local", "chan-A", "deaths")
	require.NoError(t, err)
	assert.Equal(t, "deaths", got.Name)
	assert.Equal(t, int64(5), got.Value)
}

func TestName_Invalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"", "   ", "has space", "bad!chars", strings.Repeat("x", maxCounterNameLen+1)} {
		_, err := s.Add(ctx, "local", "chan-A", name, 1)
		assert.ErrorIs(t, err, ErrInvalid, "name %q must be invalid", name)
		_, err = s.Set(ctx, "local", "chan-A", name, 1)
		assert.ErrorIs(t, err, ErrInvalid, "name %q must be invalid", name)
		assert.ErrorIs(t, s.Delete(ctx, "local", "chan-A", name), ErrInvalid)
		_, err = s.Get(ctx, "local", "chan-A", name)
		assert.ErrorIs(t, err, ErrInvalid)
	}
}

func TestErrors_AreDistinct(t *testing.T) {
	assert.NotEqual(t, ErrNotFound, ErrInvalid)
}

func TestConcurrent_AddNoLostUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Add(ctx, "local", "chan-A", "deaths", 1)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	got, err := s.Get(ctx, "local", "chan-A", "deaths")
	require.NoError(t, err)
	assert.Equal(t, int64(n), got.Value)
}

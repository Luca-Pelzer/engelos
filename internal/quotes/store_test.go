package quotes

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "quotes.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAdd_AssignsSequentialNumbers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	q1, err := s.Add(ctx, "local", "chan-A", "first line", "mod-1")
	require.NoError(t, err)
	assert.Equal(t, 1, q1.Number)
	assert.NotEmpty(t, q1.ID)
	assert.Equal(t, "first line", q1.Text)
	assert.Equal(t, "mod-1", q1.CreatedBy)
	assert.False(t, q1.CreatedAt.IsZero())

	q2, err := s.Add(ctx, "local", "chan-A", "second line", "mod-1")
	require.NoError(t, err)
	assert.Equal(t, 2, q2.Number)

	q3, err := s.Add(ctx, "local", "chan-A", "third line", "mod-1")
	require.NoError(t, err)
	assert.Equal(t, 3, q3.Number)
}

func TestGet_RoundTripAndNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	added, err := s.Add(ctx, "local", "chan-A", "hello world", "mod-1")
	require.NoError(t, err)

	got, err := s.Get(ctx, "local", "chan-A", added.Number)
	require.NoError(t, err)
	assert.Equal(t, added.ID, got.ID)
	assert.Equal(t, "hello world", got.Text)
	assert.Equal(t, 1, got.Number)

	_, err = s.Get(ctx, "local", "chan-A", 99)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDelete_RemovesAndPreservesGaps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Add(ctx, "local", "chan-A", "one", "mod-1")
	require.NoError(t, err)
	_, err = s.Add(ctx, "local", "chan-A", "two", "mod-1")
	require.NoError(t, err)
	_, err = s.Add(ctx, "local", "chan-A", "three", "mod-1")
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, "local", "chan-A", 2))

	_, err = s.Get(ctx, "local", "chan-A", 2)
	assert.ErrorIs(t, err, ErrNotFound)

	assert.ErrorIs(t, s.Delete(ctx, "local", "chan-A", 2), ErrNotFound)

	q1, err := s.Get(ctx, "local", "chan-A", 1)
	require.NoError(t, err)
	assert.Equal(t, "one", q1.Text)
	q3, err := s.Get(ctx, "local", "chan-A", 3)
	require.NoError(t, err)
	assert.Equal(t, "three", q3.Text)

	q4, err := s.Add(ctx, "local", "chan-A", "four", "mod-1")
	require.NoError(t, err)
	assert.Equal(t, 4, q4.Number, "max+1 must not reuse the deleted number 2")
}

func TestDelete_MissingNotFound(t *testing.T) {
	s := newTestStore(t)
	assert.ErrorIs(t, s.Delete(context.Background(), "local", "chan-A", 1), ErrNotFound)
}

func TestGetRandom_EmptyChannel(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetRandom(context.Background(), "local", "chan-empty")
	assert.ErrorIs(t, err, ErrEmpty)
}

func TestGetRandom_ReturnsExistingQuote(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	want := map[int]bool{}
	for _, text := range []string{"a", "b", "c", "d", "e"} {
		q, err := s.Add(ctx, "local", "chan-A", text, "mod-1")
		require.NoError(t, err)
		want[q.Number] = true
	}

	for i := 0; i < 25; i++ {
		q, err := s.GetRandom(ctx, "local", "chan-A")
		require.NoError(t, err)
		assert.True(t, want[q.Number], "random returned unknown number %d", q.Number)
	}
}

func TestList_OrderedAndScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Add(ctx, "local", "chan-A", "one", "mod-1")
	require.NoError(t, err)
	_, err = s.Add(ctx, "local", "chan-A", "two", "mod-1")
	require.NoError(t, err)
	_, err = s.Add(ctx, "local", "chan-A", "three", "mod-1")
	require.NoError(t, err)

	_, err = s.Add(ctx, "local", "chan-B", "b-one", "mod-1")
	require.NoError(t, err)
	_, err = s.Add(ctx, "other-tenant", "chan-A", "x-one", "mod-1")
	require.NoError(t, err)

	got, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, 1, got[0].Number)
	assert.Equal(t, 2, got[1].Number)
	assert.Equal(t, 3, got[2].Number)
	assert.Equal(t, "one", got[0].Text)
	assert.Equal(t, "two", got[1].Text)
	assert.Equal(t, "three", got[2].Text)

	gotB, err := s.List(ctx, "local", "chan-B")
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, "b-one", gotB[0].Text)
	assert.Equal(t, 1, gotB[0].Number)
}

func TestList_EmptyChannel(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List(context.Background(), "local", "chan-empty")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, err := s.Count(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	_, err = s.Add(ctx, "local", "chan-A", "one", "mod-1")
	require.NoError(t, err)
	_, err = s.Add(ctx, "local", "chan-A", "two", "mod-1")
	require.NoError(t, err)

	n, err = s.Count(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestAdd_InvalidText(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, text := range []string{"", "   ", "\t\n"} {
		_, err := s.Add(ctx, "local", "chan-A", text, "mod-1")
		assert.ErrorIs(t, err, ErrInvalid, "text %q must be invalid", text)
	}

	_, err := s.Add(ctx, "local", "chan-A", strings.Repeat("x", maxQuoteLen+1), "mod-1")
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestErrors_AreDistinct(t *testing.T) {
	assert.False(t, errorsIs(ErrNotFound, ErrInvalid))
	assert.False(t, errorsIs(ErrInvalid, ErrEmpty))
	assert.False(t, errorsIs(ErrEmpty, ErrNotFound))
}

func errorsIs(a, b error) bool { return a == b }

func TestConcurrent_AddAssignsUniqueSequentialNumbers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Add(ctx, "local", "chan-A", "line", "mod-1")
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	count, err := s.Count(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, n, count)

	quotes, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, quotes, n)
	seen := make(map[int]bool, n)
	for _, q := range quotes {
		assert.False(t, seen[q.Number], "duplicate number %d", q.Number)
		seen[q.Number] = true
	}
	for want := 1; want <= n; want++ {
		assert.True(t, seen[want], "missing number %d (gap)", want)
	}
}

package liveops

import (
	"context"
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
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAdd_NumberIncrementsPerChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(time.Hour)

	for i := 1; i <= 3; i++ {
		e, err := s.Add(ctx, "local", "chan-A", "evt", "", base.Add(time.Duration(i)*time.Hour), nil)
		require.NoError(t, err)
		assert.Equal(t, i, e.Number)
		assert.NotEmpty(t, e.ID)
		assert.False(t, e.CreatedAt.IsZero())
	}

	b, err := s.Add(ctx, "local", "chan-B", "evt", "", base, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, b.Number, "separate channel starts at 1")

	other, err := s.Add(ctx, "other-tenant", "chan-A", "evt", "", base, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, other.Number, "separate tenant starts at 1")
}

func TestAdd_EndsAtRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(time.Hour)
	end := start.Add(2 * time.Hour)

	e, err := s.Add(ctx, "local", "chan-A", "weekend", "double points", start, &end)
	require.NoError(t, err)
	require.NotNil(t, e.EndsAt)
	assert.Equal(t, end.UnixNano(), e.EndsAt.UnixNano())
	assert.Equal(t, "double points", e.Description)

	list, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotNil(t, list[0].EndsAt)
	assert.Equal(t, end.UnixNano(), list[0].EndsAt.UnixNano())
}

func TestDelete_GapPreserved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(time.Hour)

	for i := 1; i <= 3; i++ {
		_, err := s.Add(ctx, "local", "chan-A", "evt", "", base.Add(time.Duration(i)*time.Hour), nil)
		require.NoError(t, err)
	}

	require.NoError(t, s.Delete(ctx, "local", "chan-A", 2))

	e, err := s.Add(ctx, "local", "chan-A", "evt", "", base.Add(9*time.Hour), nil)
	require.NoError(t, err)
	assert.Equal(t, 4, e.Number, "deleting #2 leaves a gap; next is MAX+1=4")
}

func TestDelete_NotFound(t *testing.T) {
	s := newTestStore(t)
	assert.ErrorIs(t, s.Delete(context.Background(), "local", "chan-A", 99), ErrNotFound)
}

func TestNext_EarliestFutureSkipsPast(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Unix(0, 0).UTC().Add(100 * time.Hour)

	_, err := s.Add(ctx, "local", "chan-A", "past", "", now.Add(-time.Hour), nil)
	require.NoError(t, err)
	soon, err := s.Add(ctx, "local", "chan-A", "soon", "", now.Add(2*time.Hour), nil)
	require.NoError(t, err)
	_, err = s.Add(ctx, "local", "chan-A", "later", "", now.Add(5*time.Hour), nil)
	require.NoError(t, err)

	got, err := s.Next(ctx, "local", "chan-A", now)
	require.NoError(t, err)
	assert.Equal(t, soon.ID, got.ID)
	assert.Equal(t, "soon", got.Name)
}

func TestNext_NoneFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Unix(0, 0).UTC().Add(100 * time.Hour)

	_, err := s.Add(ctx, "local", "chan-A", "past", "", now.Add(-time.Hour), nil)
	require.NoError(t, err)

	_, err = s.Next(ctx, "local", "chan-A", now)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpcoming_OrderingActiveAndLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Unix(0, 0).UTC().Add(100 * time.Hour)

	activeEnd := now.Add(time.Hour)
	active, err := s.Add(ctx, "local", "chan-A", "active", "", now.Add(-time.Hour), &activeEnd)
	require.NoError(t, err)
	// A past event whose end has also passed must NOT appear.
	endedEnd := now.Add(-30 * time.Minute)
	_, err = s.Add(ctx, "local", "chan-A", "ended", "", now.Add(-2*time.Hour), &endedEnd)
	require.NoError(t, err)
	// An instantaneous past milestone (no end) must NOT appear.
	_, err = s.Add(ctx, "local", "chan-A", "past-milestone", "", now.Add(-time.Hour), nil)
	require.NoError(t, err)

	f1, err := s.Add(ctx, "local", "chan-A", "future1", "", now.Add(2*time.Hour), nil)
	require.NoError(t, err)
	f2, err := s.Add(ctx, "local", "chan-A", "future2", "", now.Add(3*time.Hour), nil)
	require.NoError(t, err)
	_, err = s.Add(ctx, "local", "chan-A", "future3", "", now.Add(4*time.Hour), nil)
	require.NoError(t, err)

	got, err := s.Upcoming(ctx, "local", "chan-A", now, 3)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, active.ID, got[0].ID, "active event sorts first by StartsAt")
	assert.Equal(t, f1.ID, got[1].ID)
	assert.Equal(t, f2.ID, got[2].ID)
}

func TestUpcoming_DefaultLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Unix(0, 0).UTC().Add(100 * time.Hour)

	for i := 1; i <= 8; i++ {
		_, err := s.Add(ctx, "local", "chan-A", "evt", "", now.Add(time.Duration(i)*time.Hour), nil)
		require.NoError(t, err)
	}

	got, err := s.Upcoming(ctx, "local", "chan-A", now, 0)
	require.NoError(t, err)
	assert.Len(t, got, defaultUpcomingLimit, "limit<=0 falls back to the default")
}

func TestValidation_Errors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(time.Hour)
	before := start.Add(-time.Minute)

	cases := []struct {
		name        string
		evtName     string
		description string
		startsAt    time.Time
		endsAt      *time.Time
	}{
		{"empty name", "", "", start, nil},
		{"whitespace name", "   ", "", start, nil},
		{"name too long", strings.Repeat("x", maxNameLen+1), "", start, nil},
		{"description too long", "ok", strings.Repeat("y", maxDescriptionLen+1), start, nil},
		{"zero startsAt", "ok", "", time.Time{}, nil},
		{"endsAt before startsAt", "ok", "", start, &before},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.Add(ctx, "local", "chan-A", c.evtName, c.description, c.startsAt, c.endsAt)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	_, err := s.Add(ctx, "", "chan-A", "ok", "", start, nil)
	assert.ErrorIs(t, err, ErrInvalid, "empty tenant")
	_, err = s.Add(ctx, "local", "", "ok", "", start, nil)
	assert.ErrorIs(t, err, ErrInvalid, "empty channel")
}

func TestErrors_AreDistinct(t *testing.T) {
	assert.NotEqual(t, ErrNotFound, ErrInvalid)
}

func TestConcurrent_AddDistinctNumbers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(time.Hour)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Add(ctx, "local", "chan-A", "evt", "", base.Add(time.Duration(i)*time.Minute), nil)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	list, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, list, n)

	seen := make(map[int]bool, n)
	for _, e := range list {
		assert.False(t, seen[e.Number], "duplicate number %d", e.Number)
		seen[e.Number] = true
	}
	for i := 1; i <= n; i++ {
		assert.True(t, seen[i], "missing number %d in 1..%d", i, n)
	}
}

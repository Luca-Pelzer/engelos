package queue

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "sq.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleItem(tenant, channel, videoID, title string) Item {
	return Item{
		TenantID:    tenant,
		Channel:     channel,
		VideoID:     videoID,
		Title:       title,
		Artist:      "Some Artist",
		DurationMS:  180000,
		RequestedBy: "viewer-1",
	}
}

func TestEnqueue_AssignsIncreasingPosition(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vid1", "Song One"))
	require.NoError(t, err)
	assert.NotEmpty(t, a.ID)
	assert.Equal(t, StatusQueued, a.Status)
	assert.EqualValues(t, 1, a.Position)
	assert.False(t, a.CreatedAt.IsZero())

	b, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vid2", "Song Two"))
	require.NoError(t, err)
	assert.EqualValues(t, 2, b.Position)

	c, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vid3", "Song Three"))
	require.NoError(t, err)
	assert.EqualValues(t, 3, c.Position)
}

func TestList_FIFOOrderAndLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vid1", "One"))
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, sampleItem("local", "chan-A", "vid2", "Two"))
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, sampleItem("local", "chan-A", "vid3", "Three"))
	require.NoError(t, err)

	all, err := s.List(ctx, "local", "chan-A", 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, "vid1", all[0].VideoID)
	assert.Equal(t, "vid2", all[1].VideoID)
	assert.Equal(t, "vid3", all[2].VideoID)

	limited, err := s.List(ctx, "local", "chan-A", 2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
	assert.Equal(t, "vid1", limited[0].VideoID)
	assert.Equal(t, "vid2", limited[1].VideoID)
}

func TestNext_PromotesOldestQueued(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vid1", "One"))
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, sampleItem("local", "chan-A", "vid2", "Two"))
	require.NoError(t, err)

	first, err := s.Next(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, "vid1", first.VideoID)
	assert.Equal(t, StatusPlaying, first.Status)

	// vid1 is no longer queued, so List drops it.
	remaining, err := s.List(ctx, "local", "chan-A", 0)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "vid2", remaining[0].VideoID)

	second, err := s.Next(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, "vid2", second.VideoID)
	assert.Equal(t, StatusPlaying, second.Status)
}

func TestNext_EmptyReturnsErrEmpty(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Next(context.Background(), "local", "chan-empty")
	assert.ErrorIs(t, err, ErrEmpty)
}

func TestCurrent_ReturnsPlaying(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Current(ctx, "local", "chan-A")
	assert.ErrorIs(t, err, ErrEmpty)

	_, err = s.Enqueue(ctx, sampleItem("local", "chan-A", "vid1", "One"))
	require.NoError(t, err)

	promoted, err := s.Next(ctx, "local", "chan-A")
	require.NoError(t, err)

	cur, err := s.Current(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, promoted.ID, cur.ID)
	assert.Equal(t, "vid1", cur.VideoID)
	assert.Equal(t, StatusPlaying, cur.Status)
}

func TestMarkPlayed_MovesPlayingToPlayed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vid1", "One"))
	require.NoError(t, err)
	cur, err := s.Next(ctx, "local", "chan-A")
	require.NoError(t, err)

	require.NoError(t, s.MarkPlayed(ctx, "local", "chan-A", cur.ID))

	_, err = s.Current(ctx, "local", "chan-A")
	assert.ErrorIs(t, err, ErrEmpty)
}

func TestMarkPlayed_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.MarkPlayed(context.Background(), "local", "chan-A", "missing-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRemove_QueuedItem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vid1", "One"))
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, sampleItem("local", "chan-A", "vid2", "Two"))
	require.NoError(t, err)

	require.NoError(t, s.Remove(ctx, "local", "chan-A", a.ID))

	remaining, err := s.List(ctx, "local", "chan-A", 0)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "vid2", remaining[0].VideoID)
}

func TestRemove_MissingReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Remove(context.Background(), "local", "chan-A", "missing-id")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClear_EmptiesChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vid1", "One"))
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, sampleItem("local", "chan-A", "vid2", "Two"))
	require.NoError(t, err)
	// promote one so a playing item also gets cleared
	_, err = s.Next(ctx, "local", "chan-A")
	require.NoError(t, err)

	require.NoError(t, s.Clear(ctx, "local", "chan-A"))

	queued, err := s.List(ctx, "local", "chan-A", 0)
	require.NoError(t, err)
	assert.Empty(t, queued)

	_, err = s.Current(ctx, "local", "chan-A")
	assert.ErrorIs(t, err, ErrEmpty)
}

func TestPerChannelIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.Enqueue(ctx, sampleItem("local", "chan-A", "vidA", "A"))
	require.NoError(t, err)
	b, err := s.Enqueue(ctx, sampleItem("local", "chan-B", "vidB", "B"))
	require.NoError(t, err)

	// Position is monotonic per channel: each channel starts at 1.
	assert.EqualValues(t, 1, a.Position)
	assert.EqualValues(t, 1, b.Position)

	listA, err := s.List(ctx, "local", "chan-A", 0)
	require.NoError(t, err)
	require.Len(t, listA, 1)
	assert.Equal(t, "vidA", listA[0].VideoID)

	// Next on chan-A must not touch chan-B.
	_, err = s.Next(ctx, "local", "chan-A")
	require.NoError(t, err)

	_, err = s.Current(ctx, "local", "chan-B")
	assert.ErrorIs(t, err, ErrEmpty, "chan-B has nothing playing")

	listB, err := s.List(ctx, "local", "chan-B", 0)
	require.NoError(t, err)
	require.Len(t, listB, 1)
	assert.Equal(t, "vidB", listB[0].VideoID)

	// Tenant isolation too.
	_, err = s.Enqueue(ctx, sampleItem("other-tenant", "chan-A", "vidX", "X"))
	require.NoError(t, err)
	listLocalA, err := s.List(ctx, "local", "chan-A", 0)
	require.NoError(t, err)
	assert.Empty(t, listLocalA, "local chan-A queued is empty after its only item was promoted")
}

func TestChannelNormalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Enqueue(ctx, sampleItem("local", "#Chan-A", "vid1", "One"))
	require.NoError(t, err)

	got, err := s.List(ctx, "local", "chan-a", 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "chan-a", got[0].Channel)
}

func TestEnqueue_Invalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// empty video id
	bad := sampleItem("local", "chan-A", "", "Title")
	_, err := s.Enqueue(ctx, bad)
	assert.ErrorIs(t, err, ErrInvalid)

	// empty title
	bad = sampleItem("local", "chan-A", "vid1", "")
	_, err = s.Enqueue(ctx, bad)
	assert.ErrorIs(t, err, ErrInvalid)

	// negative duration
	bad = sampleItem("local", "chan-A", "vid1", "Title")
	bad.DurationMS = -1
	_, err = s.Enqueue(ctx, bad)
	assert.ErrorIs(t, err, ErrInvalid)

	// empty tenant
	bad = sampleItem("", "chan-A", "vid1", "Title")
	_, err = s.Enqueue(ctx, bad)
	assert.ErrorIs(t, err, ErrInvalid)

	// empty channel
	bad = sampleItem("local", "", "vid1", "Title")
	_, err = s.Enqueue(ctx, bad)
	assert.ErrorIs(t, err, ErrInvalid)

	// bogus status
	bad = sampleItem("local", "chan-A", "vid1", "Title")
	bad.Status = "wizard"
	_, err = s.Enqueue(ctx, bad)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestErrors_AreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrNotFound, ErrEmpty))
	assert.False(t, errors.Is(ErrEmpty, ErrInvalid))
	assert.False(t, errors.Is(ErrInvalid, ErrNotFound))
}

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "sq.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

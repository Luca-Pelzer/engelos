package timers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "timers.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleTimer(tenant, channel, name, message string) Timer {
	return Timer{
		TenantID:     tenant,
		Channel:      channel,
		Name:         name,
		Message:      message,
		Interval:     600 * time.Second,
		MinChatLines: 0,
		Enabled:      true,
		CreatedBy:    "mod-1",
	}
}

func TestCreate_GetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleTimer("local", "chan-A", "rules", "Follow the rules!")
	got, err := s.Create(ctx, in)
	require.NoError(t, err)
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, "rules", got.Name)
	assert.Equal(t, "Follow the rules!", got.Message)
	assert.Equal(t, 600*time.Second, got.Interval)
	assert.True(t, got.Enabled)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())

	fetched, err := s.Get(ctx, "local", "chan-A", "rules")
	require.NoError(t, err)
	assert.Equal(t, got.ID, fetched.ID)
	assert.Equal(t, "Follow the rules!", fetched.Message)
	assert.Equal(t, 600*time.Second, fetched.Interval)
	assert.Equal(t, "mod-1", fetched.CreatedBy)
}

func TestCreate_Duplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleTimer("local", "chan-A", "rules", "hi"))
	require.NoError(t, err)

	_, err = s.Create(ctx, sampleTimer("local", "chan-A", "rules", "hey"))
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

func TestCreate_NameNormalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.Create(ctx, sampleTimer("local", "chan-A", "!Rules", "hi"))
	require.NoError(t, err)
	assert.Equal(t, "rules", got.Name)

	fetched, err := s.Get(ctx, "local", "chan-A", "!RULES")
	require.NoError(t, err)
	assert.Equal(t, got.ID, fetched.ID)
}

func TestCreate_InvalidName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, n := range []string{"", "   ", "has space", "with!bang", "weird$", "name-with-dash"} {
		_, err := s.Create(ctx, sampleTimer("local", "chan-A", n, "x"))
		assert.ErrorIs(t, err, ErrInvalid, "name %q must be invalid", n)
	}
}

func TestCreate_InvalidMessage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleTimer("local", "chan-A", "rules", "   ")
	_, err := s.Create(ctx, in)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestCreate_IntervalBelowFloor(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleTimer("local", "chan-A", "rules", "hi")
	in.Interval = 4 * time.Second
	_, err := s.Create(ctx, in)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestCreate_NegativeMinChatLines(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleTimer("local", "chan-A", "rules", "hi")
	in.MinChatLines = -1
	_, err := s.Create(ctx, in)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestCreate_InvalidTenantChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleTimer("", "chan-A", "rules", "hi"))
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Create(ctx, sampleTimer("local", "", "rules", "hi"))
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "local", "chan-A", "absent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDelete_RoundTripAndNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleTimer("local", "chan-A", "rules", "hi"))
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, "local", "chan-A", "!RULES"))

	_, err = s.Get(ctx, "local", "chan-A", "rules")
	assert.ErrorIs(t, err, ErrNotFound)

	err = s.Delete(ctx, "local", "chan-A", "rules")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdate_MutatesAndBumpsUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, sampleTimer("local", "chan-A", "rules", "hi"))
	require.NoError(t, err)

	time.Sleep(2 * time.Millisecond)

	updated, err := s.Update(ctx, "local", "chan-A", "!rules",
		"New message", 1800*time.Second, 5, false)
	require.NoError(t, err)
	assert.Equal(t, "New message", updated.Message)
	assert.Equal(t, 1800*time.Second, updated.Interval)
	assert.Equal(t, 5, updated.MinChatLines)
	assert.False(t, updated.Enabled)
	assert.True(t, updated.UpdatedAt.After(created.UpdatedAt),
		"UpdatedAt must advance: was %v, now %v", created.UpdatedAt, updated.UpdatedAt)
	assert.Equal(t, created.CreatedAt, updated.CreatedAt, "CreatedAt unchanged")
}

func TestUpdate_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Update(context.Background(), "local", "chan-A", "absent",
		"x", 600*time.Second, 0, true)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdate_InvalidArguments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleTimer("local", "chan-A", "rules", "hi"))
	require.NoError(t, err)

	_, err = s.Update(ctx, "local", "chan-A", "rules", "", 600*time.Second, 0, true)
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Update(ctx, "local", "chan-A", "rules", "x", 1*time.Second, 0, true)
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Update(ctx, "local", "chan-A", "rules", "x", 600*time.Second, -1, true)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestList_OrderedAndScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleTimer("local", "chan-A", "zeta", "z"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleTimer("local", "chan-A", "alpha", "a"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleTimer("local", "chan-A", "mike", "m"))
	require.NoError(t, err)

	_, err = s.Create(ctx, sampleTimer("local", "chan-B", "alpha", "other"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleTimer("other-tenant", "chan-A", "alpha", "x"))
	require.NoError(t, err)

	got, err := s.List(ctx, "local", "chan-A")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "alpha", got[0].Name)
	assert.Equal(t, "mike", got[1].Name)
	assert.Equal(t, "zeta", got[2].Name)

	gotB, err := s.List(ctx, "local", "chan-B")
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, "other", gotB[0].Message)
}

func TestList_EmptyChannel(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List(context.Background(), "local", "chan-empty")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestListEnabled_ExcludesDisabledAndSpansChannels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleTimer("local", "chan-A", "rules", "a"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleTimer("local", "chan-B", "discord", "b"))
	require.NoError(t, err)

	disabled := sampleTimer("local", "chan-A", "off", "x")
	disabled.Enabled = false
	_, err = s.Create(ctx, disabled)
	require.NoError(t, err)

	_, err = s.Create(ctx, sampleTimer("other-tenant", "chan-A", "rules", "nope"))
	require.NoError(t, err)

	got, err := s.ListEnabled(ctx, "local")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "chan-A", got[0].Channel)
	assert.Equal(t, "rules", got[0].Name)
	assert.Equal(t, "chan-B", got[1].Channel)
	assert.Equal(t, "discord", got[1].Name)
	for _, tm := range got {
		assert.NotEqual(t, "off", tm.Name, "disabled timer must be excluded")
	}
}

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "timers.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestErrors_AreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrNotFound, ErrAlreadyExists))
	assert.False(t, errors.Is(ErrAlreadyExists, ErrInvalid))
	assert.False(t, errors.Is(ErrInvalid, ErrNotFound))
}

func TestConcurrent_CreateRace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	var successes, dupes int64
	var mu sync.Mutex

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Create(ctx, sampleTimer("local", "chan-A", "race", "hi"))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrAlreadyExists):
				dupes++
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, successes, "exactly one creator must win")
	assert.EqualValues(t, n-1, dupes, "all losers must see ErrAlreadyExists")
}

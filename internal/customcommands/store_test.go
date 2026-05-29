package customcommands

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
	dsn := filepath.Join(dir, "cc.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleCmd(tenant, channel, name, response string) CustomCommand {
	return CustomCommand{
		TenantID:  tenant,
		Channel:   channel,
		Name:      name,
		Response:  response,
		MinRole:   "everyone",
		CreatedBy: "mod-1",
	}
}

func TestCreate_GetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleCmd("local", "chan-A", "hello", "Welcome $user!")
	got, err := s.Create(ctx, in)
	require.NoError(t, err)
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, "hello", got.Name)
	assert.Equal(t, "Welcome $user!", got.Response)
	assert.Equal(t, "everyone", got.MinRole)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())

	fetched, err := s.Get(ctx, "local", "chan-A", "hello")
	require.NoError(t, err)
	assert.Equal(t, got.ID, fetched.ID)
	assert.Equal(t, "Welcome $user!", fetched.Response)
	assert.Equal(t, "mod-1", fetched.CreatedBy)
}

func TestCreate_Duplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleCmd("local", "chan-A", "hello", "hi"))
	require.NoError(t, err)

	_, err = s.Create(ctx, sampleCmd("local", "chan-A", "hello", "hey"))
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

func TestCreate_NameNormalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.Create(ctx, sampleCmd("local", "chan-A", "!Hello", "hi"))
	require.NoError(t, err)
	assert.Equal(t, "hello", got.Name)

	fetched, err := s.Get(ctx, "local", "chan-A", "!HELLO")
	require.NoError(t, err)
	assert.Equal(t, got.ID, fetched.ID)
}

func TestCreate_DefaultMinRoleEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleCmd("local", "chan-A", "hi", "yo")
	in.MinRole = ""
	got, err := s.Create(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "everyone", got.MinRole)
}

func TestCreate_InvalidName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, n := range []string{"", "   ", "has space", "with!bang", "weird$", "name-with-dash"} {
		_, err := s.Create(ctx, sampleCmd("local", "chan-A", n, "x"))
		assert.ErrorIs(t, err, ErrInvalid, "name %q must be invalid", n)
	}
}

func TestCreate_InvalidResponse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleCmd("local", "chan-A", "hello", "   ")
	_, err := s.Create(ctx, in)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestCreate_InvalidMinRole(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleCmd("local", "chan-A", "hello", "hi")
	in.MinRole = "wizard"
	_, err := s.Create(ctx, in)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestCreate_InvalidTenantChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleCmd("", "chan-A", "hello", "hi"))
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Create(ctx, sampleCmd("local", "", "hello", "hi"))
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

	_, err := s.Create(ctx, sampleCmd("local", "chan-A", "hello", "hi"))
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, "local", "chan-A", "!HELLO"))

	_, err = s.Get(ctx, "local", "chan-A", "hello")
	assert.ErrorIs(t, err, ErrNotFound)

	err = s.Delete(ctx, "local", "chan-A", "hello")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdate_ChangesResponseAndRoleAndBumpsUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, sampleCmd("local", "chan-A", "hello", "hi"))
	require.NoError(t, err)

	// guarantee a measurable UnixNano delta
	time.Sleep(2 * time.Millisecond)

	updated, err := s.Update(ctx, "local", "chan-A", "!hello", "Welcome $user", "moderator")
	require.NoError(t, err)
	assert.Equal(t, "Welcome $user", updated.Response)
	assert.Equal(t, "moderator", updated.MinRole)
	assert.True(t, updated.UpdatedAt.After(created.UpdatedAt),
		"UpdatedAt must advance: was %v, now %v", created.UpdatedAt, updated.UpdatedAt)
	assert.Equal(t, created.CreatedAt, updated.CreatedAt, "CreatedAt unchanged")
}

func TestUpdate_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Update(context.Background(), "local", "chan-A", "absent", "x", "everyone")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdate_InvalidArguments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleCmd("local", "chan-A", "hello", "hi"))
	require.NoError(t, err)

	_, err = s.Update(ctx, "local", "chan-A", "hello", "", "everyone")
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Update(ctx, "local", "chan-A", "hello", "x", "wizard")
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestList_OrderedAndScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleCmd("local", "chan-A", "zeta", "z"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleCmd("local", "chan-A", "alpha", "a"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleCmd("local", "chan-A", "mike", "m"))
	require.NoError(t, err)

	// different channel — must NOT appear in chan-A list
	_, err = s.Create(ctx, sampleCmd("local", "chan-B", "alpha", "other"))
	require.NoError(t, err)
	// different tenant — must NOT appear
	_, err = s.Create(ctx, sampleCmd("other-tenant", "chan-A", "alpha", "x"))
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
	assert.Equal(t, "other", gotB[0].Response)
}

func TestList_EmptyChannel(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List(context.Background(), "local", "chan-empty")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "cc.db")
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
			_, err := s.Create(ctx, sampleCmd("local", "chan-A", "race", "hi"))
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

package rewards

import (
	"context"
	"errors"
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

// newTestStore returns a Store backed by a uniquely-named in-memory
// SQLite database. The shared cache keeps the single pooled connection
// pointed at the same memory DB for the lifetime of the test.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := fmt.Sprintf("file:rewards-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleReward(tenant, channel, name string, cost int64, description string) Reward {
	return Reward{
		TenantID:    tenant,
		Channel:     channel,
		Name:        name,
		Description: description,
		Cost:        cost,
		CreatedBy:   "mod-1",
	}
}

func TestCreate_GetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleReward("local", "chan-a", "coffee", 500, "A hot coffee")
	got, err := s.Create(ctx, in)
	require.NoError(t, err)
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, "coffee", got.Name)
	assert.EqualValues(t, 500, got.Cost)
	assert.Equal(t, "A hot coffee", got.Description)
	assert.Equal(t, "mod-1", got.CreatedBy)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())

	fetched, err := s.Get(ctx, "local", "chan-a", "coffee")
	require.NoError(t, err)
	assert.Equal(t, got.ID, fetched.ID)
	assert.EqualValues(t, 500, fetched.Cost)
	assert.Equal(t, "A hot coffee", fetched.Description)
	assert.Equal(t, "mod-1", fetched.CreatedBy)
}

func TestCreate_Duplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleReward("local", "chan-a", "coffee", 500, "x"))
	require.NoError(t, err)

	_, err = s.Create(ctx, sampleReward("local", "chan-a", "coffee", 999, "y"))
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

func TestCreate_NameAndChannelNormalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.Create(ctx, sampleReward("local", "#Chan-A", "  Coffee  ", 100, "z"))
	require.NoError(t, err)
	assert.Equal(t, "coffee", got.Name)
	assert.Equal(t, "chan-a", got.Channel)

	fetched, err := s.Get(ctx, "local", "CHAN-A", "COFFEE")
	require.NoError(t, err)
	assert.Equal(t, got.ID, fetched.ID)
}

func TestCreate_InvalidName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, n := range []string{"", "   ", "Bad Name", "with!bang", "weird$", "name-with-dash", strings.Repeat("a", maxNameLen+1)} {
		_, err := s.Create(ctx, sampleReward("local", "chan-a", n, 100, "x"))
		assert.ErrorIs(t, err, ErrInvalid, "name %q must be invalid", n)
	}
}

func TestCreate_InvalidCost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleReward("local", "chan-a", "coffee", 0, "x"))
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Create(ctx, sampleReward("local", "chan-a", "coffee", -5, "x"))
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestCreate_DescriptionTooLong(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := sampleReward("local", "chan-a", "coffee", 100, strings.Repeat("x", maxDescriptionLen+1))
	_, err := s.Create(ctx, in)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestCreate_EmptyDescriptionAllowed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.Create(ctx, sampleReward("local", "chan-a", "coffee", 100, ""))
	require.NoError(t, err)
	assert.Equal(t, "", got.Description)
}

func TestCreate_InvalidTenantChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleReward("", "chan-a", "coffee", 100, "x"))
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Create(ctx, sampleReward("local", "", "coffee", 100, "x"))
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "local", "chan-a", "absent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdate_ChangesCostAndDescription(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, sampleReward("local", "chan-a", "coffee", 500, "old"))
	require.NoError(t, err)

	updated, err := s.Update(ctx, "local", "chan-a", "coffee", 750, "new desc")
	require.NoError(t, err)
	assert.EqualValues(t, 750, updated.Cost)
	assert.Equal(t, "new desc", updated.Description)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, created.CreatedAt, updated.CreatedAt, "CreatedAt unchanged")
	assert.False(t, updated.UpdatedAt.Before(created.UpdatedAt), "UpdatedAt must not regress")

	fetched, err := s.Get(ctx, "local", "chan-a", "coffee")
	require.NoError(t, err)
	assert.EqualValues(t, 750, fetched.Cost)
	assert.Equal(t, "new desc", fetched.Description)
}

func TestUpdate_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Update(context.Background(), "local", "chan-a", "absent", 100, "x")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdate_InvalidArguments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleReward("local", "chan-a", "coffee", 500, "x"))
	require.NoError(t, err)

	_, err = s.Update(ctx, "local", "chan-a", "coffee", 0, "x")
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = s.Update(ctx, "local", "chan-a", "coffee", 100, strings.Repeat("x", maxDescriptionLen+1))
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestDelete_RoundTripAndNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Create(ctx, sampleReward("local", "chan-a", "coffee", 500, "x"))
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, "local", "#CHAN-A", "COFFEE"))

	_, err = s.Get(ctx, "local", "chan-a", "coffee")
	assert.ErrorIs(t, err, ErrNotFound)

	err = s.Delete(ctx, "local", "chan-a", "coffee")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestList_OrderedByCostThenName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// insert out of order; expect cost ASC, then name ASC
	_, err := s.Create(ctx, sampleReward("local", "chan-a", "zeta", 100, "z"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleReward("local", "chan-a", "alpha", 100, "a"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleReward("local", "chan-a", "cheap", 50, "c"))
	require.NoError(t, err)
	_, err = s.Create(ctx, sampleReward("local", "chan-a", "pricey", 900, "p"))
	require.NoError(t, err)

	// different channel — must NOT appear in chan-a list
	_, err = s.Create(ctx, sampleReward("local", "chan-b", "coffee", 10, "other"))
	require.NoError(t, err)
	// different tenant — must NOT appear
	_, err = s.Create(ctx, sampleReward("other-tenant", "chan-a", "coffee", 10, "x"))
	require.NoError(t, err)

	got, err := s.List(ctx, "local", "chan-a")
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, "cheap", got[0].Name)  // cost 50
	assert.Equal(t, "alpha", got[1].Name)  // cost 100, name alpha
	assert.Equal(t, "zeta", got[2].Name)   // cost 100, name zeta
	assert.Equal(t, "pricey", got[3].Name) // cost 900

	gotB, err := s.List(ctx, "local", "chan-b")
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	assert.Equal(t, "other", gotB[0].Description)
}

func TestList_EmptyChannel(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List(context.Background(), "local", "chan-empty")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestTimestamps_RoundTripToTheSecond(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.Create(ctx, sampleReward("local", "chan-a", "coffee", 100, "x"))
	require.NoError(t, err)

	fetched, err := s.Get(ctx, "local", "chan-a", "coffee")
	require.NoError(t, err)

	// Stored as Unix seconds: equality must hold exactly after round-trip.
	assert.Equal(t, got.CreatedAt.Unix(), fetched.CreatedAt.Unix())
	assert.Equal(t, got.UpdatedAt.Unix(), fetched.UpdatedAt.Unix())
	assert.True(t, got.CreatedAt.Equal(fetched.CreatedAt), "CreatedAt round-trips to the second")
	assert.True(t, got.UpdatedAt.Equal(fetched.UpdatedAt), "UpdatedAt round-trips to the second")
	assert.Equal(t, time.UTC, fetched.CreatedAt.Location())
}

func TestClose_Idempotent(t *testing.T) {
	dsn := fmt.Sprintf("file:rewards-close-%d?mode=memory&cache=shared", time.Now().UnixNano())
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
			_, err := s.Create(ctx, sampleReward("local", "chan-a", "race", 100, "hi"))
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

package loyalty

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore spins up an isolated in-memory SQLite store. A unique
// nanosecond-stamped name keeps each test's cache:shared namespace private.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := fmt.Sprintf("file:loyalty-%d?mode=memory&cache=shared", time.Now().UnixNano())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestEarn_CreatesAndIncrements(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 10)
	require.NoError(t, err)
	assert.Equal(t, int64(10), a.Balance)
	assert.Equal(t, "v1", a.ViewerID)
	assert.Equal(t, "Alice", a.Username)
	assert.NotEmpty(t, a.ID)
	assert.False(t, a.UpdatedAt.IsZero())

	a, err = s.Earn(ctx, "local", "chan-A", "v1", "AliceRenamed", 5)
	require.NoError(t, err)
	assert.Equal(t, int64(15), a.Balance)
	assert.Equal(t, "AliceRenamed", a.Username, "Earn updates last-seen username")
}

func TestBalance_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Balance(context.Background(), "local", "chan-A", "ghost")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestBalance_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	earned, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 42)
	require.NoError(t, err)

	got, err := s.Balance(ctx, "local", "chan-A", "v1")
	require.NoError(t, err)
	assert.Equal(t, earned.ID, got.ID)
	assert.Equal(t, int64(42), got.Balance)
}

func TestEarn_InvalidAmount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, amt := range []int64{0, -1, -100} {
		_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", amt)
		assert.ErrorIs(t, err, ErrInvalid, "amount %d must be invalid", amt)
	}
}

func TestEarn_InvalidIdentity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "", "chan-A", "v1", "Alice", 1)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Earn(ctx, "local", "  ", "v1", "Alice", 1)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Earn(ctx, "local", "chan-A", "", "Alice", 1)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestSpend_Success(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 100)
	require.NoError(t, err)

	a, err := s.Spend(ctx, "local", "chan-A", "v1", 30)
	require.NoError(t, err)
	assert.Equal(t, int64(70), a.Balance)

	a, err = s.Spend(ctx, "local", "chan-A", "v1", 70)
	require.NoError(t, err)
	assert.Equal(t, int64(0), a.Balance)
}

func TestSpend_InsufficientLeavesBalanceIntact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 20)
	require.NoError(t, err)

	_, err = s.Spend(ctx, "local", "chan-A", "v1", 21)
	assert.ErrorIs(t, err, ErrInsufficient)

	got, err := s.Balance(ctx, "local", "chan-A", "v1")
	require.NoError(t, err)
	assert.Equal(t, int64(20), got.Balance, "balance must be untouched on shortfall")
}

func TestSpend_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Spend(context.Background(), "local", "chan-A", "ghost", 1)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSpend_InvalidAmount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 10)
	require.NoError(t, err)

	for _, amt := range []int64{0, -5} {
		_, err := s.Spend(ctx, "local", "chan-A", "v1", amt)
		assert.ErrorIs(t, err, ErrInvalid, "amount %d must be invalid", amt)
	}
}

func TestTransfer_Success(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 100)
	require.NoError(t, err)
	_, err = s.Earn(ctx, "local", "chan-A", "v2", "Bob", 5)
	require.NoError(t, err)

	from, to, err := s.Transfer(ctx, "local", "chan-A", "v1", "v2", "Bob", 40)
	require.NoError(t, err)
	assert.Equal(t, int64(60), from.Balance)
	assert.Equal(t, int64(45), to.Balance)

	// Funds are conserved: total before (105) == total after.
	assert.Equal(t, int64(105), from.Balance+to.Balance)
}

func TestTransfer_CreatesRecipient(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 100)
	require.NoError(t, err)

	from, to, err := s.Transfer(ctx, "local", "chan-A", "v1", "v2", "Bob", 25)
	require.NoError(t, err)
	assert.Equal(t, int64(75), from.Balance)
	assert.Equal(t, int64(25), to.Balance)
	assert.Equal(t, "Bob", to.Username)
	assert.Equal(t, "v2", to.ViewerID)
	assert.NotEmpty(t, to.ID)
}

func TestTransfer_Insufficient(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 10)
	require.NoError(t, err)

	_, _, err = s.Transfer(ctx, "local", "chan-A", "v1", "v2", "Bob", 11)
	assert.ErrorIs(t, err, ErrInsufficient)

	// Neither side changed.
	from, err := s.Balance(ctx, "local", "chan-A", "v1")
	require.NoError(t, err)
	assert.Equal(t, int64(10), from.Balance)
	_, err = s.Balance(ctx, "local", "chan-A", "v2")
	assert.ErrorIs(t, err, ErrNotFound, "recipient must not be created on a failed transfer")
}

func TestTransfer_SenderMissing(t *testing.T) {
	s := newTestStore(t)
	_, _, err := s.Transfer(context.Background(), "local", "chan-A", "ghost", "v2", "Bob", 5)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestTransfer_SelfInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 100)
	require.NoError(t, err)

	_, _, err = s.Transfer(ctx, "local", "chan-A", "v1", "v1", "Alice", 10)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestTransfer_InvalidAmount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 100)
	require.NoError(t, err)

	for _, amt := range []int64{0, -1} {
		_, _, err := s.Transfer(ctx, "local", "chan-A", "v1", "v2", "Bob", amt)
		assert.ErrorIs(t, err, ErrInvalid, "amount %d must be invalid", amt)
	}
}

func TestLeaderboard_OrderingAndClamp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 50)
	require.NoError(t, err)
	_, err = s.Earn(ctx, "local", "chan-A", "v2", "Bob", 100)
	require.NoError(t, err)
	_, err = s.Earn(ctx, "local", "chan-A", "v3", "Cara", 100) // tie with Bob
	require.NoError(t, err)
	_, err = s.Earn(ctx, "local", "chan-A", "v4", "Dan", 10)
	require.NoError(t, err)
	// Different channel / tenant must not leak in.
	_, err = s.Earn(ctx, "local", "chan-B", "v9", "Zoe", 9999)
	require.NoError(t, err)
	_, err = s.Earn(ctx, "other", "chan-A", "v9", "Zoe", 9999)
	require.NoError(t, err)

	top, err := s.Leaderboard(ctx, "local", "chan-A", 10)
	require.NoError(t, err)
	require.Len(t, top, 4)
	// balance DESC, ties (Bob=Cara=100) broken by Username ASC.
	assert.Equal(t, "Bob", top[0].Username)
	assert.Equal(t, int64(100), top[0].Balance)
	assert.Equal(t, "Cara", top[1].Username)
	assert.Equal(t, int64(100), top[1].Balance)
	assert.Equal(t, "Alice", top[2].Username)
	assert.Equal(t, "Dan", top[3].Username)

	// limit clamps to [1,100].
	one, err := s.Leaderboard(ctx, "local", "chan-A", 0)
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, "Bob", one[0].Username)

	hundred, err := s.Leaderboard(ctx, "local", "chan-A", 9999)
	require.NoError(t, err)
	assert.Len(t, hundred, 4)
}

func TestLeaderboard_Empty(t *testing.T) {
	s := newTestStore(t)
	got, err := s.Leaderboard(context.Background(), "local", "chan-empty", 10)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestChannel_Normalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "#Chan-A", "v1", "Alice", 7)
	require.NoError(t, err)

	// Lookup with a differently-cased, hash-prefixed channel resolves to
	// the same canonical account.
	got, err := s.Balance(ctx, "local", "chan-a", "v1")
	require.NoError(t, err)
	assert.Equal(t, int64(7), got.Balance)
	assert.Equal(t, "chan-a", got.Channel)
}

func TestErrors_AreDistinct(t *testing.T) {
	assert.NotErrorIs(t, ErrNotFound, ErrInvalid)
	assert.NotErrorIs(t, ErrInvalid, ErrInsufficient)
	assert.NotErrorIs(t, ErrInsufficient, ErrNotFound)
}

// TestConcurrent_EarnNoLostUpdates fires 50 goroutines each earning 1; the
// atomic upsert must leave a final balance of exactly 50.
func TestConcurrent_EarnNoLostUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 1)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	got, err := s.Balance(ctx, "local", "chan-A", "v1")
	require.NoError(t, err)
	assert.Equal(t, int64(n), got.Balance)
}

// TestConcurrent_SpendNeverOverdraws funds an account with 50 points then
// fires 100 goroutines each trying to spend 1. Exactly 50 must succeed and
// the balance must bottom out at 0 — never negative.
func TestConcurrent_SpendNeverOverdraws(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Earn(ctx, "local", "chan-A", "v1", "Alice", 50)
	require.NoError(t, err)

	const attempts = 100
	var ok, short int64
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Spend(ctx, "local", "chan-A", "v1", 1)
			switch {
			case err == nil:
				atomic.AddInt64(&ok, 1)
			case errorsIsInsufficient(err):
				atomic.AddInt64(&short, 1)
			default:
				assert.NoError(t, err)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(50), ok, "exactly the funded amount may be spent")
	assert.Equal(t, int64(50), short, "the rest must be rejected as insufficient")

	got, err := s.Balance(ctx, "local", "chan-A", "v1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.Balance, "balance must never go negative")
}

func errorsIsInsufficient(err error) bool {
	return err != nil && strings.Contains(err.Error(), "insufficient")
}

package moments

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

// baseTime is a fixed reference instant used so window/closed checks are
// deterministic instead of depending on time.Now.
var baseTime = time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "moments.db") + "?_pragma=busy_timeout(5000)"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedJoin records n participants on the channel's active moment, each with
// a distinct viewer id/username, using joinTime as the join instant.
func seedJoin(t *testing.T, s Store, tenant, channel string, n int, joinTime time.Time) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		vid := "viewer-" + itoa(i)
		_, err := s.Join(ctx, tenant, channel, vid, "user-"+itoa(i), joinTime)
		require.NoError(t, err)
	}
}

// itoa is a tiny non-strconv int formatter to keep imports minimal.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

func TestOpen_ActiveRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, err := s.Open(ctx, "local", "chan-A", "Drop everything", "mod-1", time.Minute)
	require.NoError(t, err)
	assert.NotEmpty(t, m.ID)
	assert.Equal(t, "local", m.TenantID)
	assert.Equal(t, "chan-a", m.Channel)
	assert.Equal(t, "Drop everything", m.Title)
	assert.Equal(t, "open", m.Status)
	assert.Equal(t, Rarity(""), m.Rarity)
	assert.Equal(t, "mod-1", m.OpenedBy)
	assert.False(t, m.OpenedAt.IsZero())
	assert.True(t, m.ClosesAt.Equal(m.OpenedAt.Add(time.Minute)))
	assert.True(t, m.ClosedAt.IsZero())

	got, err := s.Active(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, m.ID, got.ID)
	assert.Equal(t, "open", got.Status)
	assert.Equal(t, 0, got.Participants)
}

func TestOpen_ChannelNormalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, err := s.Open(ctx, "local", "#Chan-A", "t", "mod", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "chan-a", m.Channel)

	got, err := s.Active(ctx, "local", "CHAN-A")
	require.NoError(t, err)
	assert.Equal(t, m.ID, got.ID)
}

func TestOpen_ActiveExists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Open(ctx, "local", "chan-A", "first", "mod", time.Minute)
	require.NoError(t, err)

	_, err = s.Open(ctx, "local", "chan-A", "second", "mod", time.Minute)
	assert.ErrorIs(t, err, ErrActiveExists)
}

func TestOpen_Invalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Open(ctx, "", "chan-A", "t", "mod", time.Minute)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Open(ctx, "local", "", "t", "mod", time.Minute)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Open(ctx, "local", "chan-A", "", "mod", time.Minute)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Open(ctx, "local", "chan-A", "t", "", time.Minute)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Open(ctx, "local", "chan-A", "t", "mod", 0)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Open(ctx, "local", "chan-A", "t", "mod", -time.Second)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestActive_NoActive(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Active(context.Background(), "local", "chan-A")
	assert.ErrorIs(t, err, ErrNoActive)
}

func TestJoin_IncrementsCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Open(ctx, "local", "chan-A", "t", "mod", time.Hour)
	require.NoError(t, err)

	n, err := s.Join(ctx, "local", "chan-A", "v1", "alice", baseTime)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n, err = s.Join(ctx, "local", "chan-A", "v2", "bob", baseTime)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	got, err := s.Active(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, 2, got.Participants)
}

func TestJoin_Duplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Open(ctx, "local", "chan-A", "t", "mod", time.Hour)
	require.NoError(t, err)

	_, err = s.Join(ctx, "local", "chan-A", "v1", "alice", baseTime)
	require.NoError(t, err)

	_, err = s.Join(ctx, "local", "chan-A", "v1", "alice", baseTime)
	assert.ErrorIs(t, err, ErrAlreadyJoined)
}

func TestJoin_AfterWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, err := s.Open(ctx, "local", "chan-A", "t", "mod", time.Minute)
	require.NoError(t, err)

	// now strictly after ClosesAt -> window elapsed.
	after := m.ClosesAt.Add(time.Second)
	_, err = s.Join(ctx, "local", "chan-A", "v1", "alice", after)
	assert.ErrorIs(t, err, ErrClosed)

	// exactly at ClosesAt is also closed (now >= closes_at).
	_, err = s.Join(ctx, "local", "chan-A", "v2", "bob", m.ClosesAt)
	assert.ErrorIs(t, err, ErrClosed)
}

func TestJoin_NoActive(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Join(context.Background(), "local", "chan-A", "v1", "alice", baseTime)
	assert.ErrorIs(t, err, ErrNoActive)
}

func TestJoin_Invalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.Open(ctx, "local", "chan-A", "t", "mod", time.Hour)
	require.NoError(t, err)

	_, err = s.Join(ctx, "local", "chan-A", "", "alice", baseTime)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = s.Join(ctx, "local", "chan-A", "v1", "", baseTime)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestEnd_RarityTiers(t *testing.T) {
	cases := []struct {
		name    string
		count   int
		want    Rarity
		channel string
	}{
		{"common-zero", 0, RarityCommon, "chan-common"},
		{"rare-threshold", RareThreshold, RarityRare, "chan-rare"},
		{"legendary-threshold", LegendaryThreshold, RarityLegendary, "chan-legendary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			ctx := context.Background()

			_, err := s.Open(ctx, "local", tc.channel, "t", "mod", time.Hour)
			require.NoError(t, err)
			seedJoin(t, s, "local", tc.channel, tc.count, baseTime)

			ended, err := s.End(ctx, "local", tc.channel, baseTime.Add(time.Minute))
			require.NoError(t, err)
			assert.Equal(t, "closed", ended.Status)
			assert.Equal(t, tc.want, ended.Rarity)
			assert.Equal(t, tc.count, ended.Participants)
			assert.False(t, ended.ClosedAt.IsZero())

			// Active afterwards -> ErrNoActive.
			_, err = s.Active(ctx, "local", tc.channel)
			assert.ErrorIs(t, err, ErrNoActive)
		})
	}
}

func TestEnd_NoActive(t *testing.T) {
	s := newTestStore(t)
	_, err := s.End(context.Background(), "local", "chan-A", baseTime)
	assert.ErrorIs(t, err, ErrNoActive)
}

func TestRarityFor(t *testing.T) {
	assert.Equal(t, RarityCommon, RarityFor(0))
	assert.Equal(t, RarityCommon, RarityFor(RareThreshold-1))
	assert.Equal(t, RarityRare, RarityFor(RareThreshold))
	assert.Equal(t, RarityRare, RarityFor(LegendaryThreshold-1))
	assert.Equal(t, RarityLegendary, RarityFor(LegendaryThreshold))
	assert.Equal(t, RarityLegendary, RarityFor(LegendaryThreshold+100))
}

func TestHistory_NewestFirstAndLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Open+End three moments in sequence with increasing closed_at.
	for i, title := range []string{"first", "second", "third"} {
		_, err := s.Open(ctx, "local", "chan-A", title, "mod", time.Hour)
		require.NoError(t, err)
		_, err = s.End(ctx, "local", "chan-A", baseTime.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
	}

	all, err := s.History(ctx, "local", "chan-A", 50)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, "third", all[0].Title)
	assert.Equal(t, "second", all[1].Title)
	assert.Equal(t, "first", all[2].Title)

	// limit clamps to [1,50].
	one, err := s.History(ctx, "local", "chan-A", 0)
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, "third", one[0].Title)

	two, err := s.History(ctx, "local", "chan-A", 2)
	require.NoError(t, err)
	require.Len(t, two, 2)
}

func TestParticipants_Ordered(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, err := s.Open(ctx, "local", "chan-A", "t", "mod", time.Hour)
	require.NoError(t, err)

	_, err = s.Join(ctx, "local", "chan-A", "v1", "alice", baseTime)
	require.NoError(t, err)
	_, err = s.Join(ctx, "local", "chan-A", "v2", "bob", baseTime.Add(time.Second))
	require.NoError(t, err)
	_, err = s.Join(ctx, "local", "chan-A", "v3", "carol", baseTime.Add(2*time.Second))
	require.NoError(t, err)

	names, err := s.Participants(ctx, "local", "chan-A", m.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"alice", "bob", "carol"}, names)
}

func TestParticipants_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Participants(context.Background(), "local", "chan-A", "nope")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPerChannelIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Open(ctx, "local", "chan-A", "A-moment", "mod", time.Hour)
	require.NoError(t, err)
	_, err = s.Open(ctx, "local", "chan-B", "B-moment", "mod", time.Hour)
	require.NoError(t, err)

	a, err := s.Active(ctx, "local", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, "A-moment", a.Title)

	b, err := s.Active(ctx, "local", "chan-B")
	require.NoError(t, err)
	assert.Equal(t, "B-moment", b.Title)

	// Joining chan-A must not affect chan-B's count.
	_, err = s.Join(ctx, "local", "chan-A", "v1", "alice", baseTime)
	require.NoError(t, err)

	bAgain, err := s.Active(ctx, "local", "chan-B")
	require.NoError(t, err)
	assert.Equal(t, 0, bAgain.Participants)

	// Different tenant, same channel name -> independent moment.
	_, err = s.Open(ctx, "other", "chan-A", "other-moment", "mod", time.Hour)
	require.NoError(t, err)
	o, err := s.Active(ctx, "other", "chan-A")
	require.NoError(t, err)
	assert.Equal(t, "other-moment", o.Title)
}

func TestHistory_ScopedToChannel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Open(ctx, "local", "chan-A", "a", "mod", time.Hour)
	require.NoError(t, err)
	_, err = s.End(ctx, "local", "chan-A", baseTime)
	require.NoError(t, err)

	_, err = s.Open(ctx, "local", "chan-B", "b", "mod", time.Hour)
	require.NoError(t, err)
	_, err = s.End(ctx, "local", "chan-B", baseTime)
	require.NoError(t, err)

	histA, err := s.History(ctx, "local", "chan-A", 50)
	require.NoError(t, err)
	require.Len(t, histA, 1)
	assert.Equal(t, "a", histA[0].Title)
}

func TestReopenAfterEnd(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Open(ctx, "local", "chan-A", "first", "mod", time.Hour)
	require.NoError(t, err)
	_, err = s.End(ctx, "local", "chan-A", baseTime)
	require.NoError(t, err)

	// A new moment can be opened once the prior one is closed.
	m2, err := s.Open(ctx, "local", "chan-A", "second", "mod", time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "second", m2.Title)
}

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "moments.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := OpenSQLiteStore(context.Background(), dsn, logger)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestErrors_AreDistinct(t *testing.T) {
	all := []error{ErrActiveExists, ErrNoActive, ErrClosed, ErrAlreadyJoined, ErrNotFound, ErrInvalid}
	for i := range all {
		for j := range all {
			if i == j {
				continue
			}
			assert.False(t, errors.Is(all[i], all[j]),
				"sentinel %v must not match %v", all[i], all[j])
		}
	}
}

func TestConcurrent_OpenRace(t *testing.T) {
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
			_, err := s.Open(ctx, "local", "chan-A", "race", "mod", time.Hour)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrActiveExists):
				dupes++
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, successes, "exactly one opener must win")
	assert.EqualValues(t, n-1, dupes, "all losers must see ErrActiveExists")
}

func TestConcurrent_JoinRace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Open(ctx, "local", "chan-A", "t", "mod", time.Hour)
	require.NoError(t, err)

	const n = 20
	var wg sync.WaitGroup
	var successes, dupes int64
	var mu sync.Mutex

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Same viewer id every time -> idempotent, exactly one wins.
			_, err := s.Join(ctx, "local", "chan-A", "same-viewer", "alice", baseTime)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrAlreadyJoined):
				dupes++
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, successes, "exactly one join must win")
	assert.EqualValues(t, n-1, dupes, "all losers must see ErrAlreadyJoined")
}

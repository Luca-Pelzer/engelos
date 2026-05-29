package streak_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTenant  = "tenant-1"
	testChannel = "chan-A"
	testViewer  = "viewer-1"
	testUser    = "alice"
)

func newStore(t *testing.T) *eventsourcing.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "events.db")
	store, err := eventsourcing.OpenSQLite(
		context.Background(),
		dsn,
		eventsourcing.WithMaxOpenConns(4),
		eventsourcing.WithBusyTimeout(5*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{t: start.UTC()} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Millisecond)
	return c.t
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t.UTC()
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newSystem(t *testing.T, cfg streak.Config) (*streak.System, *fakeClock) {
	t.Helper()
	store := newStore(t)
	s, err := streak.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	clk := newFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	s.WithClock(clk.now)
	return s, clk
}

func newSystemWithStore(t *testing.T, cfg streak.Config) (*streak.System, *fakeClock, *eventsourcing.SQLiteStore) {
	t.Helper()
	store := newStore(t)
	s, err := streak.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	clk := newFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	s.WithClock(clk.now)
	return s, clk, store
}

// noGraceCfg returns a config with grace=0 so day boundaries are exact
// midnights — easier to reason about in most tests.
func noGraceCfg() streak.Config {
	cfg := streak.DefaultConfig()
	cfg.GraceWindow = 0
	return cfg
}

func TestDefaultConfigIsValid(t *testing.T) {
	t.Parallel()
	cfg := streak.DefaultConfig()
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 30, cfg.MaxFreezesHeld)
	assert.Equal(t, 6*time.Hour, cfg.GraceWindow)
	assert.Equal(t, 1, cfg.FreezeMilestones[7])
	assert.Equal(t, 3, cfg.FreezeMilestones[30])
	assert.Equal(t, 7, cfg.FreezeMilestones[100])
	assert.Equal(t, 30, cfg.FreezeMilestones[365])
}

func TestConfigValidate_Rejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mut  func(*streak.Config)
	}{
		{"zero max freezes", func(c *streak.Config) { c.MaxFreezesHeld = 0 }},
		{"negative max freezes", func(c *streak.Config) { c.MaxFreezesHeld = -1 }},
		{"grace >= 24h", func(c *streak.Config) { c.GraceWindow = 24 * time.Hour }},
		{"grace negative", func(c *streak.Config) { c.GraceWindow = -1 * time.Hour }},
		{"milestone non-positive day", func(c *streak.Config) {
			c.FreezeMilestones = map[int]int{0: 1}
		}},
		{"milestone negative award", func(c *streak.Config) {
			c.FreezeMilestones = map[int]int{7: -1}
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := streak.DefaultConfig()
			tc.mut(&cfg)
			assert.Error(t, cfg.Validate())
		})
	}
}

func TestNew_RejectsBadConfig(t *testing.T) {
	t.Parallel()
	_, err := streak.New(streak.Config{}, newStore(t), newSilentLogger())
	require.Error(t, err)
}

func TestNew_RequiresStore(t *testing.T) {
	t.Parallel()
	_, err := streak.New(streak.DefaultConfig(), nil, newSilentLogger())
	require.Error(t, err)
}

func TestNew_DefaultLogger(t *testing.T) {
	t.Parallel()
	s, err := streak.New(streak.DefaultConfig(), newStore(t), nil)
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestSystem_WithClock_NilIgnored(t *testing.T) {
	t.Parallel()
	s, _ := newSystem(t, noGraceCfg())
	s.WithClock(nil)
}

func TestTick_FirstEverEmitsStarted(t *testing.T) {
	t.Parallel()
	s, _ := newSystem(t, noGraceCfg())

	res, err := s.Tick(context.Background(), testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.Equal(t, 1, res.DaysCurrent)
	assert.Equal(t, 1, res.DaysLongest)
	assert.False(t, res.SameDayReTick)
	assert.Equal(t, 0, res.UsedFreezes)

	st := s.Status(testTenant, testChannel, testViewer)
	assert.Equal(t, 1, st.DaysCurrent)
	assert.Equal(t, 7, st.NextMilestone)
}

func TestTick_SameDayReTick(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()

	_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)

	clk.advance(2 * time.Hour)
	res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.True(t, res.SameDayReTick)
	assert.Equal(t, 1, res.DaysCurrent)
}

func TestTick_NextDayAdvances(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()

	clk.set(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)

	clk.set(time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC))
	res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.Equal(t, 2, res.DaysCurrent)
	assert.Equal(t, 2, res.DaysLongest)
	assert.False(t, res.SameDayReTick)
}

func TestTick_MilestoneFiresOnceAt7(t *testing.T) {
	t.Parallel()
	cfg := noGraceCfg()
	s, clk := newSystem(t, cfg)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 7; day++ {
		clk.set(start.AddDate(0, 0, day))
		res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
		if day == 6 {
			assert.Equal(t, 7, res.DaysCurrent)
			assert.Equal(t, 7, res.Milestone)
			assert.Equal(t, 1, res.FreezesAvailable, "1 freeze awarded at day 7")
		} else {
			assert.Equal(t, 0, res.Milestone)
		}
	}

	clk.set(start.AddDate(0, 0, 6).Add(2 * time.Hour))
	res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.True(t, res.SameDayReTick, "same effective day re-tick")
	assert.Equal(t, 0, res.Milestone, "milestone not re-emitted")
}

func TestTick_MilestoneAt30(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var lastRes streak.Result
	for day := 0; day < 30; day++ {
		clk.set(start.AddDate(0, 0, day))
		r, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
		lastRes = r
	}
	assert.Equal(t, 30, lastRes.DaysCurrent)
	assert.Equal(t, 30, lastRes.Milestone)
	assert.Equal(t, 1+3, lastRes.FreezesAvailable, "1 from day7, 3 from day30")
}

func TestTick_SkipOneDayConsumesOneFreeze(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 7; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	// Now viewer has 1 freeze and 7-day streak. Skip day 7, tick on day 8.
	clk.set(start.AddDate(0, 0, 8))
	res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.Equal(t, 8, res.DaysCurrent, "freeze bridged the gap")
	assert.Equal(t, 1, res.UsedFreezes)
	assert.Equal(t, 0, res.FreezesAvailable)
	assert.Equal(t, 0, res.BrokenFromDays)
}

func TestTick_SkipTwoDaysWithThreeFreezes(t *testing.T) {
	t.Parallel()
	cfg := noGraceCfg()
	cfg.FreezeMilestones = map[int]int{3: 3}
	s, clk := newSystem(t, cfg)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 3; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	st := s.Status(testTenant, testChannel, testViewer)
	require.Equal(t, 3, st.FreezesAvailable)

	clk.set(start.AddDate(0, 0, 5))
	res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.Equal(t, 4, res.DaysCurrent, "streak +1, 2 missed days bridged")
	assert.Equal(t, 2, res.UsedFreezes)
	assert.Equal(t, 1, res.FreezesAvailable)
}

func TestTick_SkipTwoDaysWithOneFreezeBreaks(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 7; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	require.Equal(t, 1, s.Status(testTenant, testChannel, testViewer).FreezesAvailable)

	// Skip 2 full days (no tick on day 7 or 8), tick on day 9.
	clk.set(start.AddDate(0, 0, 9))
	res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.Equal(t, 1, res.DaysCurrent, "streak reset")
	assert.Equal(t, 7, res.BrokenFromDays)
	assert.Equal(t, 0, res.UsedFreezes, "freeze preserved per design")
	assert.Equal(t, 1, res.FreezesAvailable)
	assert.Equal(t, 7, res.DaysLongest, "personal best preserved")
}

func TestTick_GraceWindow_SameDay(t *testing.T) {
	t.Parallel()
	cfg := streak.DefaultConfig() // 6h grace
	s, clk := newSystem(t, cfg)
	ctx := context.Background()

	clk.set(time.Date(2026, 5, 29, 22, 0, 0, 0, time.UTC))
	_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)

	clk.set(time.Date(2026, 5, 30, 3, 0, 0, 0, time.UTC))
	res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.True(t, res.SameDayReTick, "03:00 within 6h grace counts as previous day")
	assert.Equal(t, 1, res.DaysCurrent)
}

func TestTick_GraceWindow_PastGraceAdvances(t *testing.T) {
	t.Parallel()
	cfg := streak.DefaultConfig()
	s, clk := newSystem(t, cfg)
	ctx := context.Background()

	clk.set(time.Date(2026, 5, 29, 22, 0, 0, 0, time.UTC))
	_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)

	clk.set(time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC))
	res, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.False(t, res.SameDayReTick, "08:00 past grace counts as new day")
	assert.Equal(t, 2, res.DaysCurrent)
}

func TestTick_RejectsEmptyIdentity(t *testing.T) {
	t.Parallel()
	s, _ := newSystem(t, noGraceCfg())
	ctx := context.Background()
	_, err := s.Tick(ctx, "", testChannel, testViewer, testUser)
	require.Error(t, err)
	_, err = s.Tick(ctx, testTenant, "", testViewer, testUser)
	require.Error(t, err)
	_, err = s.Tick(ctx, testTenant, testChannel, "", testUser)
	require.Error(t, err)
}

func TestUseFreeze_NoFreezesReturnsError(t *testing.T) {
	t.Parallel()
	s, _ := newSystem(t, noGraceCfg())
	ctx := context.Background()
	_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)

	res, err := s.UseFreeze(ctx, testTenant, testChannel, testViewer, testUser)
	assert.ErrorIs(t, err, streak.ErrNoFreezesAvailable)
	assert.Equal(t, 0, res.FreezesAvailable)
}

func TestUseFreeze_ConsumesOne(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 7; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	res, err := s.UseFreeze(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.Equal(t, 1, res.UsedFreezes)
	assert.Equal(t, 0, res.FreezesAvailable)
	assert.Equal(t, 7, res.DaysCurrent, "manual freeze does not advance days")
}

func TestUseFreeze_RejectsEmptyIdentity(t *testing.T) {
	t.Parallel()
	s, _ := newSystem(t, noGraceCfg())
	_, err := s.UseFreeze(context.Background(), "", testChannel, testViewer, testUser)
	require.Error(t, err)
}

func TestReset_ClearsStreak(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 3; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	require.NoError(t, s.Reset(ctx, testTenant, testChannel, testViewer, "admin"))
	st := s.Status(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, st.DaysCurrent)
}

func TestReset_RejectsEmptyIdentity(t *testing.T) {
	t.Parallel()
	s, _ := newSystem(t, noGraceCfg())
	require.Error(t, s.Reset(context.Background(), "", testChannel, testViewer, "x"))
}

func TestLeaderboard_StableSort(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	plan := map[string]int{
		"viewer-a": 3,
		"viewer-b": 5,
		"viewer-c": 5,
		"viewer-d": 1,
	}
	for viewer, days := range plan {
		for d := 0; d < days; d++ {
			clk.set(start.AddDate(0, 0, d))
			_, err := s.Tick(ctx, testTenant, testChannel, viewer, "u")
			require.NoError(t, err)
		}
	}

	board := s.Leaderboard(testTenant, testChannel, 10)
	require.Len(t, board, 4)
	assert.Equal(t, "viewer-b", board[0].ViewerID)
	assert.Equal(t, 5, board[0].DaysCurrent)
	assert.Equal(t, "viewer-c", board[1].ViewerID)
	assert.Equal(t, "viewer-a", board[2].ViewerID)
	assert.Equal(t, "viewer-d", board[3].ViewerID)

	top2 := s.Leaderboard(testTenant, testChannel, 2)
	require.Len(t, top2, 2)
	assert.Equal(t, "viewer-b", top2[0].ViewerID)
	assert.Equal(t, "viewer-c", top2[1].ViewerID)

	assert.Empty(t, s.Leaderboard(testTenant, testChannel, 0))
	assert.Empty(t, s.Leaderboard(testTenant, "no-such-channel", 10))
}

func TestLeaderboard_AllChannels(t *testing.T) {
	t.Parallel()
	s, _ := newSystem(t, noGraceCfg())
	ctx := context.Background()

	_, err := s.Tick(ctx, testTenant, "chan-A", "v1", "u1")
	require.NoError(t, err)
	_, err = s.Tick(ctx, testTenant, "chan-B", "v2", "u2")
	require.NoError(t, err)

	board := s.Leaderboard(testTenant, "", 10)
	require.Len(t, board, 2)
}

func TestStatus_NextMilestone(t *testing.T) {
	t.Parallel()
	s, clk := newSystem(t, noGraceCfg())
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 7; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	st := s.Status(testTenant, testChannel, testViewer)
	assert.Equal(t, 30, st.NextMilestone)
}

func TestStatus_NextMilestoneZeroPastMax(t *testing.T) {
	t.Parallel()
	cfg := noGraceCfg()
	cfg.FreezeMilestones = map[int]int{2: 1}
	s, clk := newSystem(t, cfg)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 3; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	st := s.Status(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, st.NextMilestone)
}

func TestRecover_RebuildsReadModel(t *testing.T) {
	t.Parallel()
	cfg := noGraceCfg()
	store := newStore(t)
	s, err := streak.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	clk := newFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	s.WithClock(clk.now)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 15; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	expected := s.ReadModel().Get(testTenant, testChannel, testViewer)

	s2, err := streak.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	pre := s2.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, pre.DaysCurrent)

	require.NoError(t, s2.Recover(ctx, testTenant))
	got := s2.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, expected.DaysCurrent, got.DaysCurrent)
	assert.Equal(t, expected.DaysLongest, got.DaysLongest)
	assert.Equal(t, expected.FreezesAvailable, got.FreezesAvailable)
}

func TestRecover_RejectsEmptyTenant(t *testing.T) {
	t.Parallel()
	s, _ := newSystem(t, noGraceCfg())
	require.Error(t, s.Recover(context.Background(), ""))
}

func TestRecover_HighVolume(t *testing.T) {
	t.Parallel()
	cfg := noGraceCfg()
	cfg.FreezeMilestones = map[int]int{7: 1, 30: 3, 50: 2}
	store := newStore(t)
	s, err := streak.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	clk := newFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	s.WithClock(clk.now)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	viewers := []string{"v1", "v2", "v3", "v4", "v5"}
	for day := 0; day < 60; day++ {
		clk.set(start.AddDate(0, 0, day))
		for _, v := range viewers {
			_, err := s.Tick(ctx, testTenant, testChannel, v, "u-"+v)
			require.NoError(t, err)
		}
	}
	count, err := store.Count(ctx, eventsourcing.ReadOptions{TenantID: testTenant})
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, int64(300))

	live := map[string]streak.State{}
	for _, v := range viewers {
		live[v] = s.ReadModel().Get(testTenant, testChannel, v)
	}

	s2, err := streak.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	require.NoError(t, s2.Recover(ctx, testTenant))
	for _, v := range viewers {
		got := s2.ReadModel().Get(testTenant, testChannel, v)
		exp := live[v]
		assert.Equal(t, exp.DaysCurrent, got.DaysCurrent, "viewer %s days", v)
		assert.Equal(t, exp.DaysLongest, got.DaysLongest, "viewer %s longest", v)
		assert.Equal(t, exp.FreezesAvailable, got.FreezesAvailable, "viewer %s freezes", v)
	}
}

func TestSystem_ConcurrentDifferentViewers(t *testing.T) {
	t.Parallel()
	cfg := noGraceCfg()
	s, _ := newSystem(t, cfg)
	ctx := context.Background()

	const viewers = 8
	const ops = 20
	var wg sync.WaitGroup
	var failures atomic.Int64
	wg.Add(viewers)
	for v := 0; v < viewers; v++ {
		go func(viewerID string) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				if _, err := s.Tick(ctx, testTenant, testChannel, viewerID, "u"); err != nil {
					failures.Add(1)
					return
				}
			}
		}(viewerLabel(v))
	}
	wg.Wait()
	assert.Equal(t, int64(0), failures.Load())
	for v := 0; v < viewers; v++ {
		state := s.ReadModel().Get(testTenant, testChannel, viewerLabel(v))
		assert.GreaterOrEqual(t, state.DaysCurrent, 1)
	}
}

func TestSystem_ConfigAndReadModelAccessors(t *testing.T) {
	t.Parallel()
	cfg := noGraceCfg()
	cfg.MaxFreezesHeld = 42
	s, _ := newSystem(t, cfg)
	assert.Equal(t, 42, s.Config().MaxFreezesHeld)
	assert.NotNil(t, s.ReadModel())
}

func TestTick_MilestoneCappedAtMaxFreezesHeld(t *testing.T) {
	t.Parallel()
	cfg := noGraceCfg()
	cfg.MaxFreezesHeld = 2
	cfg.FreezeMilestones = map[int]int{3: 5}
	s, clk := newSystem(t, cfg)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 3; day++ {
		clk.set(start.AddDate(0, 0, day))
		_, err := s.Tick(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
	}
	st := s.Status(testTenant, testChannel, testViewer)
	assert.Equal(t, 2, st.FreezesAvailable, "5-award clamped to MaxFreezesHeld=2")
}

func TestErrNoFreezesAvailable_IsExported(t *testing.T) {
	t.Parallel()
	assert.True(t, errors.Is(streak.ErrNoFreezesAvailable, streak.ErrNoFreezesAvailable))
}

func viewerLabel(i int) string {
	return "v-" + string(rune('a'+i))
}

package pity_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
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
	// Advance by a full millisecond so each emitted event lands in its own
	// ULID timestamp bucket - this keeps replay order deterministic
	// regardless of the random entropy embedded in each ULID.
	c.t = c.t.Add(time.Millisecond)
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newSystemWithSeed(t *testing.T, cfg pity.Config, seed int64) (*pity.System, *fakeClock) {
	s, clk, _ := newSystemWithStore(t, cfg, seed)
	return s, clk
}

func newSystemWithStore(t *testing.T, cfg pity.Config, seed int64) (*pity.System, *fakeClock, *eventsourcing.SQLiteStore) {
	t.Helper()
	store := newStore(t)
	s, err := pity.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	clk := newFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	rng := pity.NewSeededRng(seed)
	s.WithRng(rng).WithClock(clk.now).WithSeed(seed)
	return s, clk, store
}

func TestDefaultConfigIsValid(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 1, cfg.PointsPerMessage)
	assert.Equal(t, 100, cfg.HardPityThreshold)
	assert.InDelta(t, 0.7, cfg.SoftPityFraction, 1e-9)
	assert.Equal(t, 70, cfg.SoftPityThreshold())
}

func TestConfigValidate_Rejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mut  func(*pity.Config)
	}{
		{"negative points-per-message", func(c *pity.Config) { c.PointsPerMessage = -1 }},
		{"zero hard pity", func(c *pity.Config) { c.HardPityThreshold = 0 }},
		{"soft fraction zero", func(c *pity.Config) { c.SoftPityFraction = 0 }},
		{"soft fraction one", func(c *pity.Config) { c.SoftPityFraction = 1 }},
		{"base chance > 1", func(c *pity.Config) { c.BaseWinChance = 1.5 }},
		{"base chance < 0", func(c *pity.Config) { c.BaseWinChance = -0.1 }},
		{"negative max window", func(c *pity.Config) { c.MaxPointsPerWindow = -1 }},
		{"window without duration", func(c *pity.Config) { c.WindowDuration = 0 }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := pity.DefaultConfig()
			tc.mut(&cfg)
			assert.Error(t, cfg.Validate())
		})
	}
}

func TestNew_RejectsBadConfig(t *testing.T) {
	t.Parallel()
	_, err := pity.New(pity.Config{}, newStore(t), newSilentLogger())
	require.Error(t, err)
}

func TestNew_RequiresStore(t *testing.T) {
	t.Parallel()
	_, err := pity.New(pity.DefaultConfig(), nil, newSilentLogger())
	require.Error(t, err)
}

func TestEffectiveChance_Tiers(t *testing.T) {
	t.Parallel()
	s, _ := newSystemWithSeed(t, pity.DefaultConfig(), 1)

	assert.InDelta(t, 0.05, s.EffectiveChance(0), 1e-9)
	assert.InDelta(t, 0.05, s.EffectiveChance(50), 1e-9)
	assert.InDelta(t, 0.05, s.EffectiveChance(69), 1e-9)
	// Soft pity (>=70) interpolates.
	assert.InDelta(t, 0.05, s.EffectiveChance(70), 1e-9)
	atSoftBoundary := s.EffectiveChance(85)
	assert.Greater(t, atSoftBoundary, 0.05)
	assert.Less(t, atSoftBoundary, 1.0)
	assert.InDelta(t, 0.05+(1.0-0.05)*float64(99-70)/float64(100-70), s.EffectiveChance(99), 1e-9)
	// Exact hard pity threshold guarantees.
	assert.Equal(t, 1.0, s.EffectiveChance(100))
	assert.Equal(t, 1.0, s.EffectiveChance(250))
}

func TestGrantPoints_AppendsEventAndUpdatesReadModel(t *testing.T) {
	t.Parallel()
	s, _ := newSystemWithSeed(t, pity.DefaultConfig(), 1)
	ctx := context.Background()

	total, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 5)
	require.NoError(t, err)
	assert.Equal(t, 5, total)

	state := s.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 5, state.Points)
	assert.Equal(t, 5, state.PointsThisWindow)
	assert.Equal(t, testUser, state.Username)
	assert.False(t, state.WindowStartedAt.IsZero())
}

func TestGrantPoints_RejectsNegativeAmount(t *testing.T) {
	t.Parallel()
	s, _ := newSystemWithSeed(t, pity.DefaultConfig(), 1)
	_, err := s.GrantPoints(context.Background(), testTenant, testChannel, testViewer, testUser, "msg", -3)
	require.Error(t, err)
}

func TestGrantPoints_RejectsEmptyIdentity(t *testing.T) {
	t.Parallel()
	s, _ := newSystemWithSeed(t, pity.DefaultConfig(), 1)
	_, err := s.GrantPoints(context.Background(), "", testChannel, testViewer, testUser, "msg", 1)
	require.Error(t, err)
	_, err = s.GrantPoints(context.Background(), testTenant, "", testViewer, testUser, "msg", 1)
	require.Error(t, err)
	_, err = s.GrantPoints(context.Background(), testTenant, testChannel, "", testUser, "msg", 1)
	require.Error(t, err)
}

func TestGrantPoints_RateLimitCapsWithinWindow(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.MaxPointsPerWindow = 10
	cfg.WindowDuration = time.Hour
	s, _, store := newSystemWithStore(t, cfg, 1)
	ctx := context.Background()

	total, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 6)
	require.NoError(t, err)
	assert.Equal(t, 6, total)

	total, err = s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 8)
	require.NoError(t, err)
	assert.Equal(t, 10, total, "second grant capped at remaining 4")

	total, err = s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 1)
	require.NoError(t, err)
	assert.Equal(t, 10, total, "further grants are no-ops once cap reached")

	assert.Equal(t, int64(2), rollCountFromStore(t, store, testTenant, pity.EventTypePointsGranted),
		"no event emitted when grant is fully capped to 0")
}

func TestGrantPoints_WindowRollover(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.MaxPointsPerWindow = 5
	cfg.WindowDuration = 10 * time.Minute
	s, clk := newSystemWithSeed(t, cfg, 1)
	ctx := context.Background()

	total, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 5)
	require.NoError(t, err)
	assert.Equal(t, 5, total)

	_, err = s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 1)
	require.NoError(t, err)
	state := s.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 5, state.Points, "no points granted because window cap reached")

	clk.advance(20 * time.Minute)
	total, err = s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 4)
	require.NoError(t, err)
	assert.Equal(t, 9, total, "window reset restored grant capacity")

	state = s.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 4, state.PointsThisWindow)
}

func TestRoll_HardPityGuaranteesAndResets(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.HardPityThreshold = 10
	cfg.MaxPointsPerWindow = 0
	s, _ := newSystemWithSeed(t, cfg, 42)
	ctx := context.Background()

	_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 10)
	require.NoError(t, err)

	res, err := s.Roll(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.True(t, res.Won)
	assert.True(t, res.WasGuaranteed)
	assert.Equal(t, 10, res.PointsBefore)
	assert.Equal(t, 0, res.PointsAfter)
	assert.Equal(t, 1.0, res.EffectiveChance)

	state := s.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, state.Points)
	assert.False(t, state.LastWinAt.IsZero())
}

func TestRoll_NaturalWinResetsPoints(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.HardPityThreshold = 1000
	cfg.BaseWinChance = 1.0
	cfg.MaxPointsPerWindow = 0
	s, _ := newSystemWithSeed(t, cfg, 1)
	ctx := context.Background()

	_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 5)
	require.NoError(t, err)

	res, err := s.Roll(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.True(t, res.Won)
	assert.False(t, res.WasGuaranteed)
	assert.Equal(t, 5, res.PointsBefore)
	assert.Equal(t, 0, res.PointsAfter)

	state := s.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, state.Points)
}

func TestRoll_LossKeepsPoints(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.BaseWinChance = 0.0
	cfg.HardPityThreshold = 1000
	cfg.MaxPointsPerWindow = 0
	s, _ := newSystemWithSeed(t, cfg, 7)
	ctx := context.Background()

	_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 5)
	require.NoError(t, err)

	res, err := s.Roll(ctx, testTenant, testChannel, testViewer, testUser)
	require.NoError(t, err)
	assert.False(t, res.Won)
	assert.Equal(t, 5, res.PointsBefore)
	assert.Equal(t, 5, res.PointsAfter)

	state := s.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 5, state.Points)
}

func TestRoll_SeedDeterminism(t *testing.T) {
	t.Parallel()
	runOnce := func(seed int64) []bool {
		cfg := pity.DefaultConfig()
		cfg.HardPityThreshold = 1000
		cfg.BaseWinChance = 0.5
		cfg.MaxPointsPerWindow = 0
		s, _ := newSystemWithSeed(t, cfg, seed)
		ctx := context.Background()
		_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 1)
		require.NoError(t, err)
		out := make([]bool, 0, 20)
		for i := 0; i < 20; i++ {
			res, err := s.Roll(ctx, testTenant, testChannel, testViewer, testUser)
			require.NoError(t, err)
			out = append(out, res.Won)
			if res.Won {
				_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 1)
				require.NoError(t, err)
			}
		}
		return out
	}

	a := runOnce(12345)
	b := runOnce(12345)
	assert.Equal(t, a, b, "same seed must produce identical outcomes")

	c := runOnce(99)
	assert.NotEqual(t, a, c, "different seeds should diverge")
}

func TestStatus_Tiers(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.MaxPointsPerWindow = 0
	s, _ := newSystemWithSeed(t, cfg, 1)
	ctx := context.Background()

	st := s.Status(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, st.Points)
	assert.False(t, st.SoftPityHit)

	_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 70)
	require.NoError(t, err)
	st = s.Status(testTenant, testChannel, testViewer)
	assert.True(t, st.SoftPityHit)
	assert.False(t, st.NearGuaranteed)

	_, err = s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 30)
	require.NoError(t, err)
	st = s.Status(testTenant, testChannel, testViewer)
	assert.True(t, st.NearGuaranteed)
	assert.Equal(t, 1.0, st.EffectiveChance)
}

func TestReset_ClearsPoints(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.MaxPointsPerWindow = 0
	s, _ := newSystemWithSeed(t, cfg, 1)
	ctx := context.Background()

	_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 42)
	require.NoError(t, err)
	require.NoError(t, s.Reset(ctx, testTenant, testChannel, testViewer, "admin"))

	state := s.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, state.Points)
}

func TestReset_RejectsEmptyIdentity(t *testing.T) {
	t.Parallel()
	s, _ := newSystemWithSeed(t, pity.DefaultConfig(), 1)
	require.Error(t, s.Reset(context.Background(), "", testChannel, testViewer, "x"))
}

func TestRecover_RebuildsReadModel(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.HardPityThreshold = 20
	cfg.MaxPointsPerWindow = 0
	store := newStore(t)
	s, err := pity.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s.WithRng(pity.NewSeededRng(1)).WithClock(clk.now).WithSeed(1)
	ctx := context.Background()

	for i := 0; i < 15; i++ {
		_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 1)
		require.NoError(t, err)
	}
	expected := s.ReadModel().Get(testTenant, testChannel, testViewer)

	// Build a fresh System on top of the same store; its read model is
	// empty until Recover is called.
	s2, err := pity.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	s2.WithClock(clk.now)

	preRecover := s2.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, preRecover.Points, "fresh System starts blank")

	require.NoError(t, s2.Recover(ctx, testTenant))
	got := s2.ReadModel().Get(testTenant, testChannel, testViewer)
	assert.Equal(t, expected.Points, got.Points)
	assert.Equal(t, expected.PointsThisWindow, got.PointsThisWindow)
}

func TestRecover_RejectsEmptyTenant(t *testing.T) {
	t.Parallel()
	s, _ := newSystemWithSeed(t, pity.DefaultConfig(), 1)
	require.Error(t, s.Recover(context.Background(), ""))
}

func TestSystem_ConcurrentDifferentViewers(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.MaxPointsPerWindow = 0
	cfg.HardPityThreshold = 1000
	cfg.BaseWinChance = 0.0
	s, _ := newSystemWithSeed(t, cfg, 1)
	ctx := context.Background()

	const viewers = 8
	const ops = 50
	var wg sync.WaitGroup
	var failures atomic.Int64
	wg.Add(viewers)
	for v := 0; v < viewers; v++ {
		go func(viewerID string) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				if _, err := s.GrantPoints(ctx, testTenant, testChannel, viewerID, "u", "msg", 1); err != nil {
					failures.Add(1)
					return
				}
				if _, err := s.Roll(ctx, testTenant, testChannel, viewerID, "u"); err != nil {
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
		assert.GreaterOrEqual(t, state.Points, 0)
		assert.LessOrEqual(t, state.Points, ops)
	}
}

func TestRoll_RejectsEmptyIdentity(t *testing.T) {
	t.Parallel()
	s, _ := newSystemWithSeed(t, pity.DefaultConfig(), 1)
	_, err := s.Roll(context.Background(), "", testChannel, testViewer, testUser)
	require.Error(t, err)
}

func TestSeededRng_DeterministicAndReseed(t *testing.T) {
	t.Parallel()
	a := pity.NewSeededRng(42)
	b := pity.NewSeededRng(42)
	for i := 0; i < 10; i++ {
		assert.InDelta(t, a.Float64(), b.Float64(), 0)
	}

	c := pity.NewSeededRng(1)
	first := c.Float64()
	c.Seed(1)
	assert.InDelta(t, first, c.Float64(), 0, "reseeding restores the stream")
}

func TestSystem_ConfigAndReadModelAccessors(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.HardPityThreshold = 42
	s, _ := newSystemWithSeed(t, cfg, 1)
	assert.Equal(t, 42, s.Config().HardPityThreshold)
	assert.NotNil(t, s.ReadModel())
}

func TestSystem_WithRng_NilIgnored(t *testing.T) {
	t.Parallel()
	s, _ := newSystemWithSeed(t, pity.DefaultConfig(), 1)
	s.WithRng(nil)
	s.WithClock(nil)
}

func TestCryptoRng_DefaultExercisesProductionPath(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.MaxPointsPerWindow = 0
	cfg.HardPityThreshold = 1000
	cfg.BaseWinChance = 0.0
	s, err := pity.New(cfg, newStore(t), newSilentLogger())
	require.NoError(t, err)
	s.WithSeed(123)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, err := s.GrantPoints(ctx, testTenant, testChannel, testViewer, testUser, "msg", 1)
		require.NoError(t, err)
		res, err := s.Roll(ctx, testTenant, testChannel, testViewer, testUser)
		require.NoError(t, err)
		assert.False(t, res.Won, "with BaseWinChance=0 and below hard pity, never wins")
	}
}

func viewerLabel(i int) string {
	return "v-" + string(rune('a'+i))
}

func rollCountFromStore(t *testing.T, store *eventsourcing.SQLiteStore, tenant, eventType string) int64 {
	t.Helper()
	opts := eventsourcing.ReadOptions{TenantID: tenant, Types: []string{eventType}}
	n, err := store.Count(context.Background(), opts)
	require.NoError(t, err)
	return n
}

package streak_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustEvent(t *testing.T, tenant, eventType string, payload any, occurredAt time.Time) eventsourcing.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	ev, err := eventsourcing.NewEvent(tenant, eventType, raw)
	require.NoError(t, err)
	ev.OccurredAt = occurredAt
	return ev
}

func TestReadModel_GetMissingReturnsZero(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, testTenant, s.TenantID)
	assert.Equal(t, 0, s.DaysCurrent)
}

func TestReadModel_Apply_UnknownEventTypeIgnored(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	ev, err := eventsourcing.NewEvent(testTenant, "some.other.event", json.RawMessage(`{"x":1}`))
	require.NoError(t, err)
	require.NoError(t, rm.Apply(ev))
	assert.Empty(t, rm.Snapshot())
}

func TestReadModel_Apply_RejectsMalformedPayload(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	ev, err := eventsourcing.NewEvent(testTenant, streak.EventTypeStreakStarted,
		json.RawMessage(`{"channel": 7}`))
	require.NoError(t, err)
	assert.Error(t, rm.Apply(ev))
}

func TestReadModel_Apply_StartedSetsBaseline(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakStarted,
		streak.StreakStartedPayload{Channel: testChannel, ViewerID: testViewer, Username: testUser},
		t0)))
	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 1, s.DaysCurrent)
	assert.Equal(t, 1, s.DaysLongest)
	assert.Equal(t, testUser, s.Username)
}

func TestReadModel_Apply_ContinuedUpdatesLongest(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakStarted,
		streak.StreakStartedPayload{Channel: testChannel, ViewerID: testViewer}, t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakContinued,
		streak.StreakContinuedPayload{
			Channel: testChannel, ViewerID: testViewer,
			DaysCurrent: 5, DaysLongest: 5,
		}, t0.Add(24*time.Hour))))
	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 5, s.DaysCurrent)
	assert.Equal(t, 5, s.DaysLongest)
}

func TestReadModel_Apply_BrokenClearsCurrentNotLongest(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakStarted,
		streak.StreakStartedPayload{Channel: testChannel, ViewerID: testViewer}, t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakContinued,
		streak.StreakContinuedPayload{
			Channel: testChannel, ViewerID: testViewer,
			DaysCurrent: 9, DaysLongest: 9,
		}, t0.Add(24*time.Hour))))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakBroken,
		streak.StreakBrokenPayload{
			Channel: testChannel, ViewerID: testViewer,
			DaysAtBreak: 9, MissedDays: 3,
		}, t0.Add(96*time.Hour))))
	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, s.DaysCurrent)
	assert.Equal(t, 9, s.DaysLongest, "personal best preserved across breaks")
}

func TestReadModel_Apply_MilestoneSetsTotals(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakStarted,
		streak.StreakStartedPayload{Channel: testChannel, ViewerID: testViewer}, t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakMilestone,
		streak.StreakMilestonePayload{
			Channel: testChannel, ViewerID: testViewer,
			Milestone: 7, FreezesAwarded: 1, FreezesTotal: 1,
		}, t0.Add(time.Hour))))
	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 1, s.FreezesAvailable)
	assert.True(t, s.MilestonesHit[7])
}

func TestReadModel_Apply_FrozenUpdatesFreezes(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakStarted,
		streak.StreakStartedPayload{Channel: testChannel, ViewerID: testViewer}, t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakFrozen,
		streak.StreakFrozenPayload{
			Channel: testChannel, ViewerID: testViewer,
			DaysBridged: 2, FreezesSpent: 2, FreezesRemain: 1,
			DaysCurrent: 8,
		}, t0.Add(72*time.Hour))))
	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 1, s.FreezesAvailable)
	assert.Equal(t, 8, s.DaysCurrent)
	assert.Equal(t, 8, s.DaysLongest)
}

func TestReadModel_ReplayMatchesLiveSystem(t *testing.T) {
	t.Parallel()
	cfg := streak.DefaultConfig()
	cfg.GraceWindow = 0
	cfg.FreezeMilestones = map[int]int{3: 2, 7: 1, 30: 3}

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

	clk := newFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	s, err := streak.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	s.WithClock(clk.now)

	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	viewers := []string{"v1", "v2", "v3", "v4"}
	for day := 0; day < 40; day++ {
		clk.set(start.AddDate(0, 0, day))
		for i, v := range viewers {
			if day%(i+2) == 0 {
				_, err := s.Tick(ctx, testTenant, testChannel, v, "u-"+v)
				require.NoError(t, err)
			}
		}
	}

	live := map[string]streak.State{}
	for _, v := range viewers {
		live[v] = s.ReadModel().Get(testTenant, testChannel, v)
	}

	replay := streak.NewReadModel()
	var collected []eventsourcing.Event
	for ev, err := range store.Read(ctx, eventsourcing.ReadOptions{TenantID: testTenant}) {
		require.NoError(t, err)
		collected = append(collected, ev)
	}
	sort.SliceStable(collected, func(i, j int) bool {
		if collected[i].OccurredAt.Equal(collected[j].OccurredAt) {
			return collected[i].ID.String() < collected[j].ID.String()
		}
		return collected[i].OccurredAt.Before(collected[j].OccurredAt)
	})
	require.GreaterOrEqual(t, len(collected), 40)
	for _, ev := range collected {
		require.NoError(t, replay.Apply(ev))
	}

	for _, v := range viewers {
		got := replay.Get(testTenant, testChannel, v)
		exp := live[v]
		assert.Equal(t, exp.DaysCurrent, got.DaysCurrent, "viewer %s days", v)
		assert.Equal(t, exp.DaysLongest, got.DaysLongest, "viewer %s longest", v)
		assert.Equal(t, exp.FreezesAvailable, got.FreezesAvailable, "viewer %s freezes", v)
	}
}

func TestReadModel_Leaderboard_MultiTenantIsolation(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	require.NoError(t, rm.Apply(mustEvent(t, "tenant-a", streak.EventTypeStreakStarted,
		streak.StreakStartedPayload{Channel: "c1", ViewerID: "v1", Username: "alice"}, t0)))
	require.NoError(t, rm.Apply(mustEvent(t, "tenant-b", streak.EventTypeStreakStarted,
		streak.StreakStartedPayload{Channel: "c1", ViewerID: "v2", Username: "bob"}, t0)))

	a := rm.Leaderboard("tenant-a", "", 10)
	require.Len(t, a, 1)
	assert.Equal(t, "v1", a[0].ViewerID)

	b := rm.Leaderboard("tenant-b", "", 10)
	require.Len(t, b, 1)
	assert.Equal(t, "v2", b[0].ViewerID)
}

func TestReadModel_GetReturnsDeepCopy(t *testing.T) {
	t.Parallel()
	rm := streak.NewReadModel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakStarted,
		streak.StreakStartedPayload{Channel: testChannel, ViewerID: testViewer}, t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, streak.EventTypeStreakMilestone,
		streak.StreakMilestonePayload{
			Channel: testChannel, ViewerID: testViewer,
			Milestone: 7, FreezesAwarded: 1, FreezesTotal: 1,
		}, t0.Add(time.Hour))))

	got := rm.Get(testTenant, testChannel, testViewer)
	got.MilestonesHit[999] = true
	got.DaysCurrent = 99

	fresh := rm.Get(testTenant, testChannel, testViewer)
	assert.False(t, fresh.MilestonesHit[999], "Get returns deep copy")
	assert.Equal(t, 1, fresh.DaysCurrent)
}

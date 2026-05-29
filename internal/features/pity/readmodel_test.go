package pity_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
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
	rm := pity.NewReadModel()
	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, testTenant, s.TenantID)
	assert.Equal(t, 0, s.Points)
}

func TestReadModel_Apply_UnknownEventTypeIgnored(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	ev, err := eventsourcing.NewEvent(testTenant, "some.other.event", json.RawMessage(`{"x":1}`))
	require.NoError(t, err)
	require.NoError(t, rm.Apply(ev))
	assert.Empty(t, rm.Snapshot())
}

func TestReadModel_Apply_RejectsMalformedPayload(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	ev, err := eventsourcing.NewEvent(testTenant, pity.EventTypePointsGranted, json.RawMessage(`{"amount":"not-an-int"}`))
	require.NoError(t, err)
	assert.Error(t, rm.Apply(ev))
}

func TestReadModel_Apply_PointsGrantedAccumulates(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: testChannel, ViewerID: testViewer, Username: testUser, Amount: 3, NewTotal: 3},
		t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: testChannel, ViewerID: testViewer, Amount: 5, NewTotal: 8},
		t0.Add(time.Second))))

	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 8, s.Points)
	assert.Equal(t, 8, s.PointsThisWindow)
	assert.Equal(t, testUser, s.Username)
}

func TestReadModel_Apply_ResetClearsPoints(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: testChannel, ViewerID: testViewer, Amount: 7, NewTotal: 7},
		t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypeReset,
		pity.ResetPayload{Channel: testChannel, ViewerID: testViewer, Reason: "admin"},
		t0.Add(time.Minute))))
	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 0, s.Points)
}

func TestReadModel_Apply_WindowRolloverFromTimestamps(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel().WithWindowDuration(10 * time.Minute)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: testChannel, ViewerID: testViewer, Amount: 4, NewTotal: 4},
		t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: testChannel, ViewerID: testViewer, Amount: 3, NewTotal: 7},
		t0.Add(5*time.Minute))))

	s := rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 7, s.PointsThisWindow)

	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: testChannel, ViewerID: testViewer, Amount: 2, NewTotal: 9},
		t0.Add(20*time.Minute))))

	s = rm.Get(testTenant, testChannel, testViewer)
	assert.Equal(t, 2, s.PointsThisWindow, "window rolled, only the new grant counts")
	assert.Equal(t, t0.Add(20*time.Minute), s.WindowStartedAt)
}

func TestReadModel_ReplayMatchesLiveSystem(t *testing.T) {
	t.Parallel()
	cfg := pity.DefaultConfig()
	cfg.HardPityThreshold = 15
	cfg.MaxPointsPerWindow = 0

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

	s, err := pity.New(cfg, store, newSilentLogger())
	require.NoError(t, err)
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s.WithRng(pity.NewSeededRng(2026)).WithClock(clk.now).WithSeed(2026)

	ctx := context.Background()
	viewers := []string{"v1", "v2", "v3", "v4"}
	for i := 0; i < 300; i++ {
		v := viewers[i%len(viewers)]
		_, err := s.GrantPoints(ctx, testTenant, testChannel, v, "u-"+v, "msg", 1)
		require.NoError(t, err)
		if i%5 == 0 {
			_, err := s.Roll(ctx, testTenant, testChannel, v, "u-"+v)
			require.NoError(t, err)
		}
	}

	liveStates := indexByViewer(s.ReadModel().Snapshot())

	replay := pity.NewReadModel().WithWindowDuration(cfg.WindowDuration)
	opts := eventsourcing.ReadOptions{TenantID: testTenant}
	var collected []eventsourcing.Event
	for ev, err := range store.Read(ctx, opts) {
		require.NoError(t, err)
		collected = append(collected, ev)
	}
	sort.SliceStable(collected, func(i, j int) bool {
		if collected[i].OccurredAt.Equal(collected[j].OccurredAt) {
			return collected[i].ID.String() < collected[j].ID.String()
		}
		return collected[i].OccurredAt.Before(collected[j].OccurredAt)
	})
	for _, ev := range collected {
		require.NoError(t, replay.Apply(ev))
	}
	count := len(collected)
	assert.GreaterOrEqual(t, count, 300, "should have replayed >= 300 events")
	replayStates := indexByViewer(replay.Snapshot())

	require.Equal(t, len(liveStates), len(replayStates))
	for k, live := range liveStates {
		got, ok := replayStates[k]
		require.True(t, ok, "viewer %s missing in replay", k)
		assert.Equal(t, live.Points, got.Points, "viewer %s points mismatch", k)
		assert.Equal(t, live.LastWinAt.Equal(got.LastWinAt), true, "viewer %s last-win mismatch", k)
	}
}

func TestReadModel_Leaderboard_EmptyAndLimit(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	assert.Empty(t, rm.Leaderboard(testTenant, "", 10))
	assert.Nil(t, rm.Leaderboard(testTenant, "", 0))
	assert.Nil(t, rm.Leaderboard(testTenant, "", -1))
}

func TestReadModel_Leaderboard_OrderingAndTieBreak(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	grant := func(viewerID, username string, total int) {
		require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
			pity.PointsGrantedPayload{
				Channel: testChannel, ViewerID: viewerID, Username: username,
				Amount: total, NewTotal: total,
			}, t0)))
	}

	grant("v-c", "carol", 7)
	grant("v-a", "alice", 9)
	grant("v-b", "bob", 9)
	grant("v-d", "dave", 1)

	got := rm.Leaderboard(testTenant, testChannel, 10)
	require.Len(t, got, 4)
	assert.Equal(t, "v-a", got[0].ViewerID, "9 points, alphabetic tie-break wins")
	assert.Equal(t, 9, got[0].Points)
	assert.Equal(t, "v-b", got[1].ViewerID)
	assert.Equal(t, "v-c", got[2].ViewerID)
	assert.Equal(t, "v-d", got[3].ViewerID)
}

func TestReadModel_Leaderboard_ChannelFilter(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	grant := func(channel, viewerID string, total int) {
		require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
			pity.PointsGrantedPayload{
				Channel: channel, ViewerID: viewerID,
				Amount: total, NewTotal: total,
			}, t0)))
	}

	grant("c1", "v1", 5)
	grant("c1", "v2", 3)
	grant("c2", "v3", 8)

	c1 := rm.Leaderboard(testTenant, "c1", 10)
	require.Len(t, c1, 2)
	assert.Equal(t, "v1", c1[0].ViewerID)
	assert.Equal(t, "c1", c1[0].Channel)

	c2 := rm.Leaderboard(testTenant, "c2", 10)
	require.Len(t, c2, 1)
	assert.Equal(t, "v3", c2[0].ViewerID)

	all := rm.Leaderboard(testTenant, "", 10)
	require.Len(t, all, 3)
	assert.Equal(t, "v3", all[0].ViewerID, "cross-channel: highest points first")
	assert.Equal(t, 8, all[0].Points)
}

func TestReadModel_Leaderboard_ExcludesZeroAndNegative(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: testChannel, ViewerID: "winner", Amount: 4, NewTotal: 4},
		t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: testChannel, ViewerID: "loser", Amount: 5, NewTotal: 5},
		t0)))
	require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypeReset,
		pity.ResetPayload{Channel: testChannel, ViewerID: "loser", Reason: "win"},
		t0.Add(time.Minute))))

	got := rm.Leaderboard(testTenant, testChannel, 10)
	require.Len(t, got, 1)
	assert.Equal(t, "winner", got[0].ViewerID)
}

func TestReadModel_Leaderboard_Truncation(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		require.NoError(t, rm.Apply(mustEvent(t, testTenant, pity.EventTypePointsGranted,
			pity.PointsGrantedPayload{
				Channel: testChannel, ViewerID: fmt.Sprintf("v-%02d", i),
				Amount: 10 - i, NewTotal: 10 - i,
			}, t0)))
	}

	got := rm.Leaderboard(testTenant, testChannel, 2)
	require.Len(t, got, 2)
	assert.Equal(t, "v-00", got[0].ViewerID)
	assert.Equal(t, 10, got[0].Points)
	assert.Equal(t, "v-01", got[1].ViewerID)
	assert.Equal(t, 9, got[1].Points)
}

func TestReadModel_Leaderboard_MultiTenantIsolation(t *testing.T) {
	t.Parallel()
	rm := pity.NewReadModel()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, rm.Apply(mustEvent(t, "tenant-a", pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: "c1", ViewerID: "v1", Username: "alice", Amount: 4, NewTotal: 4},
		t0)))
	require.NoError(t, rm.Apply(mustEvent(t, "tenant-b", pity.EventTypePointsGranted,
		pity.PointsGrantedPayload{Channel: "c1", ViewerID: "v2", Username: "bob", Amount: 9, NewTotal: 9},
		t0)))

	a := rm.Leaderboard("tenant-a", "", 10)
	require.Len(t, a, 1)
	assert.Equal(t, "v1", a[0].ViewerID)

	b := rm.Leaderboard("tenant-b", "", 10)
	require.Len(t, b, 1)
	assert.Equal(t, "v2", b[0].ViewerID)
}

func indexByViewer(states []pity.State) map[string]pity.State {
	out := make(map[string]pity.State, len(states))
	for _, s := range states {
		out[s.ViewerID] = s
	}
	return out
}

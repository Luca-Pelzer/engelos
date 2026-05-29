package pity_test

import (
	"context"
	"encoding/json"
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

func indexByViewer(states []pity.State) map[string]pity.State {
	out := make(map[string]pity.State, len(states))
	for _, s := range states {
		out[s.ViewerID] = s
	}
	return out
}

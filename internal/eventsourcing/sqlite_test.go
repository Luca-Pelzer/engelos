package eventsourcing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/engelos-bot/engelos/internal/eventsourcing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *eventsourcing.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "events.db")
	store, err := eventsourcing.OpenSQLite(context.Background(), dsn,
		eventsourcing.WithMaxOpenConns(4),
		eventsourcing.WithBusyTimeout(5*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustEvent(t *testing.T, tenant, eventType string, payload any) eventsourcing.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	ev, err := eventsourcing.NewEvent(tenant, eventType, raw)
	require.NoError(t, err)
	return ev
}

func collect(t *testing.T, store *eventsourcing.SQLiteStore, opts eventsourcing.ReadOptions) []eventsourcing.Event {
	t.Helper()
	var out []eventsourcing.Event
	for ev, err := range store.Read(context.Background(), opts) {
		require.NoError(t, err)
		out = append(out, ev)
	}
	return out
}

func TestNewEvent_Validation(t *testing.T) {
	_, err := eventsourcing.NewEvent("", "x", nil)
	require.Error(t, err)

	_, err = eventsourcing.NewEvent("tenant", "", nil)
	require.Error(t, err)

	_, err = eventsourcing.NewEvent("tenant", "x", json.RawMessage(`{not json`))
	require.Error(t, err)

	ev, err := eventsourcing.NewEvent("tenant", "x.y", nil)
	require.NoError(t, err)
	require.Equal(t, eventsourcing.CurrentSchemaVersion, ev.Version)
	require.False(t, ev.OccurredAt.IsZero())
	require.Equal(t, "tenant", ev.TenantID)
}

func TestEvent_Chain(t *testing.T) {
	root := mustEvent(t, "tenantA", "root", map[string]string{"k": "v"})
	child, err := root.Chain("child", json.RawMessage(`{"n":1}`))
	require.NoError(t, err)
	require.NotNil(t, child.CorrelationID)
	require.NotNil(t, child.CausationID)
	require.Equal(t, root.ID, *child.CorrelationID)
	require.Equal(t, root.ID, *child.CausationID)

	grand, err := child.Chain("grand", json.RawMessage(`{"n":2}`))
	require.NoError(t, err)
	require.Equal(t, root.ID, *grand.CorrelationID, "correlation propagates")
	require.Equal(t, child.ID, *grand.CausationID, "causation points at immediate parent")
}

func TestAppendAndReadRoundtrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	original := mustEvent(t, "tenantA", "chat.message", map[string]any{"text": "hi"})
	require.NoError(t, store.Append(ctx, original))

	out := collect(t, store, eventsourcing.ReadOptions{TenantID: "tenantA"})
	require.Len(t, out, 1)

	got := out[0]
	assert.Equal(t, original.ID, got.ID)
	assert.Equal(t, original.Type, got.Type)
	assert.Equal(t, original.TenantID, got.TenantID)
	assert.True(t, original.OccurredAt.Equal(got.OccurredAt),
		"occurred_at roundtrip: %v vs %v", original.OccurredAt, got.OccurredAt)
	assert.Equal(t, original.Version, got.Version)
	assert.JSONEq(t, string(original.Payload), string(got.Payload))
}

func TestAppendBatchAtomicity(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	good1 := mustEvent(t, "tenantA", "a", nil)
	good2 := mustEvent(t, "tenantA", "b", nil)
	bad := mustEvent(t, "tenantA", "c", nil)
	bad.TenantID = ""

	err := store.AppendBatch(ctx, []eventsourcing.Event{good1, good2, bad})
	require.Error(t, err, "batch with invalid event must fail")

	n, err := store.Count(ctx, eventsourcing.ReadOptions{TenantID: "tenantA"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "atomic: no events should have been written")

	require.NoError(t, store.AppendBatch(ctx, []eventsourcing.Event{good1, good2}))
	n, err = store.Count(ctx, eventsourcing.ReadOptions{TenantID: "tenantA"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
}

func TestMultiTenantIsolation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, store.Append(ctx, mustEvent(t, "tenantA", "x", map[string]int{"i": i})))
	}
	for i := 0; i < 3; i++ {
		require.NoError(t, store.Append(ctx, mustEvent(t, "tenantB", "x", map[string]int{"i": i})))
	}

	a := collect(t, store, eventsourcing.ReadOptions{TenantID: "tenantA"})
	b := collect(t, store, eventsourcing.ReadOptions{TenantID: "tenantB"})
	assert.Len(t, a, 5)
	assert.Len(t, b, 3)

	for _, ev := range a {
		assert.Equal(t, "tenantA", ev.TenantID)
	}
	for _, ev := range b {
		assert.Equal(t, "tenantB", ev.TenantID)
	}

	_, err := store.Count(ctx, eventsourcing.ReadOptions{})
	require.Error(t, err, "missing tenant id must error")
}

func TestPaginationCursor(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const total = 25
	events := make([]eventsourcing.Event, 0, total)
	for i := 0; i < total; i++ {
		ev := mustEvent(t, "tenantA", "x", map[string]int{"i": i})
		events = append(events, ev)
		require.NoError(t, store.Append(ctx, ev))
		time.Sleep(time.Millisecond)
	}

	pageSize := 10
	var collected []eventsourcing.Event
	cursor := ""
	for {
		page := collect(t, store, eventsourcing.ReadOptions{
			TenantID: "tenantA", AfterID: cursor, Limit: pageSize,
		})
		if len(page) == 0 {
			break
		}
		collected = append(collected, page...)
		cursor = page[len(page)-1].ID.String()
		if len(page) < pageSize {
			break
		}
	}
	require.Len(t, collected, total)
	for i, ev := range collected {
		assert.Equal(t, events[i].ID, ev.ID, "events return in ULID order at index %d", i)
	}
}

func TestTimeRangeFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	base := time.Now().UTC().Add(-1 * time.Hour)
	for i := 0; i < 5; i++ {
		ev := mustEvent(t, "tenantA", "x", nil)
		ev.OccurredAt = base.Add(time.Duration(i) * 10 * time.Minute)
		require.NoError(t, store.Append(ctx, ev))
	}

	after := base.Add(20 * time.Minute)
	before := base.Add(40 * time.Minute)
	got := collect(t, store, eventsourcing.ReadOptions{
		TenantID:       "tenantA",
		OccurredAfter:  after,
		OccurredBefore: before,
	})
	assert.Len(t, got, 2, "events at minutes 20 and 30 fall in [20, 40)")
	for _, ev := range got {
		assert.True(t, !ev.OccurredAt.Before(after))
		assert.True(t, ev.OccurredAt.Before(before))
	}
}

func TestTypeFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	types := []string{"chat.message", "user.sub", "chat.message", "mod.timeout", "user.sub"}
	for _, tp := range types {
		require.NoError(t, store.Append(ctx, mustEvent(t, "tenantA", tp, nil)))
	}

	got := collect(t, store, eventsourcing.ReadOptions{
		TenantID: "tenantA",
		Types:    []string{"chat.message", "user.sub"},
	})
	assert.Len(t, got, 4)
	for _, ev := range got {
		assert.Contains(t, []string{"chat.message", "user.sub"}, ev.Type)
	}

	count, err := store.Count(ctx, eventsourcing.ReadOptions{
		TenantID: "tenantA",
		Types:    []string{"mod.timeout"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestConcurrentAppends(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const writers = 8
	const perWriter = 25
	var wg sync.WaitGroup
	errCh := make(chan error, writers*perWriter)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				ev, err := eventsourcing.NewEvent(
					"tenantA",
					fmt.Sprintf("writer.%d", w),
					[]byte(fmt.Sprintf(`{"w":%d,"i":%d}`, w, i)),
				)
				if err != nil {
					errCh <- err
					return
				}
				if err := store.Append(ctx, ev); err != nil {
					errCh <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	n, err := store.Count(ctx, eventsourcing.ReadOptions{TenantID: "tenantA"})
	require.NoError(t, err)
	assert.Equal(t, int64(writers*perWriter), n,
		"concurrent appends must not lose any events")

	seen := map[string]struct{}{}
	for ev, err := range store.Read(ctx, eventsourcing.ReadOptions{TenantID: "tenantA"}) {
		require.NoError(t, err)
		_, dup := seen[ev.ID.String()]
		require.False(t, dup, "duplicate id: %s", ev.ID)
		seen[ev.ID.String()] = struct{}{}
	}
	assert.Len(t, seen, writers*perWriter)
}

func TestReadOptionsValidation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, err := range readErrs(store, ctx, eventsourcing.ReadOptions{}) {
		require.Error(t, err)
	}
	for _, err := range readErrs(store, ctx, eventsourcing.ReadOptions{TenantID: "t", AfterID: "not-a-ulid"}) {
		require.Error(t, err)
	}
	for _, err := range readErrs(store, ctx, eventsourcing.ReadOptions{TenantID: "t", Limit: -1}) {
		require.Error(t, err)
	}
}

func readErrs(store *eventsourcing.SQLiteStore, ctx context.Context, opts eventsourcing.ReadOptions) []error {
	var errs []error
	for _, err := range store.Read(ctx, opts) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func TestMigrationsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "events.db")

	s1, err := eventsourcing.OpenSQLite(context.Background(), dsn)
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	s2, err := eventsourcing.OpenSQLite(context.Background(), dsn)
	require.NoError(t, err)
	require.NoError(t, s2.Close())
}

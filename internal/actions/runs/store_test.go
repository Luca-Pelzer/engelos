package runs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestStore(t *testing.T, capRuns int) *Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "runs.db")
	s, err := OpenStore(context.Background(), dsn, capRuns, discardLogger())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func traceWith(id, tenant, channel, rule, status string, nodes []actions.RunNode) actions.RunTrace {
	now := time.Now().UTC()
	return actions.RunTrace{
		ID: id, TenantID: tenant, Channel: channel, RuleName: rule,
		TriggerKind: "manual", TriggerSummary: "manual",
		StartedAt: now, FinishedAt: now, Status: status, Nodes: nodes,
	}
}

func TestStore_OpenRejectsNonPositiveCap(t *testing.T) {
	_, err := OpenStore(context.Background(), filepath.Join(t.TempDir(), "x.db"), 0, discardLogger())
	require.Error(t, err)
}

func TestStore_RecordAndGetOrderedNodes(t *testing.T) {
	s := newTestStore(t, 500)
	ctx := context.Background()
	id := actions.NewRunID()
	tr := traceWith(id, "local", "chan", "r1", actions.RunStatusPartial, []actions.RunNode{
		{Seq: 0, NodeKind: "condition", TypeID: "cond:regex", Status: "ok"},
		{Seq: 1, NodeKind: "action", TypeID: "builtin:log", Status: "ok", DurationMs: 5},
		{Seq: 2, NodeKind: "action", TypeID: "http:request", Status: "fail", DurationMs: 3, Error: "boom"},
	})
	require.NoError(t, s.Record(ctx, tr))

	got, err := s.GetRun(ctx, "local", "chan", "r1", id)
	require.NoError(t, err)
	assert.Equal(t, actions.RunStatusPartial, got.Status)
	assert.Equal(t, 3, got.NodeCount)
	require.Len(t, got.Nodes, 3)
	assert.Equal(t, "cond:regex", got.Nodes[0].TypeID)
	assert.Equal(t, "builtin:log", got.Nodes[1].TypeID)
	assert.Equal(t, "http:request", got.Nodes[2].TypeID)
	assert.Equal(t, "boom", got.Nodes[2].Error)
}

func TestStore_GetRunNotFound(t *testing.T) {
	s := newTestStore(t, 500)
	_, err := s.GetRun(context.Background(), "local", "chan", "r1", "nope")
	assert.ErrorIs(t, err, actions.ErrRunNotFound)
}

func TestStore_ListNewestFirst(t *testing.T) {
	s := newTestStore(t, 500)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("run-%03d", i)
		require.NoError(t, s.Record(ctx, traceWith(id, "local", "chan", "r1", actions.RunStatusOK, nil)))
	}
	list, err := s.ListRuns(ctx, "local", "chan", "r1", 50)
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, "run-002", list[0].ID)
	assert.Equal(t, "run-001", list[1].ID)
	assert.Equal(t, "run-000", list[2].ID)
	assert.Nil(t, list[0].Nodes, "list omits node entries")
}

func TestStore_RetentionCap(t *testing.T) {
	capN := 10
	s := newTestStore(t, capN)
	ctx := context.Background()
	for i := 0; i < capN+20; i++ {
		id := fmt.Sprintf("run-%05d", i)
		require.NoError(t, s.Record(ctx, traceWith(id, "local", "chan", "r1", actions.RunStatusOK, nil)))
	}
	n, err := s.CountRuns(ctx, "local", "chan")
	require.NoError(t, err)
	assert.Equal(t, capN, n)

	_, err = s.GetRun(ctx, "local", "chan", "r1", "run-00000")
	assert.ErrorIs(t, err, actions.ErrRunNotFound, "oldest evicted")
	_, err = s.GetRun(ctx, "local", "chan", "r1", "run-00029")
	require.NoError(t, err, "newest retained")
}

func TestStore_DeleteClears(t *testing.T) {
	s := newTestStore(t, 500)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		require.NoError(t, s.Record(ctx, traceWith(fmt.Sprintf("run-%03d", i), "local", "chan", "r1", actions.RunStatusOK, nil)))
	}
	n, err := s.DeleteRuns(ctx, "local", "chan", "r1")
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	list, _ := s.ListRuns(ctx, "local", "chan", "r1", 50)
	assert.Empty(t, list)
}

func TestStore_DeleteCascadesNodes(t *testing.T) {
	s := newTestStore(t, 500)
	ctx := context.Background()
	node := []actions.RunNode{{Seq: 0, NodeKind: "action", TypeID: "builtin:log", Status: "ok"}}
	require.NoError(t, s.Record(ctx, traceWith("run-x", "local", "chan", "r1", actions.RunStatusOK, node)))
	_, err := s.DeleteRuns(ctx, "local", "chan", "r1")
	require.NoError(t, err)
	// Re-inserting the same run id must not collide on the child (run_id, seq)
	// key, proving the node rows were cascade-deleted.
	require.NoError(t, s.Record(ctx, traceWith("run-x", "local", "chan", "r1", actions.RunStatusOK, node)))
}

func TestStore_ChannelIsolation(t *testing.T) {
	s := newTestStore(t, 500)
	ctx := context.Background()
	id := actions.NewRunID()
	require.NoError(t, s.Record(ctx, traceWith(id, "local", "chanA", "r1", actions.RunStatusOK, nil)))

	_, err := s.GetRun(ctx, "local", "chanB", "r1", id)
	assert.ErrorIs(t, err, actions.ErrRunNotFound)
	list, _ := s.ListRuns(ctx, "local", "chanB", "r1", 50)
	assert.Empty(t, list)
}

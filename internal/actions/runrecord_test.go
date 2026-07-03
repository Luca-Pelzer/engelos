package actions

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTraceBuilder_StatusOK(t *testing.T) {
	tb := newTraceBuilder(sampleRule("ch", "r"), Trigger{Kind: TriggerManual})
	tb.addAction("a1", nodeStatusOK, nil, time.Millisecond, nil)
	tb.addAction("a2", nodeStatusOK, nil, time.Millisecond, nil)
	tr := tb.finish()
	assert.Equal(t, RunStatusOK, tr.Status)
	assert.Equal(t, 2, tr.NodeCount)
	assert.False(t, tr.FinishedAt.Before(tr.StartedAt))
}

func TestTraceBuilder_StatusPartial(t *testing.T) {
	tb := newTraceBuilder(sampleRule("ch", "r"), Trigger{Kind: TriggerManual})
	tb.addAction("a1", nodeStatusOK, nil, 0, nil)
	tb.addAction("a2", nodeStatusFail, errors.New("boom"), 0, nil)
	assert.Equal(t, RunStatusPartial, tb.finish().Status)
}

func TestTraceBuilder_StatusErrorWhenAllFail(t *testing.T) {
	tb := newTraceBuilder(sampleRule("ch", "r"), Trigger{Kind: TriggerManual})
	tb.addAction("a1", nodeStatusFail, errors.New("x"), 0, nil)
	tb.addAction("a2", nodeStatusFail, errors.New("y"), 0, nil)
	assert.Equal(t, RunStatusError, tb.finish().Status)
}

func TestTraceBuilder_StatusStopped(t *testing.T) {
	tb := newTraceBuilder(sampleRule("ch", "r"), Trigger{Kind: TriggerManual})
	tb.addAction("a1", nodeStatusOK, nil, 0, nil)
	tb.markStopped([]ActionInstance{
		{TypeID: "a2", Enabled: true},
		{TypeID: "a3", Enabled: false},
	})
	tr := tb.finish()
	assert.Equal(t, RunStatusStopped, tr.Status)
	assert.Equal(t, nodeStatusOK, tr.Nodes[0].Status)
	assert.Equal(t, nodeStatusStopped, tr.Nodes[1].Status)
	assert.Equal(t, nodeStatusSkipped, tr.Nodes[2].Status)
}

func TestTraceBuilder_ConditionEntriesDoNotAffectStatus(t *testing.T) {
	tb := newTraceBuilder(sampleRule("ch", "r"), Trigger{Kind: TriggerManual})
	tb.addCondition("cond:regex", true, time.Millisecond)
	tb.addCondition("cond:output", false, time.Millisecond)
	tb.addAction("a1", nodeStatusOK, nil, 0, nil)
	tr := tb.finish()
	assert.Equal(t, nodeKindCondition, tr.Nodes[0].NodeKind)
	assert.Equal(t, nodeStatusOK, tr.Nodes[0].Status)
	assert.Equal(t, nodeStatusFail, tr.Nodes[1].Status)
	assert.Equal(t, nodeKindAction, tr.Nodes[2].NodeKind)
	// A failed CONDITION never makes the run partial - only failed actions do.
	assert.Equal(t, RunStatusOK, tr.Status)
}

func TestTraceBuilder_ErrorTruncated(t *testing.T) {
	tb := newTraceBuilder(sampleRule("ch", "r"), Trigger{Kind: TriggerManual})
	tb.addAction("a1", nodeStatusFail, errors.New(strings.Repeat("x", errorMaxRunes+100)), 0, nil)
	got := tb.finish().Nodes[0].Error
	assert.Equal(t, errorMaxRunes+1, len([]rune(got))) // 300 runes + ellipsis
	assert.True(t, strings.HasSuffix(got, "…"))
}

func TestTraceBuilder_UsesPreallocatedRunID(t *testing.T) {
	tb := newTraceBuilder(sampleRule("ch", "r"), Trigger{Kind: TriggerManual, RunID: "run-preallocated"})
	assert.Equal(t, "run-preallocated", tb.finish().ID)
}

func TestTraceBuilder_MintsRunIDWhenAbsent(t *testing.T) {
	tb := newTraceBuilder(sampleRule("ch", "r"), Trigger{Kind: TriggerManual})
	assert.NotEmpty(t, tb.finish().ID)
}

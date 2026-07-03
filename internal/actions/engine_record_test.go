package actions

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureRecorder struct {
	mu     sync.Mutex
	traces []RunTrace
}

func (c *captureRecorder) Record(_ context.Context, tr RunTrace) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.traces = append(c.traces, tr)
	return nil
}

func (c *captureRecorder) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.traces)
}

func (c *captureRecorder) first() RunTrace {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.traces[0]
}

func commandRule(name, command string, conds ConditionList, acts []ActionInstance) Rule {
	return Rule{
		TenantID:      "local",
		Channel:       "chan",
		Name:          name,
		Enabled:       true,
		TriggerKind:   TriggerCommand,
		TriggerFilter: json.RawMessage(`{"command":"` + command + `"}`),
		Conditions:    conds,
		Actions:       ActionList{Actions: acts},
	}
}

func newRecordingEngine(t *testing.T, rec RunRecorder, rules ...Rule) (*Engine, *recordingChat) {
	t.Helper()
	reg := NewRegistry()
	chat := &recordingChat{}
	require.NoError(t, RegisterBuiltins(reg, Services{Chat: chat}))
	eng, err := New(Config{
		TenantID: "local",
		Source:   &staticSource{rules: rules},
		Registry: reg,
		Recorder: rec,
		Logger:   discardLogger(),
	})
	require.NoError(t, err)
	eng.Start()
	t.Cleanup(eng.Stop)
	return eng, chat
}

func TestEngine_RecordsFiring(t *testing.T) {
	rec := &captureRecorder{}
	rule := commandRule("greet", "test",
		ConditionList{Mode: ConditionModeAll},
		[]ActionInstance{{TypeID: "builtin:send-chat", Enabled: true, Config: json.RawMessage(`{"text":"hi"}`)}})
	eng, chat := newRecordingEngine(t, rec, rule)

	eng.Fire(context.Background(), Trigger{Kind: TriggerCommand, Channel: "chan", Text: "!test"})

	require.Eventually(t, func() bool { return rec.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	tr := rec.first()
	assert.Equal(t, RunStatusOK, tr.Status)
	assert.Equal(t, "greet", tr.RuleName)
	assert.Equal(t, "command:test", tr.TriggerSummary)
	require.Len(t, tr.Nodes, 1)
	assert.Equal(t, "builtin:send-chat", tr.Nodes[0].TypeID)
	assert.Len(t, chat.messages(), 1)
}

func TestEngine_ConditionsFalseRecordsNothing(t *testing.T) {
	rec := &captureRecorder{}
	conds := ConditionList{Mode: ConditionModeAll, Conditions: []ConditionInstance{
		{TypeID: "cond:regex", Config: json.RawMessage(`{"pattern":"willnotmatch"}`)},
	}}
	rule := commandRule("gated", "test", conds,
		[]ActionInstance{{TypeID: "builtin:send-chat", Enabled: true, Config: json.RawMessage(`{"text":"hi"}`)}})
	eng, chat := newRecordingEngine(t, rec, rule)

	eng.Fire(context.Background(), Trigger{Kind: TriggerCommand, Channel: "chan", Text: "!test"})

	time.Sleep(250 * time.Millisecond)
	assert.Equal(t, 0, rec.count(), "conditions-false rules record nothing")
	assert.Empty(t, chat.messages(), "conditions-false rules run no actions")
}

func TestEngine_IncludesConditionEntriesInRecordedRun(t *testing.T) {
	rec := &captureRecorder{}
	conds := ConditionList{Mode: ConditionModeAll, Conditions: []ConditionInstance{
		{TypeID: "cond:regex", Config: json.RawMessage(`{"pattern":"test"}`)},
	}}
	rule := commandRule("gated", "test", conds,
		[]ActionInstance{{TypeID: "builtin:send-chat", Enabled: true, Config: json.RawMessage(`{"text":"hi"}`)}})
	eng, _ := newRecordingEngine(t, rec, rule)

	eng.Fire(context.Background(), Trigger{Kind: TriggerCommand, Channel: "chan", Text: "!test"})

	require.Eventually(t, func() bool { return rec.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	tr := rec.first()
	require.Len(t, tr.Nodes, 2)
	assert.Equal(t, nodeKindCondition, tr.Nodes[0].NodeKind)
	assert.Equal(t, "cond:regex", tr.Nodes[0].TypeID)
	assert.Equal(t, nodeKindAction, tr.Nodes[1].NodeKind)
}

func TestEngine_NilRecorderStillRuns(t *testing.T) {
	rule := commandRule("greet", "test",
		ConditionList{Mode: ConditionModeAll},
		[]ActionInstance{{TypeID: "builtin:send-chat", Enabled: true, Config: json.RawMessage(`{"text":"hi"}`)}})
	eng, chat := newRecordingEngine(t, nil, rule)

	eng.Fire(context.Background(), Trigger{Kind: TriggerCommand, Channel: "chan", Text: "!test"})

	require.Eventually(t, func() bool { return len(chat.messages()) == 1 }, 2*time.Second, 5*time.Millisecond)
}

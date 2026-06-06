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

// ctxWaitAction waits for a fixed delay, racing it against ec.Ctx.Done. It
// records whether the delay elapsed ("waited") or the context fired first
// ("cancelled"). It proves the per-action timeout context handed to one action
// does not leak into and pre-cancel the next action.
type ctxWaitAction struct {
	id    string
	delay time.Duration
	ch    chan<- string
}

func (a ctxWaitAction) Definition() PluginDefinition {
	return PluginDefinition{ID: a.id, Name: "ctx-wait", Description: "test action that waits"}
}

func (a ctxWaitAction) Execute(ec *ExecutionContext, _ json.RawMessage) (*ActionResult, error) {
	var result string
	select {
	case <-time.After(a.delay):
		result = "waited"
	case <-ec.Ctx.Done():
		result = "cancelled"
	}
	select {
	case a.ch <- result:
	case <-time.After(2 * time.Second):
	}
	return nil, nil
}

// TestEngineFireRacingStopNoPanic loops Fire in several goroutines while Stop
// closes the engine. Regression for A1: Stop must not close the jobs channel or
// the per-name queues, so an in-flight send can never panic on a closed
// channel. Run under -race to surface the data race the old code carried.
func TestEngineFireRacingStopNoPanic(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: make(chan capturedExec, 1024)}))

	poolRule := sampleRule("racechan", "pool-rule")
	poolRule.Actions = ActionList{
		Actions: []ActionInstance{{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)}},
	}
	queueRule := sampleRule("racechan", "queue-rule")
	queueRule.Actions = ActionList{
		QueueID: "race-queue",
		Actions: []ActionInstance{{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)}},
	}

	src := &staticSource{rules: []Rule{poolRule, queueRule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       4,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "racechan"})
			}
		}()
	}

	time.Sleep(2 * time.Millisecond)
	eng.Stop()
	wg.Wait()
}

// TestEngineSecondActionContextNotCancelled proves the second action in a list
// gets a fresh, live context rather than one already cancelled by the first
// action's cancel(). Regression for A3: the old code reassigned ec.Ctx to the
// first action's timeout context, which cancel() then poisoned, so the second
// delay-style action saw a done context and returned immediately.
func TestEngineSecondActionContextNotCancelled(t *testing.T) {
	results := make(chan string, 4)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(ctxWaitAction{id: "test:wait", delay: 60 * time.Millisecond, ch: results}))

	rule := sampleRule("waitchan", "two-delays")
	rule.Actions = ActionList{
		Actions: []ActionInstance{
			{TypeID: "test:wait", Enabled: true, Config: json.RawMessage(`{}`)},
			{TypeID: "test:wait", Enabled: true, Config: json.RawMessage(`{}`)},
		},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "waitchan"})

	for i := 0; i < 2; i++ {
		r, ok := waitForString(t, results, 2*time.Second)
		require.True(t, ok, "expected action %d to report", i+1)
		assert.Equal(t, "waited", r, "action %d context must not be pre-cancelled", i+1)
	}
}

// TestEngineNoneModeUnknownConditionDoesNotFire confirms a none-mode rule whose
// single condition type is unregistered fails closed. Regression for A4: the
// old code special-cased only the all mode and fell through to a default that
// returned true, letting a deleted or renamed condition silently open the gate.
func TestEngineNoneModeUnknownConditionDoesNotFire(t *testing.T) {
	ch := make(chan capturedExec, 4)
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: ch}))

	rule := sampleRule("nonechan", "none-unknown")
	rule.Conditions = ConditionList{
		Mode: ConditionModeNone,
		Conditions: []ConditionInstance{
			{TypeID: "nonexistent:condition", Config: json.RawMessage(`{}`)},
		},
	}
	rule.Actions = ActionList{
		Actions: []ActionInstance{{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)}},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	defer eng.Stop()

	eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "nonechan"})

	_, ok := waitForExec(t, ch, 500*time.Millisecond)
	assert.False(t, ok, "none-mode rule with unknown condition must not fire")
}

// TestEngineSerialFireAfterStopNoDeadlock fires a queue-routed rule after Stop.
// Regression for A1/A2: enqueueSerial must observe the closed stopped channel
// and return promptly instead of blocking forever or panicking.
func TestEngineSerialFireAfterStopNoDeadlock(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAction(captureAction{id: "test:capture", ch: make(chan capturedExec, 4)}))

	rule := sampleRule("serialchan", "serial-after-stop")
	rule.Actions = ActionList{
		QueueID: "after-stop-queue",
		Actions: []ActionInstance{{TypeID: "test:capture", Enabled: true, Config: json.RawMessage(`{}`)}},
	}

	src := &staticSource{rules: []Rule{rule}}
	eng, err := New(Config{
		Source:        src,
		Registry:      reg,
		Logger:        discardLogger(),
		Workers:       1,
		ActionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	eng.Start()
	eng.Stop()

	done := make(chan struct{})
	go func() {
		eng.Fire(context.Background(), Trigger{Kind: TriggerEvent, Channel: "serialchan"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Fire after Stop deadlocked on serial queue")
	}
}

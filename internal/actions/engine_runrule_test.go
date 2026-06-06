package actions

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingAction records how many times it executed and which rule fired it so
// a test can assert RunRule ran exactly the targeted rule.
type countingAction struct {
	calls *int64
	names *sync.Map
}

func (countingAction) Definition() PluginDefinition {
	return PluginDefinition{ID: "test:count", Name: "Count", Description: "test"}
}

func (a countingAction) Execute(ec *ExecutionContext, _ json.RawMessage) (*ActionResult, error) {
	atomic.AddInt64(a.calls, 1)
	if a.names != nil {
		a.names.Store(ec.RuleName, struct{}{})
	}
	return nil, nil
}

func ruleWithCountAction(name, channel string) Rule {
	return Rule{
		ID:          name,
		TenantID:    "local",
		Channel:     channel,
		Name:        name,
		Enabled:     true,
		TriggerKind: TriggerManual,
		Actions: ActionList{
			Actions: []ActionInstance{{TypeID: "test:count", Enabled: true}},
		},
	}
}

func newCountingEngine(t *testing.T, calls *int64, names *sync.Map) *Engine {
	t.Helper()
	reg := NewRegistry()
	if err := reg.RegisterAction(countingAction{calls: calls, names: names}); err != nil {
		t.Fatalf("register action: %v", err)
	}
	eng, err := New(Config{TenantID: "local", Source: emptySource{}, Registry: reg})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	eng.Start()
	return eng
}

// emptySource satisfies RuleSource for engines that are only driven via RunRule.
type emptySource struct{}

func (emptySource) ListEnabled(context.Context, string, string) ([]Rule, error) {
	return nil, nil
}

func waitForCount(t *testing.T, calls *int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(calls) >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d action calls, got %d", want, atomic.LoadInt64(calls))
}

func TestRunRuleRunsOnlyTargetedRule(t *testing.T) {
	var calls int64
	var names sync.Map
	eng := newCountingEngine(t, &calls, &names)
	defer eng.Stop()

	target := ruleWithCountAction("target", "chan")
	other := ruleWithCountAction("other", "chan")
	_ = other

	eng.RunRule(target, Trigger{Kind: TriggerManual, Channel: "chan"})
	waitForCount(t, &calls, 1)

	if _, ok := names.Load("target"); !ok {
		t.Fatalf("targeted rule did not run")
	}
	if _, ok := names.Load("other"); ok {
		t.Fatalf("non-targeted rule ran")
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("expected exactly 1 call, got %d", got)
	}
}

func TestRunRuleAfterStopDoesNotPanic(t *testing.T) {
	var calls int64
	eng := newCountingEngine(t, &calls, nil)
	eng.Stop()

	// Must not panic on send-to-closed; the schedule stopped-guard drops it.
	eng.RunRule(ruleWithCountAction("late", "chan"), Trigger{Kind: TriggerManual, Channel: "chan"})

	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Fatalf("expected no calls after Stop, got %d", got)
	}
}

func TestMatchTriggerFilterTimerAndManual(t *testing.T) {
	timerRule := Rule{TriggerKind: TriggerTimer, TriggerFilter: json.RawMessage(`{"interval_seconds":30}`)}
	if !matchTriggerFilter(timerRule, Trigger{Kind: TriggerTimer, EventType: "timer"}) {
		t.Fatalf("timer rule should match a timer trigger")
	}
	manualRule := Rule{TriggerKind: TriggerManual}
	if !matchTriggerFilter(manualRule, Trigger{Kind: TriggerManual}) {
		t.Fatalf("manual rule should match a manual trigger")
	}

	// Event/command matching must stay intact.
	eventRule := Rule{TriggerKind: TriggerEvent, TriggerFilter: json.RawMessage(`{"event_type":"sub"}`)}
	if !matchTriggerFilter(eventRule, Trigger{Kind: TriggerEvent, EventType: "sub"}) {
		t.Fatalf("event rule should match its event type")
	}
	if matchTriggerFilter(eventRule, Trigger{Kind: TriggerEvent, EventType: "raid"}) {
		t.Fatalf("event rule must not match a different event type")
	}
	cmdRule := Rule{TriggerKind: TriggerCommand, TriggerFilter: json.RawMessage(`{"command":"hello"}`)}
	if !matchTriggerFilter(cmdRule, Trigger{Kind: TriggerCommand, Text: "!hello world"}) {
		t.Fatalf("command rule should match its command word")
	}
	if matchTriggerFilter(cmdRule, Trigger{Kind: TriggerCommand, Text: "!bye"}) {
		t.Fatalf("command rule must not match a different command")
	}
}

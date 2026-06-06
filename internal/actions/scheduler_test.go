package actions

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// fakeTimerStore is an in-memory timerLister whose rule set the test mutates
// between Reloads. It satisfies the Store interface only as far as the
// scheduler needs (ListTimerRules); the other methods are unused stubs.
type fakeTimerStore struct {
	mu    sync.Mutex
	rules []Rule
}

func (f *fakeTimerStore) setRules(rules []Rule) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append([]Rule(nil), rules...)
}

func (f *fakeTimerStore) ListTimerRules(_ context.Context, _ string) ([]Rule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Rule(nil), f.rules...), nil
}

// countingRunner records per-rule fire counts; it stands in for *Engine so the
// scheduler test never needs a real worker pool.
type countingRunner struct {
	mu     sync.Mutex
	counts map[string]int64
}

func newCountingRunner() *countingRunner {
	return &countingRunner{counts: make(map[string]int64)}
}

func (c *countingRunner) RunRule(rule Rule, _ Trigger) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[rule.Name]++
}

func (c *countingRunner) count(name string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

func timerRule(name string, intervalSeconds int) Rule {
	tf, _ := json.Marshal(timerTriggerFilter{IntervalSeconds: intervalSeconds})
	return Rule{
		ID:            name,
		TenantID:      "local",
		Channel:       "chan",
		Name:          name,
		Enabled:       true,
		TriggerKind:   TriggerTimer,
		TriggerFilter: tf,
		Actions:       ActionList{Actions: []ActionInstance{{TypeID: "test:count", Enabled: true}}},
	}
}

// newTestScheduler builds a Scheduler wired to a stub runner and store with the
// minimum interval lowered so ticks land within test timeouts.
func newTestScheduler(runner ruleRunner, store timerLister) *Scheduler {
	return &Scheduler{
		engine:   runner,
		store:    store,
		tenantID: "local",
		log:      discardLogger(),
	}
}

func waitForRunnerCount(t *testing.T, c *countingRunner, name string, want int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.count(name) >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for rule %q to fire %d times, got %d", name, want, c.count(name))
}

func TestSchedulerFiresTimerRuleRepeatedly(t *testing.T) {
	runner := newCountingRunner()
	store := &fakeTimerStore{}
	store.setRules([]Rule{timerRule("a", 1)})

	sched := newTestScheduler(runner, store)
	sched.intervalOverride = 10 * time.Millisecond
	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	waitForRunnerCount(t, runner, "a", 3)
}

func TestSchedulerReloadAddsAndDropsRules(t *testing.T) {
	runner := newCountingRunner()
	store := &fakeTimerStore{}
	store.setRules([]Rule{timerRule("a", 1)})

	sched := newTestScheduler(runner, store)
	sched.intervalOverride = 10 * time.Millisecond
	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	waitForRunnerCount(t, runner, "a", 1)

	// Reload with "a" removed and "b" added: b must start firing, a must stop.
	store.setRules([]Rule{timerRule("b", 1)})
	if err := sched.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	waitForRunnerCount(t, runner, "b", 2)

	aAfterReload := runner.count("a")
	time.Sleep(50 * time.Millisecond)
	if got := runner.count("a"); got != aAfterReload {
		t.Fatalf("dropped rule %q kept firing: %d -> %d", "a", aAfterReload, got)
	}
}

func TestSchedulerSkipsNonPositiveInterval(t *testing.T) {
	runner := newCountingRunner()
	store := &fakeTimerStore{}
	store.setRules([]Rule{timerRule("zero", 0)})

	sched := newTestScheduler(runner, store)
	sched.intervalOverride = 10 * time.Millisecond
	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	time.Sleep(60 * time.Millisecond)
	if got := runner.count("zero"); got != 0 {
		t.Fatalf("rule with non-positive interval fired %d times", got)
	}
}

func TestSchedulerStopIsIdempotentAndClean(t *testing.T) {
	runner := newCountingRunner()
	store := &fakeTimerStore{}
	store.setRules([]Rule{timerRule("a", 1), timerRule("b", 1)})

	sched := newTestScheduler(runner, store)
	sched.intervalOverride = 5 * time.Millisecond
	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	sched.Stop()
	sched.Stop()

	// A reload after Stop must not resurrect tickers.
	if err := sched.Reload(context.Background()); err != nil {
		t.Fatalf("reload after stop: %v", err)
	}
	before := runner.count("a")
	time.Sleep(50 * time.Millisecond)
	if got := runner.count("a"); got != before {
		t.Fatalf("scheduler fired after Stop: %d -> %d", before, got)
	}
}

func TestSchedulerCtxCancelStopsTickers(t *testing.T) {
	runner := newCountingRunner()
	store := &fakeTimerStore{}
	store.setRules([]Rule{timerRule("a", 1)})

	ctx, cancel := context.WithCancel(context.Background())
	sched := newTestScheduler(runner, store)
	sched.intervalOverride = 10 * time.Millisecond
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	waitForRunnerCount(t, runner, "a", 1)
	cancel()

	// After ctx cancel the ticker goroutine must exit and stop firing.
	time.Sleep(30 * time.Millisecond)
	stable := runner.count("a")
	time.Sleep(50 * time.Millisecond)
	if got := runner.count("a"); got != stable {
		t.Fatalf("rule kept firing after ctx cancel: %d -> %d", stable, got)
	}
}

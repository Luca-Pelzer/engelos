package actions

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cdClock is a mutable fake clock for cooldown tests.
type cdClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *cdClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *cdClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newCooldownForTest(clk *cdClock) cooldownCondition {
	return cooldownCondition{store: &cooldownStore{last: make(map[string]cooldownEntry), now: clk.now}}
}

func ecFor(ruleID, channel, username string) *ExecutionContext {
	return &ExecutionContext{RuleID: ruleID, Channel: channel, Trigger: Trigger{Username: username}}
}

func TestCooldownCondition_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	_, ok := reg.Condition("cond:cooldown")
	assert.True(t, ok)
}

func TestCooldownCondition_PassesBlocksPasses(t *testing.T) {
	clk := &cdClock{t: time.Unix(1_000_000, 0)}
	c := newCooldownForTest(clk)
	ec := ecFor("r1", "chan", "bob")
	cfg := mustJSON(t, map[string]any{"seconds": 10})

	ok, err := c.Evaluate(ec, cfg)
	require.NoError(t, err)
	assert.True(t, ok, "first evaluation passes and arms the cooldown")

	clk.advance(5 * time.Second)
	ok, err = c.Evaluate(ec, cfg)
	require.NoError(t, err)
	assert.False(t, ok, "within the window it blocks")

	clk.advance(5 * time.Second) // now exactly 10s since arm
	ok, err = c.Evaluate(ec, cfg)
	require.NoError(t, err)
	assert.True(t, ok, "at the window boundary it passes again")
}

func TestCooldownCondition_PerUserIsolation(t *testing.T) {
	clk := &cdClock{t: time.Unix(1_000_000, 0)}
	c := newCooldownForTest(clk)
	cfg := mustJSON(t, map[string]any{"seconds": 30, "per": "user"})

	ok, err := c.Evaluate(ecFor("r1", "chan", "alice"), cfg)
	require.NoError(t, err)
	assert.True(t, ok, "alice passes")

	ok, err = c.Evaluate(ecFor("r1", "chan", "bob"), cfg)
	require.NoError(t, err)
	assert.True(t, ok, "bob has an independent window")

	clk.advance(5 * time.Second)
	ok, err = c.Evaluate(ecFor("r1", "chan", "alice"), cfg)
	require.NoError(t, err)
	assert.False(t, ok, "alice is still cooling down")
}

func TestCooldownCondition_PerRuleSharedAcrossUsers(t *testing.T) {
	clk := &cdClock{t: time.Unix(1_000_000, 0)}
	c := newCooldownForTest(clk)
	cfg := mustJSON(t, map[string]any{"seconds": 30}) // per defaults to rule

	ok, err := c.Evaluate(ecFor("r1", "chan", "alice"), cfg)
	require.NoError(t, err)
	assert.True(t, ok)

	clk.advance(5 * time.Second)
	ok, err = c.Evaluate(ecFor("r1", "chan", "bob"), cfg)
	require.NoError(t, err)
	assert.False(t, ok, "per=rule shares one window regardless of user")
}

func TestCooldownCondition_InvalidConfig(t *testing.T) {
	c := newCooldownForTest(&cdClock{t: time.Now()})
	ec := ecFor("r1", "chan", "bob")

	_, err := c.Evaluate(ec, mustJSON(t, map[string]any{"seconds": 0}))
	require.Error(t, err)

	_, err = c.Evaluate(ec, mustJSON(t, map[string]any{"seconds": 10, "per": "channel"}))
	require.Error(t, err)
}

// TestCooldownCondition_ConcurrentFire runs many evaluations at once against a
// long window: exactly one must pass, proving the store is race-free. Run with
// -race to exercise the mutex.
func TestCooldownCondition_ConcurrentFire(t *testing.T) {
	c := cooldownCondition{store: newCooldownStore()}
	ec := ecFor("r1", "chan", "bob")
	cfg := mustJSON(t, map[string]any{"seconds": 3600})

	const n = 64
	var passes int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ok, err := c.Evaluate(ec, cfg)
			assert.NoError(t, err)
			if ok {
				atomic.AddInt64(&passes, 1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(1), passes, "exactly one concurrent evaluation passes")
}

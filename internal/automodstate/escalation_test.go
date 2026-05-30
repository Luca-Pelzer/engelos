package automodstate

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a manually-advanced time source for deterministic decay tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{t: start} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestEscalator_LadderProgression(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	e := NewEscalator(DefaultTiers(), time.Hour).WithClock(clk.now)

	want := []struct {
		action  Action
		timeout time.Duration
	}{
		{ActionWarn, 0},
		{ActionTimeout, 60 * time.Second},
		{ActionTimeout, 10 * time.Minute},
		{ActionTimeout, 24 * time.Hour},
		{ActionBan, 0},
	}
	for i, w := range want {
		act, to := e.Record("chan", "user", "caps")
		if act != w.action || to != w.timeout {
			t.Fatalf("offense %d: got (%s,%s) want (%s,%s)", i+1, act, to, w.action, w.timeout)
		}
	}
}

func TestEscalator_ClampBeyondLastTier(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	e := NewEscalator(DefaultTiers(), time.Hour).WithClock(clk.now)

	// Burn through all five rungs.
	for i := 0; i < 5; i++ {
		e.Record("chan", "user", "caps")
	}
	// 6th, 7th, ... all stay pinned to the last tier (ban).
	for i := 0; i < 3; i++ {
		act, to := e.Record("chan", "user", "caps")
		if act != ActionBan || to != 0 {
			t.Fatalf("clamped offense got (%s,%s) want (ban,0)", act, to)
		}
	}
	if got := e.Offenses("chan", "user", "caps"); got != 8 {
		t.Fatalf("Offenses got %d want 8", got)
	}
}

func TestEscalator_DecayResetsCount(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	decay := time.Hour
	e := NewEscalator(DefaultTiers(), decay).WithClock(clk.now)

	if act, _ := e.Record("c", "u", "f"); act != ActionWarn {
		t.Fatalf("first offense not warn: %s", act)
	}
	if act, to := e.Record("c", "u", "f"); act != ActionTimeout || to != 60*time.Second {
		t.Fatalf("second offense got (%s,%s) want (timeout,60s)", act, to)
	}

	// Move past the decay window: the next offense is a fresh first-timer.
	clk.advance(decay + time.Second)
	if act, _ := e.Record("c", "u", "f"); act != ActionWarn {
		t.Fatalf("post-decay offense not warn: %s", act)
	}
	if got := e.Offenses("c", "u", "f"); got != 1 {
		t.Fatalf("post-decay Offenses got %d want 1", got)
	}
}

func TestEscalator_OffensesReflectsDecayWithoutMutating(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	decay := 30 * time.Minute
	e := NewEscalator(DefaultTiers(), decay).WithClock(clk.now)

	e.Record("c", "u", "f")
	if got := e.Offenses("c", "u", "f"); got != 1 {
		t.Fatalf("Offenses got %d want 1", got)
	}

	// Past decay: read-only view reports 0...
	clk.advance(decay + time.Minute)
	if got := e.Offenses("c", "u", "f"); got != 0 {
		t.Fatalf("decayed Offenses got %d want 0", got)
	}
	// ...but the stale record was not removed by the read: a subsequent Record
	// still treats it as a first offense (count reset, not stacked).
	if act, _ := e.Record("c", "u", "f"); act != ActionWarn {
		t.Fatalf("offense after decayed read not warn: %s", act)
	}
}

func TestEscalator_ResetClears(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	e := NewEscalator(DefaultTiers(), time.Hour).WithClock(clk.now)

	e.Record("c", "u", "caps")
	e.Record("c", "u", "caps")
	e.Record("c", "u", "links")
	e.Reset("c", "u")

	if got := e.Offenses("c", "u", "caps"); got != 0 {
		t.Fatalf("caps after reset got %d want 0", got)
	}
	if got := e.Offenses("c", "u", "links"); got != 0 {
		t.Fatalf("links after reset got %d want 0", got)
	}
	// After reset the next offense is a first-timer again.
	if act, _ := e.Record("c", "u", "caps"); act != ActionWarn {
		t.Fatalf("post-reset offense not warn: %s", act)
	}
}

func TestEscalator_KeyNormalisation(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	e := NewEscalator(DefaultTiers(), time.Hour).WithClock(clk.now)

	e.Record("  Chan ", "USER", "Caps")
	// Differently-cased/spaced inputs must hit the same record.
	if act, to := e.Record("chan", "user", "caps"); act != ActionTimeout || to != 60*time.Second {
		t.Fatalf("normalised second offense got (%s,%s) want (timeout,60s)", act, to)
	}
}

func TestEscalator_DistinctKeysAreIndependent(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	e := NewEscalator(DefaultTiers(), time.Hour).WithClock(clk.now)

	e.Record("c", "u", "caps")
	// A different filter for the same user starts its own ladder.
	if act, _ := e.Record("c", "u", "links"); act != ActionWarn {
		t.Fatalf("distinct filter not warn: %s", act)
	}
	// A different user likewise.
	if act, _ := e.Record("c", "other", "caps"); act != ActionWarn {
		t.Fatalf("distinct user not warn: %s", act)
	}
}

func TestEscalator_ConcurrentRecordNoLostCounts(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	// Large decay so nothing expires mid-test.
	e := NewEscalator(DefaultTiers(), 24*time.Hour).WithClock(clk.now)

	const goroutines = 50
	const perG = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				e.Record("c", "u", "caps")
			}
		}()
	}
	wg.Wait()

	if got := e.Offenses("c", "u", "caps"); got != goroutines*perG {
		t.Fatalf("concurrent Offenses got %d want %d", got, goroutines*perG)
	}
}

func TestEscalator_NewEscalatorDefaultsEmptyTiers(t *testing.T) {
	e := NewEscalator(nil, time.Hour)
	if act, _ := e.Record("c", "u", "f"); act != ActionWarn {
		t.Fatalf("empty-tiers fallback first offense not warn: %s", act)
	}
}

func TestPermitTracker_GrantConsumeOnce(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	p := NewPermitTracker(time.Minute).WithClock(clk.now)

	p.Grant("chan", "user")
	if !p.Consume("chan", "user") {
		t.Fatal("first Consume want true")
	}
	// Permit is single-use: the second Consume must fail.
	if p.Consume("chan", "user") {
		t.Fatal("second Consume want false")
	}
}

func TestPermitTracker_Expired(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	window := time.Minute
	p := NewPermitTracker(window).WithClock(clk.now)

	p.Grant("chan", "user")
	clk.advance(window + time.Second)
	if p.Consume("chan", "user") {
		t.Fatal("expired Consume want false")
	}
	// Expired permit must have been evicted; a fresh grant works again.
	p.Grant("chan", "user")
	if !p.Consume("chan", "user") {
		t.Fatal("re-granted Consume want true")
	}
}

func TestPermitTracker_NoGrant(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	p := NewPermitTracker(time.Minute).WithClock(clk.now)
	if p.Consume("chan", "user") {
		t.Fatal("Consume without grant want false")
	}
}

func TestPermitTracker_KeyNormalisation(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	p := NewPermitTracker(time.Minute).WithClock(clk.now)
	p.Grant(" Chan ", "USER")
	if !p.Consume("chan", "user") {
		t.Fatal("normalised Consume want true")
	}
}

func TestPermitTracker_ConcurrentGrantConsume(t *testing.T) {
	clk := newFakeClock(time.Unix(1_700_000_000, 0).UTC())
	p := NewPermitTracker(time.Hour).WithClock(clk.now)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			p.Grant("c", "u")
			p.Consume("c", "u")
		}()
	}
	wg.Wait()
	// No assertion on final state (races between grants/consumes are inherent);
	// this test exists to be run under -race to prove no data race.
}

package actions

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// minTimerInterval clamps a timer rule's cadence so a misconfigured rule with a
// tiny or zero interval cannot spin the worker pool.
const minTimerInterval = 5 * time.Second

// ruleRunner is the narrow slice of the engine the scheduler drives: it fires
// one specific rule per tick. *Engine satisfies it; tests inject a counting
// stub.
type ruleRunner interface {
	RunRule(rule Rule, t Trigger)
}

// timerLister supplies the enabled timer rules the scheduler arms. The store
// satisfies it; tests use an in-memory fake.
type timerLister interface {
	ListTimerRules(ctx context.Context, tenantID string) ([]Rule, error)
}

// Scheduler arms one time.Ticker per enabled timer-kind rule and fires that rule
// via the engine on each tick. Reload re-reads the store so a timer rule edited
// through the API is re-armed without a process restart.
//
// Concurrency invariants: mu guards gen, stopped and the current generation's
// stop channel. Each generation of tickers is signalled to exit by closing its
// own stop channel (never by sending on it), and wg tracks the goroutines so
// Stop and Reload can wait for the previous generation to drain before the next
// one is armed. wg.Add is always performed under mu and only after re-checking
// stopped, so an Add can never race a final Wait.
type Scheduler struct {
	engine   ruleRunner
	store    timerLister
	tenantID string
	log      *slog.Logger

	// loadMu serialises whole load (Start/Reload) calls so two concurrent
	// reloads can never arm two overlapping generations of tickers.
	loadMu sync.Mutex

	// intervalOverride, when non-zero, is used as the ticker cadence for every
	// armed rule in place of the rule's configured interval. It exists only so
	// tests can drive sub-second ticks deterministically; production leaves it
	// zero. The interval<=0 skip still applies first, so a disabled rule stays
	// skipped even under an override.
	intervalOverride time.Duration

	mu      sync.Mutex
	stopped bool
	genStop chan struct{}
	wg      sync.WaitGroup
}

// NewScheduler builds a Scheduler. engine and store are required by the caller;
// a nil logger falls back to the default.
func NewScheduler(engine *Engine, store Store, tenantID string, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if tenantID == "" {
		tenantID = "local"
	}
	return &Scheduler{
		engine:   engine,
		store:    store,
		tenantID: tenantID,
		log:      logger.With("component", "actions.scheduler"),
	}
}

// Start arms the first generation of tickers from the current store contents.
// The supplied ctx bounds every ticker goroutine: a cancelled ctx stops them
// just like Stop. It is safe to call once before any Reload.
func (s *Scheduler) Start(ctx context.Context) error {
	return s.load(ctx)
}

// Reload stops the current generation of tickers and arms a fresh one from the
// store, so a timer rule added, retimed or removed via the API takes effect
// live. It is concurrency-safe and never leaks goroutines: the previous
// generation is signalled and waited out before the next is armed.
func (s *Scheduler) Reload(ctx context.Context) error {
	return s.load(ctx)
}

// load is the shared body of Start and Reload: tear down the previous
// generation under the lock, then arm a new one. After Stop it is a no-op.
func (s *Scheduler) load(ctx context.Context) error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()

	rules, err := s.store.ListTimerRules(ctx, s.tenantID)
	if err != nil {
		s.log.Warn("list timer rules failed", "err", err)
		return err
	}

	// Stop the previous generation and wait for it to drain before arming the
	// next, so two generations never run the same rule concurrently.
	s.stopGeneration()
	s.wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	stop := make(chan struct{})
	s.genStop = stop

	armed := 0
	for _, r := range rules {
		interval := timerInterval(r)
		if interval <= 0 {
			s.log.Warn("timer rule has non-positive interval, skipping",
				"rule", r.Name, "channel", r.Channel)
			continue
		}
		if s.intervalOverride > 0 {
			interval = s.intervalOverride
		} else if interval < minTimerInterval {
			interval = minTimerInterval
		}
		// wg.Add is under mu and after the stopped re-check, so it can never
		// race the wg.Wait in Stop.
		s.wg.Add(1)
		go s.runTicker(ctx, r, interval, stop)
		armed++
	}
	s.log.Info("timer scheduler armed", "rules", armed)
	return nil
}

// runTicker fires one rule on each tick until ctx is cancelled or this
// generation's stop channel is closed.
func (s *Scheduler) runTicker(ctx context.Context, rule Rule, interval time.Duration, stop <-chan struct{}) {
	defer s.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.engine.RunRule(rule, Trigger{
				Kind:      TriggerTimer,
				Channel:   rule.Channel,
				EventType: "timer",
			})
		case <-stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

// stopGeneration closes the current generation's stop channel under the lock so
// every ticker exits. Closing (never sending) means a concurrent stopGeneration
// or Stop can never double-close or send on a closed channel: genStop is nilled
// after the close so the next caller skips it.
func (s *Scheduler) stopGeneration() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.genStop != nil {
		close(s.genStop)
		s.genStop = nil
	}
}

// Stop halts every ticker and blocks until all goroutines have returned. It is
// idempotent and, once called, makes load a no-op so a late Reload cannot
// resurrect tickers.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		s.wg.Wait()
		return
	}
	s.stopped = true
	if s.genStop != nil {
		close(s.genStop)
		s.genStop = nil
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// timerInterval decodes a timer rule's IntervalSeconds from its TriggerFilter.
// A missing or unparseable filter yields zero so load skips the rule.
func timerInterval(r Rule) time.Duration {
	if len(r.TriggerFilter) == 0 {
		return 0
	}
	var f timerTriggerFilter
	if err := json.Unmarshal(r.TriggerFilter, &f); err != nil {
		return 0
	}
	return time.Duration(f.IntervalSeconds) * time.Second
}

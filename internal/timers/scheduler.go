package timers

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// defaultTick is the scheduler's wake interval when [Config.Tick] is zero.
// Timers have second-to-minute-scale intervals, so a 10s resolution keeps
// firing punctual without busy-spinning.
const defaultTick = 10 * time.Second

// reloadInterval is how often Run re-reads the enabled-timer set from the
// store so timers added/removed via admin commands take effect without a
// restart. It is deliberately coarse: a minute of latency on a new timer is
// acceptable and avoids hammering the store every tick.
const reloadInterval = 60 * time.Second

// Sender delivers a timer message to a channel on some platform. The wiring
// layer adapts the platform adapters to this (e.g. send to the Twitch
// adapter's Do(ActionSendMessage)). Implementations must be safe for
// concurrent use.
type Sender interface {
	Send(ctx context.Context, channel, message string) error
}

// Config configures a [Scheduler].
type Config struct {
	Store    Store
	Sender   Sender
	TenantID string
	Logger   *slog.Logger

	// Tick is how often the scheduler wakes to check for due timers.
	// Default 10s when zero. (Timers themselves have minute-scale
	// intervals; this is just the resolution.)
	Tick time.Duration

	// Now is a clock seam for tests; defaults to time.Now.
	Now func() time.Time
}

// schedTimer is a Scheduler's in-memory view of a loaded timer plus its
// firing bookkeeping.
type schedTimer struct {
	timer     Timer
	lastFired time.Time
}

// Scheduler periodically fires due timers via a [Sender], gated by chat
// activity. It is decoupled from the platform layer: it depends only on the
// narrow [Store] and [Sender] interfaces.
//
// Firing semantics (see [Scheduler.Run]):
//   - A freshly loaded timer's lastFired is initialised to the load time so
//     it waits a full Interval before its first fire (no startup burst).
//   - The activity gate is per CHANNEL, shared across every timer in that
//     channel: when ANY timer in a channel fires, that channel's activity
//     counter resets to zero. With multiple timers per channel the gate is
//     therefore approximate, which is an accepted simplification.
//   - On a [Sender] error, lastFired is still advanced to "now" so a
//     persistently failing channel retries at most once per Interval rather
//     than hot-looping every tick; the error is logged at WARN.
type Scheduler struct {
	store    Store
	sender   Sender
	tenantID string
	log      *slog.Logger
	tick     time.Duration
	now      func() time.Time

	mu       sync.Mutex
	timers   []*schedTimer
	activity map[string]int
}

// New constructs a [Scheduler] from cfg. It returns an error when Store or
// Sender is nil. A zero Tick defaults to 10s; a nil Now defaults to
// [time.Now]; a nil Logger defaults to [slog.Default].
func New(cfg Config) (*Scheduler, error) {
	if cfg.Store == nil {
		return nil, errors.New("timers: scheduler requires a non-nil Store")
	}
	if cfg.Sender == nil {
		return nil, errors.New("timers: scheduler requires a non-nil Sender")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	tick := cfg.Tick
	if tick <= 0 {
		tick = defaultTick
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Scheduler{
		store:    cfg.Store,
		sender:   cfg.Sender,
		tenantID: cfg.TenantID,
		log:      logger.With("component", "timers.scheduler"),
		tick:     tick,
		now:      now,
		activity: make(map[string]int),
	}, nil
}

// RecordChatActivity increments the activity counter for a channel. The
// dispatcher calls this on every chat message so the scheduler can gate
// timers behind "chat has been active". Safe for concurrent use.
func (s *Scheduler) RecordChatActivity(channel string) {
	s.mu.Lock()
	s.activity[channel]++
	s.mu.Unlock()
}

// Run loads enabled timers and fires them on schedule until ctx is done.
// Returns nil on ctx cancellation. Run reloads the enabled-timer set on
// start and then every reloadInterval so admin changes apply without a
// restart.
func (s *Scheduler) Run(ctx context.Context) error {
	if err := s.Reload(ctx); err != nil {
		s.log.Warn("timers: initial reload failed", "err", err)
	}

	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	lastReload := s.now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			now := s.now()
			if now.Sub(lastReload) >= reloadInterval {
				if err := s.Reload(ctx); err != nil {
					s.log.Warn("timers: reload failed", "err", err)
				}
				lastReload = now
			}
			s.fireDue(ctx, now)
		}
	}
}

// Reload re-reads enabled timers from the store, preserving the lastFired
// bookkeeping for timers that survive the reload so a reload never causes a
// premature or duplicate fire. Newly-appearing timers start with lastFired
// set to now. Safe for concurrent use.
func (s *Scheduler) Reload(ctx context.Context) error {
	loaded, err := s.store.ListEnabled(ctx, s.tenantID)
	if err != nil {
		return err
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	prev := make(map[string]time.Time, len(s.timers))
	for _, st := range s.timers {
		prev[st.timer.ID] = st.lastFired
	}

	next := make([]*schedTimer, 0, len(loaded))
	for _, t := range loaded {
		last := now
		if existing, ok := prev[t.ID]; ok {
			last = existing
		}
		next = append(next, &schedTimer{timer: t, lastFired: last})
	}
	s.timers = next
	return nil
}

// fireDue posts every timer whose interval has elapsed and whose activity
// gate is satisfied. It holds s.mu for the whole pass; Sender.Send is called
// under the lock, which is acceptable because the scheduler is single-writer
// on its tick goroutine and Send is expected to be quick (one chat line).
func (s *Scheduler) fireDue(ctx context.Context, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, st := range s.timers {
		if now.Sub(st.lastFired) < st.timer.Interval {
			continue
		}
		if st.timer.MinChatLines > 0 && s.activity[st.timer.Channel] < st.timer.MinChatLines {
			continue
		}
		if err := s.sender.Send(ctx, st.timer.Channel, st.timer.Message); err != nil {
			s.log.Warn("timers: send failed",
				"channel", st.timer.Channel, "timer", st.timer.Name, "err", err)
		}
		st.lastFired = now
		s.activity[st.timer.Channel] = 0
	}
}

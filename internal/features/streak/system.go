package streak

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
)

// ErrNoFreezesAvailable is returned by [System.UseFreeze] when the viewer
// holds zero freeze credits.
var ErrNoFreezesAvailable = errors.New("streak: no freezes available")

// System is the Streak-System aggregate.
//
// A System owns no goroutines and is safe for concurrent use; all mutating
// methods serialise on an internal mutex so a single viewer's tick sequence
// is linearisable.
type System struct {
	cfg    Config
	store  eventsourcing.EventStore
	rm     *ReadModel
	logger *slog.Logger
	clock  func() time.Time

	mu sync.Mutex
}

// New constructs a System. It validates cfg, defaults the logger when nil and
// installs a UTC system clock. Use [System.WithClock] in tests to inject a
// deterministic clock.
func New(cfg Config, store eventsourcing.EventStore, logger *slog.Logger) (*System, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("streak: event store is required")
	}
	if logger == nil {
		if cfg.Logger != nil {
			logger = cfg.Logger
		} else {
			logger = slog.Default()
		}
	}
	return &System{
		cfg:    cfg,
		store:  store,
		rm:     NewReadModel(),
		logger: logger,
		clock:  func() time.Time { return time.Now().UTC() },
	}, nil
}

// WithClock replaces the clock function. Returns s for chaining. Nil is
// ignored.
func (s *System) WithClock(clock func() time.Time) *System {
	if clock == nil {
		return s
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = clock
	return s
}

// Config returns a copy of the active configuration.
func (s *System) Config() Config { return s.cfg }

// ReadModel exposes the in-memory projection.
func (s *System) ReadModel() *ReadModel { return s.rm }

// Result summarises the outcome of a single [System.Tick] or
// [System.UseFreeze] call.
type Result struct {
	DaysCurrent      int
	DaysLongest      int
	FreezesAvailable int
	Milestone        int
	SameDayReTick    bool
	UsedFreezes      int
	BrokenFromDays   int
}

// Status is a read-only snapshot of the viewer's standing.
type Status struct {
	DaysCurrent      int
	DaysLongest      int
	FreezesAvailable int
	LastTickAt       time.Time
	NextMilestone    int
}

// Tick records activity for a viewer.
//
// Behaviour:
//   - First tick ever for the viewer emits [EventTypeStreakStarted] with
//     DaysCurrent=1.
//   - A tick on the same effective UTC day as the previous tick emits
//     [EventTypeStreakContinued] with SameDayReTick=true and does not
//     advance the day count.
//   - A tick on the next effective UTC day advances DaysCurrent by 1 and
//     emits [EventTypeStreakContinued]; if a milestone threshold is
//     crossed a follow-up [EventTypeStreakMilestone] is emitted in the
//     same batch.
//   - If days were missed and the viewer holds enough freeze credits, one
//     credit is consumed per missed day, [EventTypeStreakFrozen] is emitted
//     and the streak continues (with the new day still counted).
//   - If missed days exceed freeze credits, [EventTypeStreakBroken] is
//     emitted, the streak resets and a fresh [EventTypeStreakStarted] is
//     emitted in the same batch.
//
// Tick is atomic per (tenant, channel, viewer): the read model is updated
// only after the event(s) are durably appended.
func (s *System) Tick(ctx context.Context, tenantID, channel, viewerID, username string) (Result, error) {
	if err := validateIdentity(tenantID, channel, viewerID); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock().UTC()
	current := s.rm.Get(tenantID, channel, viewerID)
	tickDay := streakDay(now, s.cfg.GraceWindow)

	if current.DaysCurrent == 0 && current.LastTickAt.IsZero() {
		return s.startFresh(ctx, tenantID, channel, viewerID, username, now, tickDay)
	}

	dayDelta := dayDiff(current.LastTickDayUTC, tickDay)

	switch {
	case dayDelta <= 0:
		return s.sameDayReTick(ctx, tenantID, channel, viewerID, username, now, current)
	case dayDelta == 1:
		return s.advanceOneDay(ctx, tenantID, channel, viewerID, username, now, tickDay, current)
	default:
		missed := dayDelta - 1
		if missed <= current.FreezesAvailable {
			return s.freezeAndAdvance(ctx, tenantID, channel, viewerID, username, now, tickDay, current, missed)
		}
		return s.breakAndRestart(ctx, tenantID, channel, viewerID, username, now, tickDay, current, missed)
	}
}

// startFresh emits a single streak.started event for a previously unseen
// viewer.
func (s *System) startFresh(
	ctx context.Context,
	tenantID, channel, viewerID, username string,
	now time.Time, tickDay time.Time,
) (Result, error) {
	ev, err := newEvent(tenantID, EventTypeStreakStarted, StreakStartedPayload{
		Channel: channel, ViewerID: viewerID, Username: username,
	}, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.store.Append(ctx, ev); err != nil {
		return Result{}, fmt.Errorf("streak: append streak.started: %w", err)
	}
	if err := s.rm.Apply(ev); err != nil {
		return Result{}, fmt.Errorf("streak: apply streak.started: %w", err)
	}
	_ = tickDay
	return Result{DaysCurrent: 1, DaysLongest: 1}, nil
}

// sameDayReTick records a no-op tick within the same effective UTC day.
func (s *System) sameDayReTick(
	ctx context.Context,
	tenantID, channel, viewerID, username string,
	now time.Time,
	current State,
) (Result, error) {
	payload := StreakContinuedPayload{
		Channel:       channel,
		ViewerID:      viewerID,
		Username:      username,
		DaysCurrent:   current.DaysCurrent,
		DaysLongest:   current.DaysLongest,
		SameDayReTick: true,
	}
	ev, err := newEvent(tenantID, EventTypeStreakContinued, payload, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.store.Append(ctx, ev); err != nil {
		return Result{}, fmt.Errorf("streak: append streak.continued: %w", err)
	}
	if err := s.rm.Apply(ev); err != nil {
		return Result{}, fmt.Errorf("streak: apply streak.continued: %w", err)
	}
	return Result{
		DaysCurrent:      current.DaysCurrent,
		DaysLongest:      current.DaysLongest,
		FreezesAvailable: current.FreezesAvailable,
		SameDayReTick:    true,
	}, nil
}

// advanceOneDay emits streak.continued and (optionally) streak.milestone
// when the tick falls on the day immediately after the previous one.
func (s *System) advanceOneDay(
	ctx context.Context,
	tenantID, channel, viewerID, username string,
	now time.Time, tickDay time.Time,
	current State,
) (Result, error) {
	newDays := current.DaysCurrent + 1
	newLongest := current.DaysLongest
	if newDays > newLongest {
		newLongest = newDays
	}
	contPayload := StreakContinuedPayload{
		Channel:     channel,
		ViewerID:    viewerID,
		Username:    username,
		DaysCurrent: newDays,
		DaysLongest: newLongest,
	}
	specs := []eventSpec{{EventTypeStreakContinued, contPayload}}

	milestone, freezesAwarded, freezesTotal := s.checkMilestone(newDays, current)
	if milestone > 0 {
		specs = append(specs, eventSpec{EventTypeStreakMilestone, StreakMilestonePayload{
			Channel: channel, ViewerID: viewerID, Username: username,
			Milestone:      milestone,
			FreezesAwarded: freezesAwarded,
			FreezesTotal:   freezesTotal,
		}})
	}

	evs, err := buildEvents(tenantID, now, specs...)
	if err != nil {
		return Result{}, err
	}
	if len(evs) == 1 {
		if err := s.store.Append(ctx, evs[0]); err != nil {
			return Result{}, fmt.Errorf("streak: append streak.continued: %w", err)
		}
	} else if err := s.store.AppendBatch(ctx, evs); err != nil {
		return Result{}, fmt.Errorf("streak: append continued+milestone: %w", err)
	}
	for _, ev := range evs {
		if err := s.rm.Apply(ev); err != nil {
			return Result{}, fmt.Errorf("streak: apply %s: %w", ev.Type, err)
		}
	}
	_ = tickDay
	out := Result{
		DaysCurrent:      newDays,
		DaysLongest:      newLongest,
		FreezesAvailable: current.FreezesAvailable,
		Milestone:        milestone,
	}
	if milestone > 0 {
		out.FreezesAvailable = freezesTotal
	}
	return out, nil
}

// freezeAndAdvance consumes one freeze per missed day, then advances the
// streak to the current day.
func (s *System) freezeAndAdvance(
	ctx context.Context,
	tenantID, channel, viewerID, username string,
	now time.Time, tickDay time.Time,
	current State, missed int,
) (Result, error) {
	freezesRemain := current.FreezesAvailable - missed
	newDays := current.DaysCurrent + 1
	newLongest := current.DaysLongest
	if newDays > newLongest {
		newLongest = newDays
	}

	specs := []eventSpec{
		{EventTypeStreakFrozen, StreakFrozenPayload{
			Channel:       channel,
			ViewerID:      viewerID,
			Username:      username,
			DaysBridged:   missed,
			FreezesSpent:  missed,
			FreezesRemain: freezesRemain,
			DaysCurrent:   newDays,
		}},
	}

	milestone, freezesAwarded, freezesTotal := s.checkMilestone(newDays, State{
		DaysCurrent:      current.DaysCurrent,
		FreezesAvailable: freezesRemain,
		MilestonesHit:    current.MilestonesHit,
	})
	if milestone > 0 {
		specs = append(specs, eventSpec{EventTypeStreakMilestone, StreakMilestonePayload{
			Channel: channel, ViewerID: viewerID, Username: username,
			Milestone:      milestone,
			FreezesAwarded: freezesAwarded,
			FreezesTotal:   freezesTotal,
		}})
	}

	evs, err := buildEvents(tenantID, now, specs...)
	if err != nil {
		return Result{}, err
	}
	if err := s.store.AppendBatch(ctx, evs); err != nil {
		return Result{}, fmt.Errorf("streak: append freeze batch: %w", err)
	}
	for _, ev := range evs {
		if err := s.rm.Apply(ev); err != nil {
			return Result{}, fmt.Errorf("streak: apply %s: %w", ev.Type, err)
		}
	}
	_ = tickDay
	out := Result{
		DaysCurrent:      newDays,
		DaysLongest:      newLongest,
		FreezesAvailable: freezesRemain,
		UsedFreezes:      missed,
		Milestone:        milestone,
	}
	if milestone > 0 {
		out.FreezesAvailable = freezesTotal
	}
	return out, nil
}

// breakAndRestart emits streak.broken then streak.started for a viewer who
// missed more days than they could cover with freezes.
func (s *System) breakAndRestart(
	ctx context.Context,
	tenantID, channel, viewerID, username string,
	now time.Time, tickDay time.Time,
	current State, missed int,
) (Result, error) {
	specs := []eventSpec{
		{EventTypeStreakBroken, StreakBrokenPayload{
			Channel:     channel,
			ViewerID:    viewerID,
			Username:    username,
			DaysAtBreak: current.DaysCurrent,
			MissedDays:  missed,
		}},
		{EventTypeStreakStarted, StreakStartedPayload{
			Channel: channel, ViewerID: viewerID, Username: username,
		}},
	}
	evs, err := buildEvents(tenantID, now, specs...)
	if err != nil {
		return Result{}, err
	}
	if err := s.store.AppendBatch(ctx, evs); err != nil {
		return Result{}, fmt.Errorf("streak: append broken+started: %w", err)
	}
	for _, ev := range evs {
		if err := s.rm.Apply(ev); err != nil {
			return Result{}, fmt.Errorf("streak: apply %s: %w", ev.Type, err)
		}
	}
	_ = tickDay
	return Result{
		DaysCurrent:      1,
		DaysLongest:      current.DaysLongest,
		FreezesAvailable: current.FreezesAvailable,
		BrokenFromDays:   current.DaysCurrent,
	}, nil
}

// checkMilestone returns (milestone, awarded, total) when newDays crosses a
// previously-un-hit threshold in cfg.FreezeMilestones. The awarded count is
// the configured value clamped so that current.FreezesAvailable+awarded never
// exceeds cfg.MaxFreezesHeld. Returns (0, 0, 0) when no milestone fires.
func (s *System) checkMilestone(newDays int, current State) (int, int, int) {
	award, ok := s.cfg.FreezeMilestones[newDays]
	if !ok {
		return 0, 0, 0
	}
	if current.MilestonesHit != nil && current.MilestonesHit[newDays] {
		return 0, 0, 0
	}
	capRemain := s.cfg.MaxFreezesHeld - current.FreezesAvailable
	if capRemain < 0 {
		capRemain = 0
	}
	if award > capRemain {
		award = capRemain
	}
	total := current.FreezesAvailable + award
	return newDays, award, total
}

// UseFreeze manually spends one freeze credit to bridge a single day. It
// emits a streak.frozen event with DaysBridged=1 and does NOT advance
// DaysCurrent — the streak is held in place. Returns
// [ErrNoFreezesAvailable] when the viewer has zero credits.
func (s *System) UseFreeze(ctx context.Context, tenantID, channel, viewerID, username string) (Result, error) {
	if err := validateIdentity(tenantID, channel, viewerID); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.rm.Get(tenantID, channel, viewerID)
	if current.FreezesAvailable <= 0 {
		return Result{
			DaysCurrent:      current.DaysCurrent,
			DaysLongest:      current.DaysLongest,
			FreezesAvailable: 0,
		}, ErrNoFreezesAvailable
	}
	now := s.clock().UTC()
	remain := current.FreezesAvailable - 1
	payload := StreakFrozenPayload{
		Channel:       channel,
		ViewerID:      viewerID,
		Username:      username,
		DaysBridged:   1,
		FreezesSpent:  1,
		FreezesRemain: remain,
		DaysCurrent:   current.DaysCurrent,
	}
	ev, err := newEvent(tenantID, EventTypeStreakFrozen, payload, now)
	if err != nil {
		return Result{}, err
	}
	if err := s.store.Append(ctx, ev); err != nil {
		return Result{}, fmt.Errorf("streak: append streak.frozen: %w", err)
	}
	if err := s.rm.Apply(ev); err != nil {
		return Result{}, fmt.Errorf("streak: apply streak.frozen: %w", err)
	}
	return Result{
		DaysCurrent:      current.DaysCurrent,
		DaysLongest:      current.DaysLongest,
		FreezesAvailable: remain,
		UsedFreezes:      1,
	}, nil
}

// Reset clears a viewer's streak state (admin command). Emits a
// streak.broken event with DaysAtBreak set to the current streak length and
// MissedDays=0 to distinguish it from a natural break in the projection.
func (s *System) Reset(ctx context.Context, tenantID, channel, viewerID, reason string) error {
	if err := validateIdentity(tenantID, channel, viewerID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock().UTC()
	current := s.rm.Get(tenantID, channel, viewerID)
	payload := StreakBrokenPayload{
		Channel:     channel,
		ViewerID:    viewerID,
		DaysAtBreak: current.DaysCurrent,
		MissedDays:  0,
	}
	ev, err := newEvent(tenantID, EventTypeStreakBroken, payload, now)
	if err != nil {
		return err
	}
	if err := s.store.Append(ctx, ev); err != nil {
		return fmt.Errorf("streak: append streak.broken (reset): %w", err)
	}
	if err := s.rm.Apply(ev); err != nil {
		return fmt.Errorf("streak: apply streak.broken (reset): %w", err)
	}
	s.logger.Debug("streak: reset",
		"tenant_id", tenantID, "channel", channel, "viewer_id", viewerID, "reason", reason)
	return nil
}

// Status returns a read-only summary suitable for UI signals.
func (s *System) Status(tenantID, channel, viewerID string) Status {
	state := s.rm.Get(tenantID, channel, viewerID)
	return Status{
		DaysCurrent:      state.DaysCurrent,
		DaysLongest:      state.DaysLongest,
		FreezesAvailable: state.FreezesAvailable,
		LastTickAt:       state.LastTickAt,
		NextMilestone:    s.nextMilestoneAfter(state.DaysCurrent),
	}
}

// Leaderboard returns the top-limit current streaks for a channel (or all
// channels when channel == ""). See [ReadModel.Leaderboard] for ordering.
func (s *System) Leaderboard(tenantID, channel string, limit int) []LeaderboardEntry {
	return s.rm.Leaderboard(tenantID, channel, limit)
}

// Recover discards the in-memory read model and rebuilds it from the event
// store. Safe to call on startup or after a crash recovery.
func (s *System) Recover(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return errors.New("streak: tenant id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	fresh := NewReadModel()
	opts := eventsourcing.ReadOptions{
		TenantID: tenantID,
		Types: []string{
			EventTypeStreakStarted,
			EventTypeStreakContinued,
			EventTypeStreakBroken,
			EventTypeStreakFrozen,
			EventTypeStreakMilestone,
		},
	}
	var collected []eventsourcing.Event
	for ev, err := range s.store.Read(ctx, opts) {
		if err != nil {
			return fmt.Errorf("streak: recover read: %w", err)
		}
		collected = append(collected, ev)
	}
	sort.SliceStable(collected, func(i, j int) bool {
		if collected[i].OccurredAt.Equal(collected[j].OccurredAt) {
			return collected[i].ID.String() < collected[j].ID.String()
		}
		return collected[i].OccurredAt.Before(collected[j].OccurredAt)
	})
	for _, ev := range collected {
		if err := fresh.Apply(ev); err != nil {
			return fmt.Errorf("streak: recover apply: %w", err)
		}
	}
	s.rm = fresh
	return nil
}

// nextMilestoneAfter returns the smallest configured milestone strictly
// greater than days, or 0 when none is configured above days.
func (s *System) nextMilestoneAfter(days int) int {
	next := 0
	for m := range s.cfg.FreezeMilestones {
		if m <= days {
			continue
		}
		if next == 0 || m < next {
			next = m
		}
	}
	return next
}

// ---- helpers ----

// streakDay returns the effective UTC calendar day of t, accounting for the
// grace window. A non-zero grace window shifts the day back by 24h when t
// falls within the first `grace` after UTC midnight.
func streakDay(t time.Time, grace time.Duration) time.Time {
	u := t.UTC()
	day := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	if grace > 0 && u.Sub(day) < grace {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

// dayDiff returns the number of whole UTC days between a and b (b - a). Both
// arguments must already be day-truncated via [streakDay]. Negative results
// indicate b is before a (clock skew).
func dayDiff(a, b time.Time) int {
	if a.IsZero() {
		return 0
	}
	delta := b.Sub(a)
	return int(delta / (24 * time.Hour))
}

// validateIdentity enforces that the routing tuple is non-empty before any
// event is shaped.
func validateIdentity(tenantID, channel, viewerID string) error {
	if tenantID == "" {
		return errors.New("streak: tenant id is required")
	}
	if channel == "" {
		return errors.New("streak: channel is required")
	}
	if viewerID == "" {
		return errors.New("streak: viewer id is required")
	}
	return nil
}

// newEvent constructs a single eventsourcing.Event with OccurredAt overridden
// to the System's clock-derived now (so tests get deterministic timestamps).
func newEvent(tenantID, eventType string, payload any, now time.Time) (eventsourcing.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return eventsourcing.Event{}, fmt.Errorf("streak: marshal %s payload: %w", eventType, err)
	}
	ev, err := eventsourcing.NewEvent(tenantID, eventType, raw)
	if err != nil {
		return eventsourcing.Event{}, err
	}
	ev.OccurredAt = now
	return ev, nil
}

type eventSpec struct {
	eventType string
	payload   any
}

// buildEvents serialises several payloads with timestamps that strictly
// increase by one nanosecond per event, so that an OccurredAt sort during
// replay reproduces the same order they were emitted by System. Without
// this nudge events sharing a single now value rely on ULID entropy for
// tie-breaking, which is non-deterministic.
func buildEvents(tenantID string, now time.Time, specs ...eventSpec) ([]eventsourcing.Event, error) {
	out := make([]eventsourcing.Event, 0, len(specs))
	for i, sp := range specs {
		ev, err := newEvent(tenantID, sp.eventType, sp.payload, now.Add(time.Duration(i)*time.Nanosecond))
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

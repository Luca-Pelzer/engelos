package streak

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
)

// stateKey identifies a single viewer's streak ledger within a tenant.
type stateKey struct {
	TenantID string
	Channel  string
	ViewerID string
}

// State is the per-viewer projection of the streak event stream.
//
// A zero-valued State is the legitimate "never seen this viewer" baseline.
// All time fields are stored in UTC.
type State struct {
	TenantID         string
	Channel          string
	ViewerID         string
	Username         string
	DaysCurrent      int
	DaysLongest      int
	FreezesAvailable int
	LastTickDayUTC   time.Time
	LastTickAt       time.Time
	MilestonesHit    map[int]bool
}

// LeaderboardEntry is one row of a streak leaderboard.
type LeaderboardEntry struct {
	Channel     string
	ViewerID    string
	Username    string
	DaysCurrent int
	DaysLongest int
}

// ReadModel is the in-memory projection of the streak event log.
//
// It is safe for concurrent use. [System] is the single canonical writer in
// production, but [ReadModel.Apply] may be invoked from any goroutine - for
// example a background replay during [System.Recover].
type ReadModel struct {
	mu     sync.RWMutex
	states map[stateKey]*State
}

// NewReadModel returns an empty ReadModel.
func NewReadModel() *ReadModel {
	return &ReadModel{states: make(map[stateKey]*State)}
}

// Get returns a snapshot of the viewer state, or a zero value if unknown.
// The returned State is a copy: mutating it does not affect the model.
func (rm *ReadModel) Get(tenantID, channel, viewerID string) State {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	if s, ok := rm.states[stateKey{tenantID, channel, viewerID}]; ok {
		return copyState(s)
	}
	return State{TenantID: tenantID, Channel: channel, ViewerID: viewerID}
}

// Snapshot returns a copy of every known state. Useful for tests and admin
// tooling; do not call on hot paths.
func (rm *ReadModel) Snapshot() []State {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	out := make([]State, 0, len(rm.states))
	for _, s := range rm.states {
		out = append(out, copyState(s))
	}
	return out
}

// Leaderboard returns the top-limit current streaks for the given channel
// (or across all channels in the tenant if channel == ""). The result is
// sorted by (DaysCurrent desc, ViewerID asc) so ordering is stable across
// calls. A limit <= 0 returns the empty slice.
func (rm *ReadModel) Leaderboard(tenantID, channel string, limit int) []LeaderboardEntry {
	if limit <= 0 {
		return nil
	}
	rm.mu.RLock()
	entries := make([]LeaderboardEntry, 0, len(rm.states))
	for k, s := range rm.states {
		if k.TenantID != tenantID {
			continue
		}
		if channel != "" && k.Channel != channel {
			continue
		}
		if s.DaysCurrent <= 0 {
			continue
		}
		entries = append(entries, LeaderboardEntry{
			Channel:     s.Channel,
			ViewerID:    s.ViewerID,
			Username:    s.Username,
			DaysCurrent: s.DaysCurrent,
			DaysLongest: s.DaysLongest,
		})
	}
	rm.mu.RUnlock()

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].DaysCurrent != entries[j].DaysCurrent {
			return entries[i].DaysCurrent > entries[j].DaysCurrent
		}
		return entries[i].ViewerID < entries[j].ViewerID
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

// Apply mutates the read model in response to a stored event. Unknown event
// types are ignored (so the read model stays forward-compatible with new
// streak event types) but malformed payloads of known types return an error.
func (rm *ReadModel) Apply(e eventsourcing.Event) error {
	switch e.Type {
	case EventTypeStreakStarted:
		return rm.applyStarted(e)
	case EventTypeStreakContinued:
		return rm.applyContinued(e)
	case EventTypeStreakBroken:
		return rm.applyBroken(e)
	case EventTypeStreakFrozen:
		return rm.applyFrozen(e)
	case EventTypeStreakMilestone:
		return rm.applyMilestone(e)
	default:
		return nil
	}
}

func (rm *ReadModel) applyStarted(e eventsourcing.Event) error {
	var p StreakStartedPayload
	if err := strictDecode(e.Payload, &p); err != nil {
		return fmt.Errorf("streak: decode streak.started payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	if p.Username != "" {
		s.Username = p.Username
	}
	s.DaysCurrent = 1
	if s.DaysLongest < 1 {
		s.DaysLongest = 1
	}
	s.LastTickAt = e.OccurredAt
	s.LastTickDayUTC = streakDay(e.OccurredAt, 0)
	s.MilestonesHit = make(map[int]bool)
	return nil
}

func (rm *ReadModel) applyContinued(e eventsourcing.Event) error {
	var p StreakContinuedPayload
	if err := strictDecode(e.Payload, &p); err != nil {
		return fmt.Errorf("streak: decode streak.continued payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	if p.Username != "" {
		s.Username = p.Username
	}
	s.DaysCurrent = p.DaysCurrent
	if p.DaysLongest > s.DaysLongest {
		s.DaysLongest = p.DaysLongest
	}
	s.LastTickAt = e.OccurredAt
	if !p.SameDayReTick {
		s.LastTickDayUTC = streakDay(e.OccurredAt, 0)
	}
	return nil
}

func (rm *ReadModel) applyBroken(e eventsourcing.Event) error {
	var p StreakBrokenPayload
	if err := strictDecode(e.Payload, &p); err != nil {
		return fmt.Errorf("streak: decode streak.broken payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	if p.Username != "" {
		s.Username = p.Username
	}
	s.DaysCurrent = 0
	s.MilestonesHit = make(map[int]bool)
	s.LastTickAt = e.OccurredAt
	return nil
}

func (rm *ReadModel) applyFrozen(e eventsourcing.Event) error {
	var p StreakFrozenPayload
	if err := strictDecode(e.Payload, &p); err != nil {
		return fmt.Errorf("streak: decode streak.frozen payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	if p.Username != "" {
		s.Username = p.Username
	}
	s.FreezesAvailable = p.FreezesRemain
	s.DaysCurrent = p.DaysCurrent
	if p.DaysCurrent > s.DaysLongest {
		s.DaysLongest = p.DaysCurrent
	}
	s.LastTickAt = e.OccurredAt
	s.LastTickDayUTC = streakDay(e.OccurredAt, 0)
	return nil
}

func (rm *ReadModel) applyMilestone(e eventsourcing.Event) error {
	var p StreakMilestonePayload
	if err := strictDecode(e.Payload, &p); err != nil {
		return fmt.Errorf("streak: decode streak.milestone payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	if p.Username != "" {
		s.Username = p.Username
	}
	if s.MilestonesHit == nil {
		s.MilestonesHit = make(map[int]bool)
	}
	s.MilestonesHit[p.Milestone] = true
	s.FreezesAvailable = p.FreezesTotal
	return nil
}

// getOrCreate must be called with rm.mu held.
func (rm *ReadModel) getOrCreate(tenantID, channel, viewerID string) *State {
	k := stateKey{tenantID, channel, viewerID}
	if s, ok := rm.states[k]; ok {
		return s
	}
	s := &State{
		TenantID:      tenantID,
		Channel:       channel,
		ViewerID:      viewerID,
		MilestonesHit: make(map[int]bool),
	}
	rm.states[k] = s
	return s
}

// copyState returns a deep copy of s suitable for handing to callers.
func copyState(s *State) State {
	out := *s
	if s.MilestonesHit != nil {
		out.MilestonesHit = make(map[int]bool, len(s.MilestonesHit))
		for k, v := range s.MilestonesHit {
			out.MilestonesHit[k] = v
		}
	}
	return out
}

// strictDecode unmarshals JSON into v while rejecting unknown fields. This
// keeps event-payload schema drift loud rather than silent.
func strictDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

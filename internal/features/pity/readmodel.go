package pity

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
)

// stateKey identifies a single viewer's pity ledger within a tenant.
type stateKey struct {
	TenantID string
	Channel  string
	ViewerID string
}

// State is the per-viewer projection of the pity event stream.
//
// A zero-valued State is the legitimate "never seen this viewer" baseline.
// All time fields are stored in UTC.
type State struct {
	TenantID         string
	Channel          string
	ViewerID         string
	Username         string
	Points           int
	LastWinAt        time.Time
	PointsThisWindow int
	WindowStartedAt  time.Time
	UpdatedAt        time.Time
}

// ReadModel is the in-memory projection of the pity event log.
//
// It is safe for concurrent use. [System] is the single canonical writer in
// production, but [ReadModel.Apply] may be invoked from any goroutine — for
// example a background replay during [System.Recover].
type ReadModel struct {
	mu             sync.RWMutex
	states         map[stateKey]*State
	windowDuration time.Duration
}

// NewReadModel returns an empty ReadModel.
func NewReadModel() *ReadModel {
	return &ReadModel{states: make(map[stateKey]*State)}
}

// WithWindowDuration enables timestamp-driven window rollover during
// [ReadModel.Apply] of [EventTypePointsGranted]. When unset (the default),
// PointsThisWindow accumulates monotonically — fine for naive consumers, but
// callers that want correct rate-limit replay (notably [System.Recover]) must
// set the window so replay matches live behaviour.
//
// Returns rm for chaining.
func (rm *ReadModel) WithWindowDuration(d time.Duration) *ReadModel {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.windowDuration = d
	return rm
}

// Get returns a snapshot of the viewer state, or a zero value if unknown.
// The returned State is a copy: mutating it does not affect the model.
func (rm *ReadModel) Get(tenantID, channel, viewerID string) State {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	if s, ok := rm.states[stateKey{tenantID, channel, viewerID}]; ok {
		return *s
	}
	return State{TenantID: tenantID, Channel: channel, ViewerID: viewerID}
}

// Snapshot returns a copy of every known state. Useful for tests and
// admin tooling; do not call on hot paths.
func (rm *ReadModel) Snapshot() []State {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	out := make([]State, 0, len(rm.states))
	for _, s := range rm.states {
		out = append(out, *s)
	}
	return out
}

// Apply mutates the read model in response to a stored event. Unknown event
// types are ignored (so the read model stays forward-compatible with new pity
// event types) but malformed payloads of known types return an error.
func (rm *ReadModel) Apply(e eventsourcing.Event) error {
	switch e.Type {
	case EventTypePointsGranted:
		return rm.applyPointsGranted(e)
	case EventTypeRollMade:
		return rm.applyRollMade(e)
	case EventTypeWinGuaranteed, EventTypeWinNatural:
		return rm.applyWin(e)
	case EventTypeReset:
		return rm.applyReset(e)
	default:
		return nil
	}
}

func (rm *ReadModel) applyPointsGranted(e eventsourcing.Event) error {
	var p PointsGrantedPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("pity: decode points-granted payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	if p.Username != "" {
		s.Username = p.Username
	}
	if rm.windowDuration > 0 {
		if s.WindowStartedAt.IsZero() || e.OccurredAt.Sub(s.WindowStartedAt) >= rm.windowDuration {
			s.WindowStartedAt = e.OccurredAt
			s.PointsThisWindow = 0
		}
	} else if s.WindowStartedAt.IsZero() {
		s.WindowStartedAt = e.OccurredAt
	}
	s.Points = p.NewTotal
	s.PointsThisWindow += p.Amount
	s.UpdatedAt = e.OccurredAt
	return nil
}

func (rm *ReadModel) applyRollMade(e eventsourcing.Event) error {
	var p RollMadePayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("pity: decode roll-made payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	if p.Username != "" {
		s.Username = p.Username
	}
	s.UpdatedAt = e.OccurredAt
	return nil
}

func (rm *ReadModel) applyWin(e eventsourcing.Event) error {
	var p WinPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("pity: decode win payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	if p.Username != "" {
		s.Username = p.Username
	}
	s.LastWinAt = e.OccurredAt
	s.UpdatedAt = e.OccurredAt
	return nil
}

func (rm *ReadModel) applyReset(e eventsourcing.Event) error {
	var p ResetPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("pity: decode reset payload: %w", err)
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := rm.getOrCreate(e.TenantID, p.Channel, p.ViewerID)
	s.Points = 0
	s.UpdatedAt = e.OccurredAt
	return nil
}

// getOrCreate must be called with rm.mu held.
func (rm *ReadModel) getOrCreate(tenantID, channel, viewerID string) *State {
	k := stateKey{tenantID, channel, viewerID}
	if s, ok := rm.states[k]; ok {
		return s
	}
	s := &State{TenantID: tenantID, Channel: channel, ViewerID: viewerID}
	rm.states[k] = s
	return s
}

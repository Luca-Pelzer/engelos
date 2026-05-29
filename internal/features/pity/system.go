package pity

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"sort"
	"sync"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
)

// RngSource is the minimal random-number contract used by [System].
//
// Production injects a crypto/rand-backed implementation; tests inject a
// seeded PCG so rolls are replayable. Implementations must be safe for
// concurrent use only if the caller does not serialise access — [System]
// serialises on its own mutex, so single-threaded implementations are fine.
type RngSource interface {
	// Float64 returns a uniform sample in the half-open interval [0, 1).
	Float64() float64
	// Seed re-keys the source. Implementations backed by crypto/rand
	// treat this as a no-op.
	Seed(seed int64)
}

// System is the Pity-System aggregate.
//
// A System owns no goroutines and is safe for concurrent use; all mutating
// methods serialise on an internal mutex so a single viewer's grant/roll
// sequence is linearisable.
type System struct {
	cfg    Config
	store  eventsourcing.EventStore
	rm     *ReadModel
	rng    RngSource
	logger *slog.Logger
	clock  func() time.Time
	seed   int64

	mu sync.Mutex
}

// New constructs a System. It validates cfg, defaults the logger when nil and
// installs a crypto/rand RngSource. Use [System.WithRng] and [System.WithClock]
// in tests to inject deterministic dependencies.
func New(cfg Config, store eventsourcing.EventStore, logger *slog.Logger) (*System, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("pity: event store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &System{
		cfg:    cfg,
		store:  store,
		rm:     NewReadModel().WithWindowDuration(cfg.WindowDuration),
		rng:    newCryptoRng(cryptorand.Reader),
		logger: logger,
		clock:  func() time.Time { return time.Now().UTC() },
	}, nil
}

// WithRng replaces the RNG. Returns s for chaining.
func (s *System) WithRng(rng RngSource) *System {
	if rng == nil {
		return s
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rng = rng
	return s
}

// WithClock replaces the clock function. Returns s for chaining.
func (s *System) WithClock(clock func() time.Time) *System {
	if clock == nil {
		return s
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = clock
	return s
}

// WithSeed records a seed value that is stamped into emitted
// [RollMadePayload]s. It does NOT itself reseed the RNG — call
// rng.Seed(seed) on a [SeededRng] separately if you want both behaviours.
//
// Production code with crypto/rand should leave this as zero.
func (s *System) WithSeed(seed int64) *System {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seed = seed
	return s
}

// Config returns a copy of the active configuration.
func (s *System) Config() Config { return s.cfg }

// ReadModel exposes the in-memory projection. Useful for read-side queries
// without mutating the System.
func (s *System) ReadModel() *ReadModel { return s.rm }

// EffectiveChance computes the win probability for a given point total.
// The formula is documented on the package overview.
func (s *System) EffectiveChance(points int) float64 {
	cfg := s.cfg
	if points >= cfg.HardPityThreshold {
		return 1.0
	}
	soft := cfg.SoftPityThreshold()
	if points < soft {
		return cfg.BaseWinChance
	}
	span := float64(cfg.HardPityThreshold - soft)
	if span <= 0 {
		return 1.0
	}
	progress := float64(points-soft) / span
	return cfg.BaseWinChance + (1.0-cfg.BaseWinChance)*progress
}

// Status returns a read-only summary suitable for UI signals.
func (s *System) Status(tenantID, channel, viewerID string) Status {
	state := s.rm.Get(tenantID, channel, viewerID)
	chance := s.EffectiveChance(state.Points)
	return Status{
		Points:          state.Points,
		SoftPityHit:     state.Points >= s.cfg.SoftPityThreshold(),
		NearGuaranteed:  state.Points >= s.cfg.HardPityThreshold,
		EffectiveChance: chance,
	}
}

// Leaderboard returns the top-limit viewers by pity points for a channel (or
// all channels when channel == ""). See [ReadModel.Leaderboard] for ordering.
func (s *System) Leaderboard(tenantID, channel string, limit int) []LeaderboardEntry {
	return s.rm.Leaderboard(tenantID, channel, limit)
}

// Status is a read-only snapshot of the viewer's current pity standing.
type Status struct {
	Points          int
	SoftPityHit     bool
	NearGuaranteed  bool
	EffectiveChance float64
}

// RollResult is returned by [System.Roll].
type RollResult struct {
	Won             bool
	WasGuaranteed   bool
	PointsBefore    int
	PointsAfter     int
	EffectiveChance float64
}

// GrantPoints credits a viewer with up to `amount` pity points. The actual
// amount granted may be reduced by the rate limit; the return value is the
// new running total. When the cap zeroes out the grant, no event is written
// and the call is a no-op apart from window bookkeeping.
func (s *System) GrantPoints(
	ctx context.Context,
	tenantID, channel, viewerID, username, reason string,
	amount int,
) (int, error) {
	if err := validateIdentity(tenantID, channel, viewerID); err != nil {
		return 0, err
	}
	if amount < 0 {
		return 0, fmt.Errorf("pity: grant amount must be >= 0, got %d", amount)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock().UTC()
	current := s.snapshot(tenantID, channel, viewerID)
	s.applyWindowRoll(&current, now)

	effective := amount
	if s.cfg.MaxPointsPerWindow > 0 {
		remaining := s.cfg.MaxPointsPerWindow - current.PointsThisWindow
		if remaining < 0 {
			remaining = 0
		}
		if effective > remaining {
			effective = remaining
		}
	}
	if effective <= 0 {
		return current.Points, nil
	}

	newTotal := current.Points + effective
	payload := PointsGrantedPayload{
		Channel:  channel,
		ViewerID: viewerID,
		Username: username,
		Amount:   effective,
		NewTotal: newTotal,
		Reason:   reason,
	}
	ev, err := newEvent(tenantID, EventTypePointsGranted, payload, now)
	if err != nil {
		return current.Points, err
	}
	if err := s.store.Append(ctx, ev); err != nil {
		return current.Points, fmt.Errorf("pity: append points-granted: %w", err)
	}
	if err := s.rm.Apply(ev); err != nil {
		s.logger.Error("pity: read model rejected points-granted", "err", err, "event_id", ev.ID.String())
		return current.Points, fmt.Errorf("pity: apply points-granted: %w", err)
	}
	return newTotal, nil
}

// Roll evaluates the pity dice for the given viewer. The return value carries
// the before/after point counts, the chance that was used, and whether the win
// (if any) was guaranteed by hitting the hard-pity threshold.
func (s *System) Roll(ctx context.Context, tenantID, channel, viewerID, username string) (RollResult, error) {
	if err := validateIdentity(tenantID, channel, viewerID); err != nil {
		return RollResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock().UTC()
	before := s.snapshot(tenantID, channel, viewerID)
	chance := s.EffectiveChance(before.Points)

	guaranteed := before.Points >= s.cfg.HardPityThreshold
	var draw float64
	if guaranteed {
		draw = 0.0
	} else {
		draw = s.rng.Float64()
	}
	won := guaranteed || draw < chance

	result := RollResult{
		Won:             won,
		WasGuaranteed:   guaranteed,
		PointsBefore:    before.Points,
		PointsAfter:     before.Points,
		EffectiveChance: chance,
	}

	if won {
		winType := EventTypeWinNatural
		if guaranteed {
			winType = EventTypeWinGuaranteed
		}
		winPayload := WinPayload{
			Channel:       channel,
			ViewerID:      viewerID,
			Username:      username,
			PointsAtWin:   before.Points,
			WasGuaranteed: guaranteed,
		}
		rollPayload := RollMadePayload{
			Channel:         channel,
			ViewerID:        viewerID,
			Username:        username,
			PointsAtRoll:    before.Points,
			EffectiveChance: chance,
			Won:             true,
			WasGuaranteed:   guaranteed,
			RngSeed:         s.seed,
		}
		resetPayload := ResetPayload{
			Channel:  channel,
			ViewerID: viewerID,
			Reason:   "win",
		}
		evs, err := buildEvents(tenantID, now,
			eventSpec{winType, winPayload},
			eventSpec{EventTypeRollMade, rollPayload},
			eventSpec{EventTypeReset, resetPayload},
		)
		if err != nil {
			return result, err
		}
		if err := s.store.AppendBatch(ctx, evs); err != nil {
			return result, fmt.Errorf("pity: append win batch: %w", err)
		}
		for _, ev := range evs {
			if err := s.rm.Apply(ev); err != nil {
				s.logger.Error("pity: read model rejected win event", "err", err, "type", ev.Type)
				return result, fmt.Errorf("pity: apply %s: %w", ev.Type, err)
			}
		}
		result.PointsAfter = 0
		return result, nil
	}

	rollPayload := RollMadePayload{
		Channel:         channel,
		ViewerID:        viewerID,
		Username:        username,
		PointsAtRoll:    before.Points,
		EffectiveChance: chance,
		Won:             false,
		WasGuaranteed:   false,
		RngSeed:         s.seed,
	}
	ev, err := newEvent(tenantID, EventTypeRollMade, rollPayload, now)
	if err != nil {
		return result, err
	}
	if err := s.store.Append(ctx, ev); err != nil {
		return result, fmt.Errorf("pity: append roll-made: %w", err)
	}
	if err := s.rm.Apply(ev); err != nil {
		s.logger.Error("pity: read model rejected roll-made", "err", err, "event_id", ev.ID.String())
		return result, fmt.Errorf("pity: apply roll-made: %w", err)
	}
	return result, nil
}

// Reset clears a viewer's accumulated points. Reason describes the trigger
// ("win", "admin", "config-change") and is stored verbatim on the event.
func (s *System) Reset(ctx context.Context, tenantID, channel, viewerID, reason string) error {
	if err := validateIdentity(tenantID, channel, viewerID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock().UTC()
	payload := ResetPayload{Channel: channel, ViewerID: viewerID, Reason: reason}
	ev, err := newEvent(tenantID, EventTypeReset, payload, now)
	if err != nil {
		return err
	}
	if err := s.store.Append(ctx, ev); err != nil {
		return fmt.Errorf("pity: append reset: %w", err)
	}
	if err := s.rm.Apply(ev); err != nil {
		return fmt.Errorf("pity: apply reset: %w", err)
	}
	return nil
}

// Recover discards the in-memory read model and rebuilds it from the event
// store. Safe to call on startup or after a crash recovery.
func (s *System) Recover(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return errors.New("pity: tenant id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	fresh := NewReadModel().WithWindowDuration(s.cfg.WindowDuration)
	opts := eventsourcing.ReadOptions{
		TenantID: tenantID,
		Types: []string{
			EventTypePointsGranted,
			EventTypeRollMade,
			EventTypeWinGuaranteed,
			EventTypeWinNatural,
			EventTypeReset,
		},
	}
	var collected []eventsourcing.Event
	for ev, err := range s.store.Read(ctx, opts) {
		if err != nil {
			return fmt.Errorf("pity: recover read: %w", err)
		}
		collected = append(collected, ev)
	}
	// The store orders by ULID, which on production hardware is effectively a
	// monotonic timestamp ordering. We sort by (OccurredAt, ID) before
	// applying so that replay is robust against sub-millisecond ULID entropy
	// collisions — important when many events are emitted within the same
	// millisecond, as happens under load or with a fake clock in tests.
	sort.SliceStable(collected, func(i, j int) bool {
		if collected[i].OccurredAt.Equal(collected[j].OccurredAt) {
			return collected[i].ID.String() < collected[j].ID.String()
		}
		return collected[i].OccurredAt.Before(collected[j].OccurredAt)
	})
	for _, ev := range collected {
		if err := fresh.Apply(ev); err != nil {
			return fmt.Errorf("pity: recover apply: %w", err)
		}
	}
	s.rm = fresh
	return nil
}

// snapshot reads the current state under s.mu (caller is responsible for
// holding the lock).
func (s *System) snapshot(tenantID, channel, viewerID string) State {
	return s.rm.Get(tenantID, channel, viewerID)
}

// applyWindowRoll mutates the *in-memory* state directly when its window has
// expired. This is a pure projection cleanup; no event needs to be persisted
// because the next granted event will carry the recomputed window total.
func (s *System) applyWindowRoll(state *State, now time.Time) {
	if s.cfg.WindowDuration <= 0 || s.cfg.MaxPointsPerWindow == 0 {
		return
	}
	if state.WindowStartedAt.IsZero() || now.Sub(state.WindowStartedAt) >= s.cfg.WindowDuration {
		state.WindowStartedAt = now
		state.PointsThisWindow = 0
		s.rm.mu.Lock()
		live := s.rm.getOrCreate(state.TenantID, state.Channel, state.ViewerID)
		live.WindowStartedAt = now
		live.PointsThisWindow = 0
		s.rm.mu.Unlock()
	}
}

// validateIdentity enforces that the routing tuple is non-empty before any
// event is shaped.
func validateIdentity(tenantID, channel, viewerID string) error {
	if tenantID == "" {
		return errors.New("pity: tenant id is required")
	}
	if channel == "" {
		return errors.New("pity: channel is required")
	}
	if viewerID == "" {
		return errors.New("pity: viewer id is required")
	}
	return nil
}

// newEvent constructs a single eventsourcing.Event with OccurredAt overridden
// to the System's clock-derived now (so tests get deterministic timestamps).
func newEvent(tenantID, eventType string, payload any, now time.Time) (eventsourcing.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return eventsourcing.Event{}, fmt.Errorf("pity: marshal %s payload: %w", eventType, err)
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

// buildEvents serialises several payloads with the same OccurredAt timestamp,
// preserving order so that downstream readers see win → roll → reset.
func buildEvents(tenantID string, now time.Time, specs ...eventSpec) ([]eventsourcing.Event, error) {
	out := make([]eventsourcing.Event, 0, len(specs))
	for _, sp := range specs {
		ev, err := newEvent(tenantID, sp.eventType, sp.payload, now)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

// ---- RNG implementations ----

// cryptoRng draws 8 bytes from a crypto/rand.Reader on every Float64 call.
type cryptoRng struct {
	mu     sync.Mutex
	reader io.Reader
}

func newCryptoRng(r io.Reader) *cryptoRng {
	if r == nil {
		r = cryptorand.Reader
	}
	return &cryptoRng{reader: r}
}

func (c *cryptoRng) Float64() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var buf [8]byte
	if _, err := io.ReadFull(c.reader, buf[:]); err != nil {
		// Crypto/rand never returns an error under normal conditions; if it
		// does the only safe behaviour is to return zero so the caller treats
		// the roll as a baseline win-eligibility check. Logging happens at the
		// System layer because the RngSource interface has no logger.
		return 0
	}
	// Normalise the high 53 bits to [0, 1) the same way math/rand/v2 does.
	u := binary.BigEndian.Uint64(buf[:]) >> 11
	return float64(u) / (1 << 53)
}

func (c *cryptoRng) Seed(_ int64) {}

// SeededRng wraps math/rand/v2's PCG generator so tests get deterministic
// streams while still satisfying [RngSource].
type SeededRng struct {
	mu sync.Mutex
	r  *mathrand.Rand
}

// NewSeededRng returns a SeededRng seeded with (seed, seed^0x9E3779B97F4A7C15).
// The second value is a fixed Weyl constant so different seeds produce
// distinct streams even on poor user input.
func NewSeededRng(seed int64) *SeededRng {
	return &SeededRng{r: mathrand.New(mathrand.NewPCG(uint64(seed), uint64(seed)^0x9E3779B97F4A7C15))}
}

func (s *SeededRng) Float64() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.Float64()
}

func (s *SeededRng) Seed(seed int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.r = mathrand.New(mathrand.NewPCG(uint64(seed), uint64(seed)^0x9E3779B97F4A7C15))
}

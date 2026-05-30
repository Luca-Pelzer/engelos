package automodstate

import (
	"strings"
	"sync"
	"time"
)

// Action is the punishment to apply, expressed as a neutral string the caller
// maps to a concrete platform call (Twitch /timeout, Discord ban, etc.). Keeping
// it platform-agnostic lets this package stand alone with no adapter imports.
type Action string

const (
	// ActionWarn deletes the offending message without timing the user out.
	ActionWarn Action = "warn"
	// ActionTimeout temporarily mutes the user for the Tier's Timeout duration.
	ActionTimeout Action = "timeout"
	// ActionBan permanently removes the user from the channel.
	ActionBan Action = "ban"
)

// Tier is one rung of the escalation ladder: the Action to take and, for
// ActionTimeout, how long the timeout lasts. Timeout is ignored for warn/ban.
type Tier struct {
	// Action is the punishment for this rung.
	Action Action
	// Timeout is the mute duration; meaningful only when Action is ActionTimeout.
	Timeout time.Duration
}

// DefaultTiers returns the researched escalation progression used by AutoMod:
// 1st offense = warn, 2nd = 60s timeout, 3rd = 10m, 4th = 24h, 5th and beyond =
// permanent ban. Callers may supply their own ladder to NewEscalator instead.
func DefaultTiers() []Tier {
	return []Tier{
		{ActionWarn, 0},
		{ActionTimeout, 60 * time.Second},
		{ActionTimeout, 10 * time.Minute},
		{ActionTimeout, 24 * time.Hour},
		{ActionBan, 0},
	}
}

// Escalator tracks per-(channel, user, filter) offense counts and maps each new
// violation onto an escalation ladder. Offenses older than the decay window are
// forgotten, so a user who behaves for a while starts fresh. All methods are
// safe for concurrent use.
type Escalator struct {
	mu      sync.Mutex
	tiers   []Tier
	decay   time.Duration       // offenses older than this are forgotten
	now     func() time.Time    // injectable clock for tests
	records map[string]*offense // key = channel|user|filter
}

// offense is the mutable per-key state: how many violations have accrued within
// the decay window and when the most recent one occurred.
type offense struct {
	count       int
	lastOffense time.Time
}

// NewEscalator returns an Escalator using the given tier ladder and decay
// window. If tiers is empty it falls back to DefaultTiers so callers can never
// end up with an unusable (zero-rung) ladder. The clock defaults to time.Now.
func NewEscalator(tiers []Tier, decay time.Duration) *Escalator {
	if len(tiers) == 0 {
		tiers = DefaultTiers()
	}
	return &Escalator{
		tiers:   tiers,
		decay:   decay,
		now:     time.Now,
		records: make(map[string]*offense),
	}
}

// WithClock overrides the time source used for decay calculations, enabling
// deterministic tests. It returns the same *Escalator for chaining.
func (e *Escalator) WithClock(now func() time.Time) *Escalator {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = now
	return e
}

// escalKey builds the canonical map key for a (channel, user, filter) triple.
// Each component is trimmed and lower-cased so "#Chan" / "chan" and "User" /
// "user" collapse to the same offender, matching how chat platforms treat
// case-insensitive names.
func escalKey(channel, user, filter string) string {
	return strings.ToLower(strings.TrimSpace(channel)) + "|" +
		strings.ToLower(strings.TrimSpace(user)) + "|" +
		strings.ToLower(strings.TrimSpace(filter))
}

// Record registers a new violation for (channel, user, filter) and returns the
// Action and (for timeouts) duration to apply now. It increments the per-key
// counter — first resetting it to zero if the previous offense is older than the
// decay window — then maps the resulting 1-based count onto the tier ladder.
// Counts beyond the final rung clamp to the last tier (a ban under DefaultTiers).
func (e *Escalator) Record(channel, user, filter string) (Action, time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	key := escalKey(channel, user, filter)
	now := e.now()

	rec := e.records[key]
	if rec == nil {
		rec = &offense{}
		e.records[key] = rec
	}
	// Decay: if the last offense is older than the window, the slate is wiped
	// before this violation counts, so a previously-warned user who reoffends
	// after the decay period is treated as a first-timer again.
	if rec.count > 0 && now.Sub(rec.lastOffense) > e.decay {
		rec.count = 0
	}
	rec.count++
	rec.lastOffense = now

	// Clamp: the count is 1-based; index into tiers with count-1, but a count
	// past the final rung stays pinned to the last (most severe) tier so a
	// persistent offender keeps getting the maximum punishment.
	idx := rec.count - 1
	if idx >= len(e.tiers) {
		idx = len(e.tiers) - 1
	}
	t := e.tiers[idx]
	return t.Action, t.Timeout
}

// Offenses returns the current offense count for a key as seen after applying
// decay (a stale record reads as 0). It is read-only and intended for dashboard
// / inspection use; it does not mutate the stored record.
func (e *Escalator) Offenses(channel, user, filter string) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	rec := e.records[escalKey(channel, user, filter)]
	if rec == nil {
		return 0
	}
	if rec.count > 0 && e.now().Sub(rec.lastOffense) > e.decay {
		return 0
	}
	return rec.count
}

// Reset clears every offense record for (channel, user) across all filters, e.g.
// when a moderator forgives the user. It is a no-op if the user has no records.
func (e *Escalator) Reset(channel, user string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	prefix := strings.ToLower(strings.TrimSpace(channel)) + "|" +
		strings.ToLower(strings.TrimSpace(user)) + "|"
	for k := range e.records {
		if strings.HasPrefix(k, prefix) {
			delete(e.records, k)
		}
	}
}

// PermitTracker handles the link `!permit <user>` flow: a moderator grants a
// user a short window during which they may post one link without tripping the
// link filter. All methods are safe for concurrent use.
type PermitTracker struct {
	mu      sync.Mutex
	window  time.Duration
	now     func() time.Time
	permits map[string]time.Time // key = channel|user -> expiry
}

// NewPermitTracker returns a PermitTracker whose grants stay valid for window.
// The clock defaults to time.Now and can be overridden with WithClock.
func NewPermitTracker(window time.Duration) *PermitTracker {
	return &PermitTracker{
		window:  window,
		now:     time.Now,
		permits: make(map[string]time.Time),
	}
}

// WithClock overrides the time source, enabling deterministic tests. It returns
// the same *PermitTracker for chaining.
func (p *PermitTracker) WithClock(now func() time.Time) *PermitTracker {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.now = now
	return p
}

// permitKey builds the canonical (channel, user) key, trimmed and lower-cased to
// match Consume's lookup regardless of how the name was cased in chat.
func permitKey(channel, user string) string {
	return strings.ToLower(strings.TrimSpace(channel)) + "|" +
		strings.ToLower(strings.TrimSpace(user))
}

// Grant gives (channel, user) a permit that expires window from now. Re-granting
// simply refreshes the expiry.
func (p *PermitTracker) Grant(channel, user string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.permits[permitKey(channel, user)] = p.now().Add(p.window)
}

// Consume reports whether (channel, user) holds a currently-valid permit and, if
// so, removes it so it cannot be used twice. A true result means "skip the link
// filter this once". Expired permits return false and are evicted on access.
func (p *PermitTracker) Consume(channel, user string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := permitKey(channel, user)
	expiry, ok := p.permits[key]
	if !ok {
		return false
	}
	// Consume-once: whether the permit is valid or already expired we delete it
	// on access. A valid one is "used up" (true); an expired one is garbage-
	// collected and reported as false.
	delete(p.permits, key)
	return !p.now().After(expiry)
}

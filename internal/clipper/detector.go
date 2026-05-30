package clipper

import (
	"math"
	"sync"
	"time"
)

// Reason labels why a clip moment fired.
type Reason string

const (
	// ReasonChatSpike fires when chat activity in the window jumps well above
	// the adaptive baseline.
	ReasonChatSpike Reason = "chat-spike"
	// ReasonSubBurst fires when several subscriptions land close together.
	ReasonSubBurst Reason = "sub-burst"
	// ReasonRaid fires when the channel is raided.
	ReasonRaid Reason = "raid"
)

// Options tunes the hype detector. The zero value is not usable; build it with
// [DefaultOptions] and adjust as needed.
type Options struct {
	// Window is the rolling window over which message activity is measured.
	Window time.Duration
	// BaselineHalfLife is the half-life of the EWMA that tracks the "normal"
	// per-window rate, so a sustained high rate gradually becomes the new
	// normal and stops firing.
	BaselineHalfLife time.Duration
	// SpikeFactor is the multiple of the baseline the current windowed rate
	// must reach to count as a chat spike.
	SpikeFactor float64
	// MinMessages is the minimum number of real messages in the window before
	// a chat spike can fire, so quiet channels do not trigger on noise.
	MinMessages int
	// Cooldown is the minimum spacing between fires for a single channel.
	Cooldown time.Duration
	// SubBoost is the virtual-message weight a single sub adds to the window.
	SubBoost float64
	// RaidBoost scales the virtual weight a raid injects: RaidBoost*sqrt(viewers).
	RaidBoost float64
}

// DefaultOptions returns production defaults tuned for a mid-sized channel.
func DefaultOptions() Options {
	return Options{
		Window:           10 * time.Second,
		BaselineHalfLife: 2 * time.Minute,
		SpikeFactor:      3.0,
		MinMessages:      8,
		Cooldown:         90 * time.Second,
		SubBoost:         5,
		RaidBoost:        10,
	}
}

// weightedEvent is one event retained in the rolling window with its weight
// (1 for a real chat message, larger for the virtual weight of subs/raids) and
// whether it counts toward the MinMessages real-message floor.
type weightedEvent struct {
	at     time.Time
	weight float64
	real   bool
}

// channelState is the per-channel detector memory.
type channelState struct {
	events   []weightedEvent
	baseline float64 // EWMA of windowed weight, the "normal" rate
	baseInit bool
	lastTick time.Time
	lastSub  time.Time
	subCount int
	lastFire time.Time
}

// Detector tracks per-channel chat/sub/raid activity and decides when a
// clip-worthy moment happens. It is safe for concurrent use.
//
// The baseline is an exponentially weighted moving average of the windowed
// activity that decays toward the current rate with BaselineHalfLife. A chat
// spike fires only when the current windowed weight exceeds SpikeFactor times
// that baseline (and the real-message floor and cooldown are satisfied). On a
// fire the baseline is nudged up so an identical follow-on burst does not
// immediately retrigger after the cooldown, and a sustained high rate raises
// the baseline until spikes stop, preventing endless firing.
//
// All methods take an explicit now so callers (and tests) control time; the
// detector never reads the clock itself.
type Detector struct {
	opts Options
	mu   sync.Mutex
	byCh map[string]*channelState
}

// New builds a Detector with opts. Non-positive fields fall back to the
// corresponding [DefaultOptions] value so a partially-filled Options is safe.
func New(opts Options) *Detector {
	d := DefaultOptions()
	if opts.Window > 0 {
		d.Window = opts.Window
	}
	if opts.BaselineHalfLife > 0 {
		d.BaselineHalfLife = opts.BaselineHalfLife
	}
	if opts.SpikeFactor > 0 {
		d.SpikeFactor = opts.SpikeFactor
	}
	if opts.MinMessages > 0 {
		d.MinMessages = opts.MinMessages
	}
	if opts.Cooldown > 0 {
		d.Cooldown = opts.Cooldown
	}
	if opts.SubBoost > 0 {
		d.SubBoost = opts.SubBoost
	}
	if opts.RaidBoost > 0 {
		d.RaidBoost = opts.RaidBoost
	}
	return &Detector{opts: d, byCh: make(map[string]*channelState)}
}

func (d *Detector) state(channel string) *channelState {
	s := d.byCh[channel]
	if s == nil {
		s = &channelState{}
		d.byCh[channel] = s
	}
	return s
}

// prune drops events older than the window and returns the surviving weighted
// sum and real-message count.
func (s *channelState) prune(now time.Time, window time.Duration) (float64, int) {
	cutoff := now.Add(-window)
	kept := s.events[:0]
	var weight float64
	var real int
	for _, e := range s.events {
		if e.at.After(cutoff) {
			kept = append(kept, e)
			weight += e.weight
			if e.real {
				real++
			}
		}
	}
	s.events = kept
	return weight, real
}

// updateBaseline decays the EWMA toward the observed windowed weight using the
// time elapsed since the last update and the configured half-life.
func (s *channelState) updateBaseline(now time.Time, observed, halfLife float64) {
	if !s.baseInit {
		s.baseline = observed
		s.baseInit = true
		s.lastTick = now
		return
	}
	elapsed := now.Sub(s.lastTick).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	// alpha is the weight given to the OLD baseline: 0.5^(elapsed/halfLife).
	alpha := math.Pow(0.5, elapsed/halfLife)
	s.baseline = alpha*s.baseline + (1-alpha)*observed
	s.lastTick = now
}

// Message records a chat message at now and reports whether it triggers a clip
// moment via a chat spike (respecting the cooldown).
func (d *Detector) Message(channel string, now time.Time) (bool, Reason) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.state(channel)
	s.events = append(s.events, weightedEvent{at: now, weight: 1, real: true})
	weight, real := s.prune(now, d.opts.Window)

	baseline := s.baseline
	s.updateBaseline(now, weight, d.opts.BaselineHalfLife.Seconds())

	if real < d.opts.MinMessages {
		return false, ""
	}
	if !s.baseInit || baseline <= 0 {
		return false, ""
	}
	if weight < d.opts.SpikeFactor*baseline {
		return false, ""
	}
	if !d.canFire(s, now) {
		return false, ""
	}
	d.fire(s, now, weight)
	return true, ReasonChatSpike
}

// Sub records a subscription at now. It fires reason sub-burst when two or more
// subs land within the window (respecting the cooldown), and injects SubBoost
// virtual weight so a following chat surge is contextualised.
func (d *Detector) Sub(channel string, now time.Time) (bool, Reason) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.state(channel)
	s.events = append(s.events, weightedEvent{at: now, weight: d.opts.SubBoost, real: false})
	s.prune(now, d.opts.Window)

	if !s.lastSub.IsZero() && now.Sub(s.lastSub) <= d.opts.Window {
		s.subCount++
	} else {
		s.subCount = 1
	}
	s.lastSub = now

	if s.subCount >= 2 && d.canFire(s, now) {
		s.subCount = 0
		weight, _ := s.prune(now, d.opts.Window)
		d.fire(s, now, weight)
		return true, ReasonSubBurst
	}
	return false, ""
}

// Raid records a raid of viewers at now. It fires reason raid immediately
// (respecting the cooldown) and injects RaidBoost*sqrt(viewers) virtual weight.
func (d *Detector) Raid(channel string, viewers int, now time.Time) (bool, Reason) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.state(channel)
	if viewers < 0 {
		viewers = 0
	}
	boost := d.opts.RaidBoost * math.Sqrt(float64(viewers))
	if boost > 0 {
		s.events = append(s.events, weightedEvent{at: now, weight: boost, real: false})
	}
	weight, _ := s.prune(now, d.opts.Window)
	if viewers <= 0 {
		return false, ""
	}
	if !d.canFire(s, now) {
		return false, ""
	}
	d.fire(s, now, weight)
	return true, ReasonRaid
}

// canFire reports whether the cooldown since the last fire has elapsed.
func (d *Detector) canFire(s *channelState, now time.Time) bool {
	return s.lastFire.IsZero() || now.Sub(s.lastFire) >= d.opts.Cooldown
}

// fire records a fire at now and nudges the baseline up toward the firing
// weight so an immediate identical burst does not retrigger after the cooldown.
func (d *Detector) fire(s *channelState, now time.Time, weight float64) {
	s.lastFire = now
	if weight > s.baseline {
		s.baseline = 0.5 * (s.baseline + weight)
		s.baseInit = true
		s.lastTick = now
	}
}

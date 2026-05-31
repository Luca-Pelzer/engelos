package clipper

import (
	"math"
	"strings"
	"sync"
	"time"
)

// Reason labels why a clip moment fired.
type Reason string

const (
	// ReasonChatSpike fires when chat activity in the window jumps well above
	// the adaptive baseline.
	ReasonChatSpike Reason = "chat-spike"
	// ReasonKeyword fires when enough distinct viewers ask for a clip (typing
	// "clip", "clip it", ...) in the keyword window.
	ReasonKeyword Reason = "keyword"
	// ReasonEmote fires when enough distinct viewers spam the same hype emote.
	ReasonEmote Reason = "emote-burst"
	// ReasonCopypasta fires when enough distinct viewers paste the same phrase.
	ReasonCopypasta Reason = "copypasta"
	// ReasonComposite fires when several weaker signals together cross the
	// combined-score threshold even though no single signal tripped its own.
	ReasonComposite Reason = "composite"
	// ReasonSubBurst fires when several subscriptions land close together.
	ReasonSubBurst Reason = "sub-burst"
	// ReasonRaid fires when the channel is raided.
	ReasonRaid Reason = "raid"
)

// Options tunes the hype detector. The zero value is not usable; build it with
// [DefaultOptions] and adjust as needed. New only overrides positive fields, so
// a partially-filled Options keeps the remaining defaults.
type Options struct {
	// Window is the rolling window over which message rate is measured.
	Window time.Duration
	// BaselineHalfLife is the half-life of the EWMA that tracks the channel's
	// normal per-window rate, so a sustained high rate becomes the new normal.
	BaselineHalfLife time.Duration
	// SpikeFactor is the multiple of the baseline the current windowed rate
	// must reach to count as a chat spike.
	SpikeFactor float64
	// MinMessages is the minimum real messages in the window before any
	// chat-spike or composite fire, so quiet channels do not trigger on noise.
	MinMessages int
	// Cooldown is the minimum spacing between fires for a single channel.
	Cooldown time.Duration
	// SubBoost is the virtual-message weight a single sub adds to the window.
	SubBoost float64
	// RaidBoost scales the virtual weight a raid injects: RaidBoost*sqrt(viewers).
	RaidBoost float64

	// SignalWindow is the (usually shorter) window over which the unique-user
	// keyword, emote and copypasta signals are counted.
	SignalWindow time.Duration
	// KeywordThreshold is the number of DISTINCT viewers who must type a clip
	// keyword within SignalWindow to fire ReasonKeyword.
	KeywordThreshold int
	// EmoteThreshold is the number of DISTINCT viewers who must post the SAME
	// hype emote within SignalWindow to fire ReasonEmote.
	EmoteThreshold int
	// CopypastaThreshold is the number of DISTINCT viewers who must post the
	// same multi-word phrase within SignalWindow to fire ReasonCopypasta.
	CopypastaThreshold int

	// CompositeThreshold is the combined 0..1 score above which ReasonComposite
	// fires when no single hard trigger tripped.
	CompositeThreshold float64
	// Weight* are the composite-score weights for each normalised signal. They
	// need not sum to 1; the score is their weighted sum.
	WeightRate      float64
	WeightKeyword   float64
	WeightEmote     float64
	WeightCopypasta float64

	// Keywords are the clip-request triggers (lowercased). Multi-word entries
	// such as "clip it" are matched as substrings; single words as whole tokens.
	Keywords []string
	// Emotes are the hype emotes to count (matched case-insensitively as whole
	// tokens). Stored lowercased internally.
	Emotes []string
	// Excludes are anticipation/irony markers (lowercased). A message that
	// contains any of them is ignored by the keyword/emote/copypasta signals
	// (it still counts toward the message rate). This kills the most common
	// false positives, where chat spams "incoming"/"?" BEFORE something happens.
	Excludes []string
}

// DefaultOptions returns production defaults informed by real auto-clip tools:
// unique-user keyword/emote thresholds of 5-6 in an 8s window, an anticipation
// exclude-list, and a composite score combining rate with the content signals.
func DefaultOptions() Options {
	return Options{
		Window:           10 * time.Second,
		BaselineHalfLife: 2 * time.Minute,
		SpikeFactor:      3.0,
		MinMessages:      8,
		Cooldown:         90 * time.Second,
		SubBoost:         5,
		RaidBoost:        10,

		SignalWindow:       8 * time.Second,
		KeywordThreshold:   5,
		EmoteThreshold:     6,
		CopypastaThreshold: 5,

		CompositeThreshold: 0.65,
		WeightRate:         0.30,
		WeightKeyword:      0.35,
		WeightEmote:        0.25,
		WeightCopypasta:    0.10,

		Keywords: []string{"clip", "clip it", "clipit", "clipped", "clippen", "clip that"},
		Emotes: []string{
			"pog", "poggers", "pogchamp", "pogu", "kekw", "lul", "lulw",
			"omegalul", "kreygasm", "pepelaugh", "ezclap", "clap", "sheesh",
			"pepega", "monkahmm", "catjam", "ratjam",
		},
		Excludes: []string{
			"incoming", "inc", "soon", "?", "prayge", "pausechamp",
			"pepepains", "monkas", "copium", "anticipation",
		},
	}
}

// weightedEvent is one rate event retained in the window with its weight (1 for
// a real chat message, larger for the virtual weight of subs/raids) and whether
// it counts toward the MinMessages real-message floor.
type weightedEvent struct {
	at     time.Time
	weight float64
	real   bool
}

// userStamp tracks the most recent time a given user contributed to a signal,
// so each signal counts DISTINCT users within its window.
type channelState struct {
	events   []weightedEvent
	baseline float64
	baseInit bool
	lastTick time.Time
	lastSub  time.Time
	subCount int
	lastFire time.Time

	// clipUsers maps user -> last time they typed a clip keyword.
	clipUsers map[string]time.Time
	// emoteUsers maps emote -> (user -> last time they posted it).
	emoteUsers map[string]map[string]time.Time
	// phraseUsers maps a normalised multi-word phrase -> (user -> last time).
	phraseUsers map[string]map[string]time.Time
}

// Detector tracks per-channel chat/sub/raid activity and decides when a
// clip-worthy moment happens. It is safe for concurrent use.
//
// It blends several signals used by real auto-clip tools: an adaptive EWMA
// rate baseline (a spike is current-rate >= SpikeFactor*baseline), and
// unique-user counts for clip keywords, hype-emote bursts and copypasta. Each
// content signal can fire on its own (high precision), and a weighted
// composite of all normalised signals can fire when several are individually
// short of their threshold (high recall). An exclude-list removes anticipation
// spam from the content signals. A per-channel cooldown rate-limits fires, and
// a sustained high rate raises the baseline so a permanently busy chat stops
// spiking.
//
// All methods take an explicit now; the detector never reads the clock itself,
// so its behaviour is fully reproducible in tests.
type Detector struct {
	opts     Options
	keywords []string
	emotes   map[string]struct{}
	excludes []string
	mu       sync.Mutex
	byCh     map[string]*channelState
}

// New builds a Detector with opts. Non-positive numeric fields and empty slices
// fall back to the corresponding [DefaultOptions] value, so a partially-filled
// Options is safe.
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
	if opts.SignalWindow > 0 {
		d.SignalWindow = opts.SignalWindow
	}
	if opts.KeywordThreshold > 0 {
		d.KeywordThreshold = opts.KeywordThreshold
	}
	if opts.EmoteThreshold > 0 {
		d.EmoteThreshold = opts.EmoteThreshold
	}
	if opts.CopypastaThreshold > 0 {
		d.CopypastaThreshold = opts.CopypastaThreshold
	}
	if opts.CompositeThreshold > 0 {
		d.CompositeThreshold = opts.CompositeThreshold
	}
	if opts.WeightRate > 0 {
		d.WeightRate = opts.WeightRate
	}
	if opts.WeightKeyword > 0 {
		d.WeightKeyword = opts.WeightKeyword
	}
	if opts.WeightEmote > 0 {
		d.WeightEmote = opts.WeightEmote
	}
	if opts.WeightCopypasta > 0 {
		d.WeightCopypasta = opts.WeightCopypasta
	}
	if len(opts.Keywords) > 0 {
		d.Keywords = opts.Keywords
	}
	if len(opts.Emotes) > 0 {
		d.Emotes = opts.Emotes
	}
	if len(opts.Excludes) > 0 {
		d.Excludes = opts.Excludes
	}

	det := &Detector{
		opts:     d,
		keywords: lowerAll(d.Keywords),
		emotes:   toSet(lowerAll(d.Emotes)),
		excludes: lowerAll(d.Excludes),
		byCh:     make(map[string]*channelState),
	}
	return det
}

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

func toSet(in []string) map[string]struct{} {
	m := make(map[string]struct{}, len(in))
	for _, s := range in {
		if s != "" {
			m[s] = struct{}{}
		}
	}
	return m
}

func (d *Detector) state(channel string) *channelState {
	s := d.byCh[channel]
	if s == nil {
		s = &channelState{
			clipUsers:   make(map[string]time.Time),
			emoteUsers:  make(map[string]map[string]time.Time),
			phraseUsers: make(map[string]map[string]time.Time),
		}
		d.byCh[channel] = s
	}
	return s
}

// prune drops rate events older than the window and returns the surviving
// weighted sum and real-message count.
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

// pruneSignals drops user stamps older than the signal window from every
// content-signal map, keeping the unique-user counts bounded and current.
func (s *channelState) pruneSignals(now time.Time, window time.Duration) {
	cutoff := now.Add(-window)
	for u, t := range s.clipUsers {
		if !t.After(cutoff) {
			delete(s.clipUsers, u)
		}
	}
	for emote, users := range s.emoteUsers {
		for u, t := range users {
			if !t.After(cutoff) {
				delete(users, u)
			}
		}
		if len(users) == 0 {
			delete(s.emoteUsers, emote)
		}
	}
	for phrase, users := range s.phraseUsers {
		for u, t := range users {
			if !t.After(cutoff) {
				delete(users, u)
			}
		}
		if len(users) == 0 {
			delete(s.phraseUsers, phrase)
		}
	}
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

// Message records a chat message from userID with the given text at now and
// reports whether it triggers a clip moment. It updates the rate baseline and
// the unique-user content signals, then evaluates the hard triggers and the
// composite score (respecting the cooldown).
func (d *Detector) Message(channel, userID, text string, now time.Time) (bool, Reason) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.state(channel)

	s.events = append(s.events, weightedEvent{at: now, weight: 1, real: true})
	weight, real := s.prune(now, d.opts.Window)

	baseline := s.baseline
	s.updateBaseline(now, weight, d.opts.BaselineHalfLife.Seconds())

	d.recordSignals(s, userID, text, now)
	s.pruneSignals(now, d.opts.SignalWindow)

	return d.evaluate(s, now, weight, real, baseline)
}

// recordSignals folds one message into the keyword/emote/copypasta unique-user
// maps, unless the message contains an excluded anticipation marker.
func (d *Detector) recordSignals(s *channelState, userID, text string, now time.Time) {
	if userID == "" {
		return
	}
	lower := strings.ToLower(text)
	if d.isExcluded(lower) {
		return
	}

	if d.matchesKeyword(lower) {
		s.clipUsers[userID] = now
	}

	tokens := strings.Fields(lower)
	seenEmote := make(map[string]bool, len(tokens))
	for _, tok := range tokens {
		tok = strings.Trim(tok, ".,!?;:'\"")
		if tok == "" {
			continue
		}
		if _, ok := d.emotes[tok]; ok && !seenEmote[tok] {
			seenEmote[tok] = true
			users := s.emoteUsers[tok]
			if users == nil {
				users = make(map[string]time.Time)
				s.emoteUsers[tok] = users
			}
			users[userID] = now
		}
	}

	// Copypasta is a repeated MULTI-word phrase (single tokens are covered by
	// the emote signal or are too generic to be a meaningful copypasta).
	if len(tokens) >= 2 {
		phrase := strings.Join(tokens, " ")
		users := s.phraseUsers[phrase]
		if users == nil {
			users = make(map[string]time.Time)
			s.phraseUsers[phrase] = users
		}
		users[userID] = now
	}
}

// isExcluded reports whether the lowercased text contains any anticipation
// marker, in which case it is ignored by the content signals.
func (d *Detector) isExcluded(lower string) bool {
	for _, ex := range d.excludes {
		if ex == "" {
			continue
		}
		if ex == "?" {
			if strings.Contains(lower, "?") {
				return true
			}
			continue
		}
		if containsToken(lower, ex) {
			return true
		}
	}
	return false
}

// matchesKeyword reports whether the lowercased text contains a clip keyword:
// multi-word keywords match as substrings, single words as whole tokens.
func (d *Detector) matchesKeyword(lower string) bool {
	for _, kw := range d.keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(kw, " ") {
			if strings.Contains(lower, kw) {
				return true
			}
			continue
		}
		if containsToken(lower, kw) {
			return true
		}
	}
	return false
}

// containsToken reports whether word appears as a whole whitespace-separated
// token in lower (ignoring surrounding punctuation), so "clip" matches "clip!"
// but not "clipboard".
func containsToken(lower, word string) bool {
	for _, tok := range strings.Fields(lower) {
		if strings.Trim(tok, ".,!?;:'\"") == word {
			return true
		}
	}
	return false
}

// maxUsers returns the largest unique-user count across the buckets of a
// emote/phrase map.
func maxUsers(m map[string]map[string]time.Time) int {
	max := 0
	for _, users := range m {
		if len(users) > max {
			max = len(users)
		}
	}
	return max
}

// evaluate applies the hard triggers (in precision order) and the composite
// score, returning the first reason that fires under the cooldown.
func (d *Detector) evaluate(s *channelState, now time.Time, weight float64, real int, baseline float64) (bool, Reason) {
	clipUsers := len(s.clipUsers)
	emoteUsers := maxUsers(s.emoteUsers)
	phraseUsers := maxUsers(s.phraseUsers)

	// Hard, high-precision triggers first. These can fire even in a quiet chat
	// because a coordinated burst of distinct viewers is itself the signal.
	if clipUsers >= d.opts.KeywordThreshold && d.canFire(s, now) {
		d.fire(s, now, weight)
		return true, ReasonKeyword
	}
	if emoteUsers >= d.opts.EmoteThreshold && d.canFire(s, now) {
		d.fire(s, now, weight)
		return true, ReasonEmote
	}
	if phraseUsers >= d.opts.CopypastaThreshold && d.canFire(s, now) {
		d.fire(s, now, weight)
		return true, ReasonCopypasta
	}

	// Rate spike: the classic signal, kept intact.
	rateSpike := s.baseInit && baseline > 0 && real >= d.opts.MinMessages &&
		weight >= d.opts.SpikeFactor*baseline
	if rateSpike && d.canFire(s, now) {
		d.fire(s, now, weight)
		return true, ReasonChatSpike
	}

	// Composite: combine the normalised signals so several weak ones together
	// can still fire. Requires the message floor so quiet chats stay silent.
	if real < d.opts.MinMessages {
		return false, ""
	}
	score := d.composite(weight, baseline, clipUsers, emoteUsers, phraseUsers, s.baseInit)
	if score >= d.opts.CompositeThreshold && d.canFire(s, now) {
		d.fire(s, now, weight)
		return true, ReasonComposite
	}
	return false, ""
}

// composite returns the weighted 0..1+ blend of the normalised signals.
func (d *Detector) composite(weight, baseline float64, clipUsers, emoteUsers, phraseUsers int, baseInit bool) float64 {
	rateScore := 0.0
	if baseInit && baseline > 0 {
		rateScore = clamp01(weight / (d.opts.SpikeFactor * baseline))
	}
	kwScore := clamp01(ratio(clipUsers, d.opts.KeywordThreshold))
	emScore := clamp01(ratio(emoteUsers, d.opts.EmoteThreshold))
	cpScore := clamp01(ratio(phraseUsers, d.opts.CopypastaThreshold))
	return d.opts.WeightRate*rateScore +
		d.opts.WeightKeyword*kwScore +
		d.opts.WeightEmote*emScore +
		d.opts.WeightCopypasta*cpScore
}

func ratio(n, denom int) float64 {
	if denom <= 0 {
		return 0
	}
	return float64(n) / float64(denom)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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

// fire records a fire at now, nudges the baseline up toward the firing weight
// so an immediate identical burst does not retrigger after the cooldown, and
// clears the content-signal maps so the same burst is not recounted.
func (d *Detector) fire(s *channelState, now time.Time, weight float64) {
	s.lastFire = now
	if weight > s.baseline {
		s.baseline = 0.5 * (s.baseline + weight)
		s.baseInit = true
		s.lastTick = now
	}
	s.clipUsers = make(map[string]time.Time)
	s.emoteUsers = make(map[string]map[string]time.Time)
	s.phraseUsers = make(map[string]map[string]time.Time)
}

package clipper

import "time"

// Settings is the per-channel, user-tunable subset of [Options]. It is what the
// dashboard edits and the settings store persists, deliberately kept small so a
// streamer only touches the knobs that matter in practice (mostly the
// unique-user thresholds, which must be lowered for a small channel where five
// distinct viewers may never coincide in one window).
//
// Every numeric field uses "zero means inherit": a non-positive value keeps the
// base/default for that field, so a channel overrides only what it explicitly
// sets. Enabled is an explicit bool because a channel row is only written once
// the streamer opts in, and an absent row is handled by the caller's allow-list.
type Settings struct {
	// Enabled turns auto-clipping on or off for this channel.
	Enabled bool
	// KeywordThreshold is the distinct-viewer count for the clip-keyword
	// trigger. Lower it (for example to 3) on a small channel.
	KeywordThreshold int
	// EmoteThreshold is the distinct-viewer count for the hype-emote burst.
	EmoteThreshold int
	// CopypastaThreshold is the distinct-viewer count for the copypasta trigger.
	CopypastaThreshold int
	// MinMessages is the windowed real-message floor below which the rate and
	// composite signals stay silent.
	MinMessages int
	// SpikeFactor is the multiple of the adaptive baseline that counts as a
	// chat spike.
	SpikeFactor float64
	// CompositeThreshold is the combined 0..1 score above which the composite
	// signal fires when no single hard trigger tripped.
	CompositeThreshold float64
	// CooldownSeconds is the minimum spacing between fires for this channel, in
	// seconds (the store and dashboard speak whole seconds, not Durations).
	CooldownSeconds int
}

// DefaultSettings returns the per-channel settings that reproduce
// [DefaultOptions] verbatim, so a freshly enabled channel behaves exactly like
// the global default until the streamer tunes it.
func DefaultSettings() Settings {
	d := DefaultOptions()
	return Settings{
		Enabled:            true,
		KeywordThreshold:   d.KeywordThreshold,
		EmoteThreshold:     d.EmoteThreshold,
		CopypastaThreshold: d.CopypastaThreshold,
		MinMessages:        d.MinMessages,
		SpikeFactor:        d.SpikeFactor,
		CompositeThreshold: d.CompositeThreshold,
		CooldownSeconds:    int(d.Cooldown / time.Second),
	}
}

// ApplyTo returns a copy of base with this Settings' positive overrides applied.
// Non-positive fields leave the corresponding base value untouched, matching the
// "zero means inherit" merge that [New] already performs against the defaults,
// so layering channel settings over an env-tuned base composes predictably.
func (s Settings) ApplyTo(base Options) Options {
	out := base
	if s.KeywordThreshold > 0 {
		out.KeywordThreshold = s.KeywordThreshold
	}
	if s.EmoteThreshold > 0 {
		out.EmoteThreshold = s.EmoteThreshold
	}
	if s.CopypastaThreshold > 0 {
		out.CopypastaThreshold = s.CopypastaThreshold
	}
	if s.MinMessages > 0 {
		out.MinMessages = s.MinMessages
	}
	if s.SpikeFactor > 0 {
		out.SpikeFactor = s.SpikeFactor
	}
	if s.CompositeThreshold > 0 {
		out.CompositeThreshold = s.CompositeThreshold
	}
	if s.CooldownSeconds > 0 {
		out.Cooldown = time.Duration(s.CooldownSeconds) * time.Second
	}
	return out
}

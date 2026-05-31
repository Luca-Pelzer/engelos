// Package clipper is the decision engine for the AI Auto-Clipper: it watches a
// channel's chat, subscription and raid activity and decides WHEN a clip-worthy
// moment happens. It does not talk to Twitch or create clips; a separate
// adapter feeds it events and acts on a fire by calling the Clips API.
//
// # Signals
//
// The detector blends several signals used by real auto-clip tools rather than
// relying on raw message rate alone:
//
//   - Rate spike: chat activity is a weighted count over a rolling window, and
//     an exponentially weighted moving average tracks the channel's normal
//     rate. A spike is current-rate >= SpikeFactor*baseline. Because the
//     baseline tracks sustained activity, a permanently busy chat raises its
//     own bar and stops firing, while a sudden jump triggers a clip.
//   - Keyword: enough DISTINCT viewers typing a clip request ("clip", "clip
//     it", ...) within the signal window. Counting distinct users stops one
//     person spamming from triggering it.
//   - Emote burst: enough distinct viewers posting the SAME hype emote.
//   - Copypasta: enough distinct viewers pasting the same multi-word phrase.
//   - Composite: a weighted blend of the normalised signals, so several weak
//     signals together can fire even when none crosses its own threshold.
//
// An exclude-list removes anticipation/irony markers ("incoming", "?",
// "PauseChamp", ...) from the content signals, which are the most common
// false-positive source because chat spams them BEFORE something happens.
//
// Subscriptions and raids contribute virtual weight (so a post-raid chat surge
// is contextualised) and can fire directly: several subs close together fire a
// sub-burst, and any raid fires immediately. A per-channel cooldown rate-limits
// all fires.
//
// # Determinism
//
// Every method takes an explicit now; the detector never reads the clock, so
// its behaviour is fully reproducible in tests. All state is guarded by a mutex
// and the package imports nothing under engelos/internal.
package clipper

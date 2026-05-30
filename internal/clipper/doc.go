// Package clipper is the decision engine for the AI Auto-Clipper: it watches a
// channel's chat, subscription and raid activity and decides WHEN a clip-worthy
// moment happens. It does not talk to Twitch or create clips; a separate
// adapter feeds it events and acts on a fire by calling the Clips API.
//
// # Adaptive baseline
//
// Chat activity is measured as a weighted count over a rolling window. The
// detector keeps an exponentially weighted moving average of that windowed
// activity as the channel's "normal" rate, decaying with a configurable
// half-life. A chat spike fires only when the current windowed weight exceeds
// SpikeFactor times the baseline, a real-message floor is met, and a per-channel
// cooldown has elapsed. Because the baseline tracks sustained activity, a
// permanently busy chat raises its own bar and stops firing, while a sudden
// jump above the recent norm triggers a clip.
//
// Subscriptions and raids contribute virtual weight (so a post-raid chat surge
// is contextualised) and can fire directly: several subs close together fire a
// sub-burst, and any raid fires immediately.
//
// # Determinism
//
// Every method takes an explicit now; the detector never reads the clock, so
// its behaviour is fully reproducible in tests. All state is guarded by a mutex
// and the package imports nothing under engelos/internal.
package clipper

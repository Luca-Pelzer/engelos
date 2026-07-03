package automod

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// shadowSampleConfig is a realistic "shadow pass" starter configuration: the
// cheap, always-on deterministic rules an operator would turn on first (links,
// caps, symbol spam, and a couple of obvious scam phrases). It exists so the
// fixture below documents what the fast path is expected to catch on its own,
// before any AI escalation. Mode is pinned to ModeActive only to make intent
// explicit; verdicts are identical in dry-run (the engine evaluates in both,
// only the caller chooses whether to enforce).
func shadowSampleConfig() Config {
	cfg := DefaultConfig()
	cfg.Mode = ModeActive

	cfg.Links.Enabled = true
	cfg.Links.TimeoutSecs = 600
	cfg.Links.AllowList = []string{"twitch.tv", "*.twitch.tv"}

	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 30

	cfg.Symbols.Enabled = true
	cfg.Symbols.TimeoutSecs = 30

	cfg.BannedWords.Enabled = true
	cfg.BannedWords.TimeoutSecs = 600
	cfg.BannedWords.Entries = []BannedEntry{
		{Phrase: "free followers", MatchMode: MatchAnywhere, Verdict: VerdictTimeout},
		{Phrase: "cheap viewers", MatchMode: MatchAnywhere, Verdict: VerdictTimeout},
	}
	return cfg
}

// TestObviousAbuseSamples is a small fixture/regression for the deterministic
// fast path: the obvious spam/scam/link/caps cases an operator expects caught
// on day one must fire, and plainly-fine chatter must pass untouched. Each
// abuse sample is crafted to trip exactly one filter so the firing filter is
// asserted too. It is intentionally a handful of representative samples, not an
// exhaustive simulator.
func TestObviousAbuseSamples(t *testing.T) {
	e := mustEngine(t, shadowSampleConfig())

	abuse := []struct {
		name       string
		text       string
		wantFilter string
	}{
		{"scam promo link", "grab cheap deals at sketchy-site.com today", "links"},
		{"follow-bot phrase", "get free followers instantly just dm me", "banned_words"},
		{"all-caps shouting", "SUBSCRIBE TO MY CHANNEL RIGHT NOW PLEASE", "caps"},
		{"symbol spam run", "giveaway %%%%%%%%%%%% winners", "symbols"},
		{"ip-address link", "join my server at 192.168.0.1 right now", "links"},
	}
	for _, tc := range abuse {
		t.Run("catches/"+tc.name, func(t *testing.T) {
			got := e.Evaluate(Message{Text: tc.text}, everyone())
			assert.NotEqualf(t, VerdictPass, got.Verdict,
				"fast path should catch %q (reason=%q)", tc.text, got.Reason)
			assert.Equalf(t, tc.wantFilter, got.FilterName,
				"unexpected firing filter for %q (reason=%q)", tc.text, got.Reason)
		})
	}

	// Plainly-fine chatter, including an allow-listed Twitch link, must pass.
	clean := []string{
		"hey everyone, great stream today!",
		"gg that was a clean clutch",
		"watch the vod later at twitch.tv/somebody",
		"lol that boss fight was rough",
	}
	for _, text := range clean {
		t.Run("passes/"+text, func(t *testing.T) {
			got := e.Evaluate(Message{Text: text}, everyone())
			assert.Equalf(t, VerdictPass, got.Verdict,
				"clean message wrongly flagged: %q (filter=%q reason=%q)", text, got.FilterName, got.Reason)
		})
	}
}

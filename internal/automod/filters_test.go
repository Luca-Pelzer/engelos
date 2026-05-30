package automod

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustEngine builds an engine and fails the test on a compile error.
func mustEngine(t *testing.T, cfg Config) *Engine {
	t.Helper()
	e, err := NewEngine(cfg)
	require.NoError(t, err)
	require.NotNil(t, e)
	return e
}

// everyone is the default non-privileged author.
func everyone() UserContext {
	return UserContext{Role: RoleEveryone, AccountAgeDays: -1, FollowAgeDays: -1}
}

// ---------------------------------------------------------------------------
// Caps
// ---------------------------------------------------------------------------

func TestCapsFilter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 30
	e := mustEngine(t, cfg)

	tests := []struct {
		name        string
		text        string
		wantVerdict Verdict
	}{
		{"short skipped", "AAAAA", VerdictPass},
		{"mixed below threshold", "Hello there friend how are you", VerdictPass},
		{"loud all caps", "STOP YELLING AT EVERYONE PLEASE", VerdictTimeout},
		{"url stripped keeps it clean", "check https://EXAMPLE.COM/PATH lower text here", VerdictPass},
		{"no alpha numbers only", "1234567890 1234567890", VerdictPass},
		{"exactly at threshold passes", "AAAAAaaaaaXXXXXxxxxx", VerdictPass}, // 50% == not > 0.60
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := e.Evaluate(Message{Text: tc.text}, everyone())
			assert.Equal(t, tc.wantVerdict, got.Verdict, "text=%q reason=%q", tc.text, got.Reason)
			if tc.wantVerdict == VerdictTimeout {
				assert.Equal(t, "caps", got.FilterName)
				assert.Equal(t, 30*time.Second, got.Timeout)
				assert.Contains(t, got.Reason, "caps")
			}
		})
	}
}

func TestCapsDeleteWhenZeroTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 0
	e := mustEngine(t, cfg)

	got := e.Evaluate(Message{Text: "STOP YELLING AT EVERYONE PLEASE"}, everyone())
	assert.Equal(t, VerdictDelete, got.Verdict)
	assert.Equal(t, time.Duration(0), got.Timeout)
}

// ---------------------------------------------------------------------------
// Symbols
// ---------------------------------------------------------------------------

func TestSymbolsFilter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Symbols.Enabled = true
	cfg.Symbols.TimeoutSecs = 10
	e := mustEngine(t, cfg)

	tests := []struct {
		name        string
		text        string
		wantVerdict Verdict
		reasonHas   string
	}{
		{"clean text", "this is a totally normal sentence", VerdictPass, ""},
		{"grouped run", "wow !!!!!!!!! that is a lot", VerdictTimeout, "run"},
		{"percentage spam", "#@#@#@#@#@#@#@#@#@#@", VerdictTimeout, ""},
		{"few symbols ok", "hey there, how's it going? good!", VerdictPass, ""},
		{"zalgo abuse", "h\u0300\u0301\u0302e\u0303\u0304\u0305llo there everyone", VerdictTimeout, "zalgo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := e.Evaluate(Message{Text: tc.text}, everyone())
			assert.Equal(t, tc.wantVerdict, got.Verdict, "reason=%q", got.Reason)
			if tc.reasonHas != "" {
				assert.Contains(t, got.Reason, tc.reasonHas)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Links
// ---------------------------------------------------------------------------

func TestLinksFilter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Links.Enabled = true
	cfg.Links.TimeoutSecs = 60
	cfg.Links.AllowList = []string{"twitch.tv", "*.twitch.tv", "clips.twitch.tv/*"}
	e := mustEngine(t, cfg)

	tests := []struct {
		name        string
		text        string
		wantVerdict Verdict
	}{
		{"no link", "just a normal message about cats", VerdictPass},
		{"allowed exact", "watch me at https://twitch.tv/someone", VerdictPass},
		{"allowed subdomain", "go to clips.twitch.tv/abc now", VerdictPass},
		{"allowed wildcard sub", "see player.twitch.tv/foo", VerdictPass},
		{"blocked domain", "buy now at sketchy-site.com/deal", VerdictTimeout},
		{"blocked www", "visit www.evil.net for free stuff", VerdictTimeout},
		{"ip address", "connect to 192.168.0.1 right now", VerdictTimeout},
		{"dot variant", "go to evil dot com to win", VerdictTimeout},
		{"bare domain", "everyone use discord.gg/xyz today", VerdictTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := e.Evaluate(Message{Text: tc.text}, everyone())
			assert.Equal(t, tc.wantVerdict, got.Verdict, "text=%q match=%q", tc.text, got.MatchedText)
			if tc.wantVerdict != VerdictPass {
				assert.Equal(t, "links", got.FilterName)
				assert.NotEmpty(t, got.MatchedText)
			}
		})
	}
}

func TestLinksIPCanBeDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Links.Enabled = true
	cfg.Links.TimeoutSecs = 60
	cfg.Links.BlockIPAddresses = false
	cfg.Links.BlockDotVariants = false
	e := mustEngine(t, cfg)

	assert.Equal(t, VerdictPass, e.Evaluate(Message{Text: "ping 10.0.0.1 please"}, everyone()).Verdict)
	assert.Equal(t, VerdictPass, e.Evaluate(Message{Text: "go evil dot com"}, everyone()).Verdict)
}

func TestHostAllowed(t *testing.T) {
	tests := []struct {
		host  string
		allow []string
		want  bool
	}{
		{"twitch.tv", []string{"twitch.tv"}, true},
		{"clips.twitch.tv", []string{"*.twitch.tv"}, true},
		{"twitch.tv", []string{"*.twitch.tv"}, true},
		{"evil.com", []string{"*.twitch.tv"}, false},
		{"example.com", []string{"example.*"}, true},
		{"example.org", []string{"example.*"}, true},
		{"sub.example.com", []string{"example.*"}, false}, // prefix.* is TLD-wildcard, not subdomain
		{"clips.twitch.tv", []string{"clips.twitch.tv/*"}, true},
		{"anything.org", []string{"*"}, true},
		{"NoCase.TV", []string{"nocase.tv"}, true},
		{"empty", nil, false},
	}
	for _, tc := range tests {
		assert.Equalf(t, tc.want, hostAllowed(tc.host, tc.allow),
			"host=%q allow=%v", tc.host, tc.allow)
	}
}

// ---------------------------------------------------------------------------
// Emotes
// ---------------------------------------------------------------------------

func TestEmotesFilter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Emotes.Enabled = true
	cfg.Emotes.TimeoutSecs = 15
	cfg.Emotes.MaxEmotes = 5
	e := mustEngine(t, cfg)

	assert.Equal(t, VerdictPass, e.Evaluate(Message{Text: "hi", EmoteCount: 5}, everyone()).Verdict)
	got := e.Evaluate(Message{Text: "spam", EmoteCount: 6}, everyone())
	assert.Equal(t, VerdictTimeout, got.Verdict)
	assert.Equal(t, "emotes", got.FilterName)
	assert.Equal(t, 15*time.Second, got.Timeout)
}

// ---------------------------------------------------------------------------
// Length
// ---------------------------------------------------------------------------

func TestLengthFilter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Length.Enabled = true
	cfg.Length.TimeoutSecs = 5
	cfg.Length.MaxChars = 10
	e := mustEngine(t, cfg)

	assert.Equal(t, VerdictPass, e.Evaluate(Message{Text: "0123456789"}, everyone()).Verdict)
	assert.Equal(t, VerdictTimeout, e.Evaluate(Message{Text: "01234567890"}, everyone()).Verdict)
	// multibyte runes are counted as runes, not bytes.
	assert.Equal(t, VerdictPass, e.Evaluate(Message{Text: "äöüäöüäöüä"}, everyone()).Verdict)
}

// ---------------------------------------------------------------------------
// Repetition
// ---------------------------------------------------------------------------

func TestRepetitionFilter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Repetition.Enabled = true
	cfg.Repetition.TimeoutSecs = 20
	e := mustEngine(t, cfg)

	tests := []struct {
		name        string
		text        string
		wantVerdict Verdict
	}{
		{"too short", "LUL LUL LUL", VerdictPass},
		{"few tokens", "go go go", VerdictPass},
		{"normal sentence", "the quick brown fox jumps over the lazy dog today", VerdictPass},
		{"heavy repeat", "LUL LUL LUL LUL LUL LUL LUL LUL", VerdictTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := e.Evaluate(Message{Text: tc.text}, everyone())
			assert.Equal(t, tc.wantVerdict, got.Verdict, "reason=%q", got.Reason)
		})
	}
}

// ---------------------------------------------------------------------------
// Banned words — every MatchMode
// ---------------------------------------------------------------------------

func TestBannedWordsMatchModes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.TimeoutSecs = 600
	cfg.BannedWords.Entries = []BannedEntry{
		{Phrase: "badword", MatchMode: MatchAnywhere, Verdict: VerdictTimeout},
		{Phrase: "scam", MatchMode: MatchWord, Verdict: VerdictTimeout},
		{Phrase: "exactly this", MatchMode: MatchExact, Verdict: VerdictBan},
		{Phrase: "free*gift", MatchMode: MatchWildcard, Verdict: VerdictTimeout},
		{Phrase: `\d{4,}`, MatchMode: MatchRegex, Verdict: VerdictDelete},
		{Phrase: "CaseSENS", MatchMode: MatchAnywhere, CaseSensitive: true, Verdict: VerdictTimeout},
	}
	e := mustEngine(t, cfg)

	tests := []struct {
		name        string
		text        string
		wantVerdict Verdict
		wantMatch   string
	}{
		{"anywhere substring", "you are a badwordish person", VerdictTimeout, "badword"},
		{"anywhere case insensitive", "BADWORD here", VerdictTimeout, "badword"},
		{"word boundary hit", "this is a scam alert", VerdictTimeout, "scam"},
		{"word boundary miss", "scampi is delicious food yum", VerdictPass, ""},
		{"exact hit", "exactly this", VerdictBan, "exactly this"},
		{"exact miss with extra", "well exactly this thing", VerdictPass, ""},
		{"wildcard hit", "claim your free xmas gift now", VerdictTimeout, ""},
		{"regex hit digits", "code 12345 now", VerdictDelete, ""},
		{"regex miss few digits", "code 12 now", VerdictPass, ""},
		{"case sensitive hit", "this is CaseSENS yo", VerdictTimeout, "CaseSENS"},
		{"case sensitive miss", "this is casesens yo", VerdictPass, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := e.Evaluate(Message{Text: tc.text}, everyone())
			assert.Equal(t, tc.wantVerdict, got.Verdict, "reason=%q match=%q", got.Reason, got.MatchedText)
			if tc.wantVerdict != VerdictPass {
				assert.Equal(t, "banned_words", got.FilterName)
				if tc.wantMatch != "" {
					assert.Equal(t, tc.wantMatch, got.MatchedText)
				}
			}
		})
	}
}

func TestBannedWordsBadRegexErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.Entries = []BannedEntry{
		{Phrase: "(unclosed", MatchMode: MatchRegex, Verdict: VerdictTimeout},
	}
	e, err := NewEngine(cfg)
	require.Error(t, err)
	assert.Nil(t, e)
	assert.Contains(t, err.Error(), "regex")
}

func TestBannedWordsBanTimeoutIsZero(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.TimeoutSecs = 600
	cfg.BannedWords.Entries = []BannedEntry{
		{Phrase: "nuke", MatchMode: MatchAnywhere, Verdict: VerdictBan},
	}
	e := mustEngine(t, cfg)
	got := e.Evaluate(Message{Text: "nuke them all"}, everyone())
	assert.Equal(t, VerdictBan, got.Verdict)
	// Ban is not a timeout, so suggested timeout stays 0.
	assert.Equal(t, time.Duration(0), got.Timeout)
}

// ---------------------------------------------------------------------------
// Role exemptions
// ---------------------------------------------------------------------------

func TestRoleExemptions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 30
	cfg.Caps.ExemptMinRole = RoleSubscriber // subs+ bypass caps
	e := mustEngine(t, cfg)

	loud := Message{Text: "STOP YELLING AT EVERYONE PLEASE"}

	assert.Equal(t, VerdictTimeout, e.Evaluate(loud, UserContext{Role: RoleEveryone}).Verdict,
		"plain chatter should be filtered")
	assert.Equal(t, VerdictPass, e.Evaluate(loud, UserContext{Role: RoleSubscriber}).Verdict,
		"subscriber is exempt for this filter")
	assert.Equal(t, VerdictPass, e.Evaluate(loud, UserContext{Role: RoleVIP}).Verdict,
		"vip is above the exempt floor")
	assert.Equal(t, VerdictPass, e.Evaluate(loud, UserContext{Role: RoleModerator}).Verdict,
		"mod is globally exempt")
	assert.Equal(t, VerdictPass, e.Evaluate(loud, UserContext{Role: RoleBroadcaster}).Verdict,
		"broadcaster is globally exempt")
}

func TestModeratorAlwaysExemptEvenWithoutPerFilterExemption(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.Entries = []BannedEntry{
		{Phrase: "nuke", MatchMode: MatchAnywhere, Verdict: VerdictBan},
	}
	e := mustEngine(t, cfg)

	assert.Equal(t, VerdictBan, e.Evaluate(Message{Text: "nuke"}, UserContext{Role: RoleEveryone}).Verdict)
	assert.Equal(t, VerdictPass, e.Evaluate(Message{Text: "nuke"}, UserContext{Role: RoleModerator}).Verdict)
}

// ---------------------------------------------------------------------------
// Mode + highest-verdict selection
// ---------------------------------------------------------------------------

func TestModeOffReturnsPass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = ModeOff
	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 30
	e := mustEngine(t, cfg)
	assert.Equal(t, VerdictPass, e.Evaluate(Message{Text: "STOP YELLING AT EVERYONE PLEASE"}, everyone()).Verdict)
}

func TestModeDryRunStillEvaluates(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = ModeDryRun
	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 30
	e := mustEngine(t, cfg)
	// DryRun still computes a verdict; the caller chooses not to act on it.
	assert.Equal(t, VerdictTimeout, e.Evaluate(Message{Text: "STOP YELLING AT EVERYONE PLEASE"}, everyone()).Verdict)
}

func TestHighestVerdictWins(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 30 // -> VerdictTimeout
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.Entries = []BannedEntry{
		{Phrase: "nuke", MatchMode: MatchAnywhere, Verdict: VerdictBan},
	}
	e := mustEngine(t, cfg)

	// Message trips both caps (timeout) and banned (ban); ban is more severe.
	got := e.Evaluate(Message{Text: "NUKE EVERYONE RIGHT NOW PLEASE"}, everyone())
	assert.Equal(t, VerdictBan, got.Verdict)
	assert.Equal(t, "banned_words", got.FilterName)
}

func TestTieBrokenByStableOrder(t *testing.T) {
	cfg := DefaultConfig()
	// Both Links and Caps will produce VerdictTimeout; Links wins the tie.
	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 30
	cfg.Links.Enabled = true
	cfg.Links.TimeoutSecs = 60
	e := mustEngine(t, cfg)

	got := e.Evaluate(Message{Text: "VISIT SKETCHY-SITE.COM RIGHT NOW EVERYONE"}, everyone())
	assert.Equal(t, VerdictTimeout, got.Verdict)
	assert.Equal(t, "links", got.FilterName, "links precedes caps in stable order")
}

func TestAllPassReturnsPass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Caps.Enabled = true
	cfg.Symbols.Enabled = true
	cfg.Links.Enabled = true
	cfg.Length.Enabled = true
	cfg.Repetition.Enabled = true
	e := mustEngine(t, cfg)
	got := e.Evaluate(Message{Text: "just a perfectly normal friendly message"}, everyone())
	assert.Equal(t, VerdictPass, got.Verdict)
}

// ---------------------------------------------------------------------------
// Config + constructor + concurrency
// ---------------------------------------------------------------------------

func TestDefaultConfigDisabledButSane(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, ModeActive, cfg.Mode)
	assert.False(t, cfg.Caps.Enabled)
	assert.Equal(t, 15, cfg.Caps.MinLength)
	assert.InDelta(t, 0.60, cfg.Caps.MaxCapsPercent, 1e-9)
	assert.Equal(t, 8, cfg.Symbols.MaxGroupedSymbols)
	assert.True(t, cfg.Symbols.BlockZalgo)
	assert.True(t, cfg.Links.BlockIPAddresses)
	assert.True(t, cfg.Links.BlockDotVariants)
	assert.Equal(t, 12, cfg.Emotes.MaxEmotes)
	assert.Equal(t, 375, cfg.Length.MaxChars)
	assert.InDelta(t, 0.50, cfg.Repetition.MaxRepeatRatio, 1e-9)

	// With everything disabled, nothing fires.
	e := mustEngine(t, cfg)
	assert.Equal(t, VerdictPass, e.Evaluate(Message{Text: "WHATEVER LOUD TEXT"}, everyone()).Verdict)
}

func TestEngineConfigReturnsCopy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Caps.Enabled = true
	e := mustEngine(t, cfg)
	assert.Equal(t, cfg.Caps.Enabled, e.Config().Caps.Enabled)
}

func TestVerdictString(t *testing.T) {
	assert.Equal(t, "pass", VerdictPass.String())
	assert.Equal(t, "delete", VerdictDelete.String())
	assert.Equal(t, "timeout", VerdictTimeout.String())
	assert.Equal(t, "ban", VerdictBan.String())
	assert.Equal(t, "unknown", Verdict(99).String())
}

// TestConcurrentEvaluate exercises the read-only guarantee under the race
// detector.
func TestConcurrentEvaluate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Caps.Enabled = true
	cfg.Caps.TimeoutSecs = 30
	cfg.Links.Enabled = true
	cfg.Links.TimeoutSecs = 60
	cfg.BannedWords.Enabled = true
	cfg.BannedWords.Entries = []BannedEntry{
		{Phrase: "nuke", MatchMode: MatchAnywhere, Verdict: VerdictBan},
	}
	e := mustEngine(t, cfg)

	inputs := []Message{
		{Text: "STOP YELLING AT EVERYONE PLEASE"},
		{Text: "visit sketchy-site.com now everyone"},
		{Text: "nuke them all immediately"},
		{Text: "a perfectly fine and calm message"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got := e.Evaluate(inputs[i%len(inputs)], everyone())
			// Just touch the result; correctness asserted elsewhere.
			_ = got.Verdict.String()
		}(i)
	}
	wg.Wait()
}

// TestStripForTextAnalysis documents that URLs are removed and whitespace
// collapsed before caps/symbols counting.
func TestStripForTextAnalysis(t *testing.T) {
	e := mustEngine(t, DefaultConfig())
	out := e.stripForTextAnalysis("hello   https://example.com/x   world")
	assert.False(t, strings.Contains(out, "example.com"))
	assert.Equal(t, "hello world", out)
}

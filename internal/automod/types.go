package automod

import "time"

// Role mirrors the privilege ladder used by the commands engine. It is
// defined locally rather than imported so that the automod package stays
// fully decoupled (no import cycles, stdlib-only). The ordering matches the
// commands engine: higher numeric value implies a strictly higher privilege,
// and a higher role satisfies every lower-role gate.
type Role int

const (
	// RoleEveryone is the default, lowest tier (unauthenticated chatter).
	RoleEveryone Role = iota
	// RoleSubscriber is a channel subscriber.
	RoleSubscriber
	// RoleVIP is a channel VIP.
	RoleVIP
	// RoleModerator is a channel moderator. Moderators are ALWAYS globally
	// exempt from every filter.
	RoleModerator
	// RoleBroadcaster is the channel owner. Always globally exempt.
	RoleBroadcaster
)

// Verdict is the severity a filter assigns to a message. Higher values are
// more severe; the engine returns the single most-severe verdict across all
// filters that fired.
type Verdict int

const (
	// VerdictPass means the filter found nothing wrong.
	VerdictPass Verdict = iota
	// VerdictDelete removes the message without timing the user out.
	VerdictDelete
	// VerdictTimeout removes the message and times the user out.
	VerdictTimeout
	// VerdictBan is a permanent ban.
	VerdictBan
)

// String renders the verdict for logs and audit output.
func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "pass"
	case VerdictDelete:
		return "delete"
	case VerdictTimeout:
		return "timeout"
	case VerdictBan:
		return "ban"
	default:
		return "unknown"
	}
}

// Message is the neutral, platform-independent input a filter inspects.
type Message struct {
	// Text is the raw message body.
	Text string
	// Username is the author's login/display name (lower-case not required).
	Username string
	// EmoteCount is the number of native emote instances the platform has
	// already parsed for this message. Third-party emotes (BTTV/FFZ/7TV)
	// are out of scope for the pure-logic engine.
	EmoteCount int
	// FirstMsg reports whether this is the user's first-ever message in the
	// channel. Carried for callers that wish to apply stricter gating; the
	// core filters do not branch on it.
	FirstMsg bool
}

// UserContext carries who the author is, used for role exemptions and
// (future) age gating.
type UserContext struct {
	// Role is the author's privilege tier.
	Role Role
	// AccountAgeDays is the age of the platform account in days, or -1 if
	// unknown.
	AccountAgeDays int
	// FollowAgeDays is how long the user has followed the channel in days,
	// or -1 if unknown.
	FollowAgeDays int
}

// FilterResult is what a single filter returns, and also what
// [Engine.Evaluate] returns (the most-severe result).
type FilterResult struct {
	// Verdict is the severity assigned. VerdictPass means no violation.
	Verdict Verdict
	// FilterName identifies which filter produced this result.
	FilterName string
	// Reason is a short human-readable explanation, e.g. "78% caps".
	Reason string
	// MatchedText is the offending substring for audit purposes; may be "".
	MatchedText string
	// Timeout is the suggested base timeout for a VerdictTimeout result. The
	// escalation layer (a separate package) may override it. It is 0 for
	// VerdictDelete and VerdictPass.
	Timeout time.Duration
}

// FilterMode controls whether and how the engine acts.
type FilterMode int

const (
	// ModeOff disables the engine entirely; Evaluate always returns Pass.
	ModeOff FilterMode = iota
	// ModeDryRun still evaluates every filter (so callers can log what WOULD
	// happen) but the caller is expected not to execute the punishment.
	ModeDryRun
	// ModeActive evaluates and the caller executes the punishment.
	ModeActive
)

// MatchMode selects how a [BannedEntry] phrase is matched against a message.
type MatchMode int

const (
	// MatchAnywhere is a plain substring match.
	MatchAnywhere MatchMode = iota
	// MatchWord requires the phrase to appear on word boundaries.
	MatchWord
	// MatchExact requires the whole (trimmed) message to equal the phrase.
	MatchExact
	// MatchWildcard treats '*' in the phrase as ".*" and matches as a word.
	MatchWildcard
	// MatchRegex treats the phrase as a Go regular expression. A bad regex
	// makes [NewEngine] return an error.
	MatchRegex
)

// Config is the full, independently-tunable filter configuration. Each filter
// embeds an Enabled flag, an ExemptMinRole (users at or above that role bypass
// the individual filter - moderators and broadcasters are ALWAYS globally
// exempt regardless), a TimeoutSecs base timeout, plus its own parameters.
type Config struct {
	// Mode controls overall engine behaviour. Default ModeActive.
	Mode FilterMode

	Caps        CapsConfig
	Symbols     SymbolsConfig
	Links       LinksConfig
	Emotes      EmotesConfig
	Length      LengthConfig
	Repetition  RepetitionConfig
	BannedWords BannedWordsConfig
}

// CapsConfig governs the ALL-CAPS filter. It is ratio-based rather than an
// absolute uppercase count (the classic Nightbot footgun), and strips URLs
// before counting.
type CapsConfig struct {
	Enabled       bool
	ExemptMinRole Role
	TimeoutSecs   int
	// MinLength skips messages shorter than this many runes. Default 15.
	MinLength int
	// MaxCapsPercent is the maximum allowed fraction of ALPHABETIC characters
	// that may be uppercase, in the range 0..1. Default 0.60.
	MaxCapsPercent float64
}

// SymbolsConfig governs the symbol-spam filter (grouped runs AND percentage),
// plus optional Zalgo (combining-diacritic abuse) blocking.
type SymbolsConfig struct {
	Enabled       bool
	ExemptMinRole Role
	TimeoutSecs   int
	// MaxGroupedSymbols is the longest allowed consecutive run of symbol
	// characters. Default 8.
	MaxGroupedSymbols int
	// MaxSymbolPercent is the maximum allowed symbol fraction (0..1) once the
	// message reaches MinLengthForPercent. Default 0.40.
	MaxSymbolPercent float64
	// MinLengthForPercent gates the percentage check. Default 15.
	MinLengthForPercent int
	// BlockZalgo enables combining-mark abuse detection. Default true.
	BlockZalgo bool
}

// LinksConfig governs link detection. The !permit flow lives in the
// dispatcher/escalation layer, NOT here - this filter only detects links and
// honours the allow-list.
type LinksConfig struct {
	Enabled       bool
	ExemptMinRole Role
	TimeoutSecs   int
	// AllowList holds permitted domains/patterns. Wildcards: a leading "*."
	// matches any subdomain ("*.twitch.tv"), a trailing "/*" prefix-matches
	// the host ("clips.twitch.tv/*"), and a bare "*" matches everything.
	AllowList []string
	// BlockIPAddresses enables IPv4-literal detection. Default true.
	BlockIPAddresses bool
	// BlockDotVariants enables evasion detection like "twitch dot tv".
	// Default true.
	BlockDotVariants bool
}

// EmotesConfig governs the native-emote-count filter.
type EmotesConfig struct {
	Enabled       bool
	ExemptMinRole Role
	TimeoutSecs   int
	// MaxEmotes is the maximum allowed native emote instances. Default 12.
	MaxEmotes int
}

// LengthConfig governs the maximum message length filter.
type LengthConfig struct {
	Enabled       bool
	ExemptMinRole Role
	TimeoutSecs   int
	// MaxChars is the maximum allowed rune count. Default 375.
	MaxChars int
}

// RepetitionConfig governs the within-message repetition filter. It carries NO
// cross-message state - the escalation package has no role here.
type RepetitionConfig struct {
	Enabled       bool
	ExemptMinRole Role
	TimeoutSecs   int
	// MinLength skips messages shorter than this many runes. Default 20.
	MinLength int
	// MaxRepeatRatio is the maximum allowed fraction of tokens that are
	// repeats of the single most-common token, in 0..1. Default 0.50.
	MaxRepeatRatio float64
}

// BannedWordsConfig governs the banned-phrase filter.
type BannedWordsConfig struct {
	Enabled       bool
	ExemptMinRole Role
	TimeoutSecs   int
	// Entries is the list of banned phrases with per-entry match modes and
	// verdicts.
	Entries []BannedEntry
}

// BannedEntry is a single banned-phrase rule.
type BannedEntry struct {
	// Phrase is the needle to match. Its interpretation depends on MatchMode.
	Phrase string
	// MatchMode selects how Phrase is matched.
	MatchMode MatchMode
	// CaseSensitive, when false (the default), matches case-insensitively.
	CaseSensitive bool
	// Verdict is the severity to assign on a match - usually VerdictTimeout
	// or VerdictBan.
	Verdict Verdict
}

// DefaultConfig returns a Config with every filter DISABLED but pre-populated
// with sensible default parameters, so enabling any single filter "just
// works" without further tuning. Mode defaults to ModeActive.
func DefaultConfig() Config {
	return Config{
		Mode: ModeActive,
		Caps: CapsConfig{
			Enabled:        false,
			MinLength:      15,
			MaxCapsPercent: 0.60,
		},
		Symbols: SymbolsConfig{
			Enabled:             false,
			MaxGroupedSymbols:   8,
			MaxSymbolPercent:    0.40,
			MinLengthForPercent: 15,
			BlockZalgo:          true,
		},
		Links: LinksConfig{
			Enabled:          false,
			BlockIPAddresses: true,
			BlockDotVariants: true,
		},
		Emotes: EmotesConfig{
			Enabled:   false,
			MaxEmotes: 12,
		},
		Length: LengthConfig{
			Enabled:  false,
			MaxChars: 375,
		},
		Repetition: RepetitionConfig{
			Enabled:        false,
			MinLength:      20,
			MaxRepeatRatio: 0.50,
		},
		BannedWords: BannedWordsConfig{
			Enabled: false,
		},
	}
}

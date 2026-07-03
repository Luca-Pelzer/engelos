package contextmod

import (
	"regexp"
	"strings"
	"unicode"
)

// minPrefilterLen is the length below which a message is treated as too short to
// be a context-dependent violation, unless it carries a risk signal.
const minPrefilterLen = 6

// urlOrInvite matches links and platform invites that hit-and-run spam relies
// on (foreign promo, follow-for-follow, bot sites). Any hit forces AI review.
var urlOrInvite = regexp.MustCompile(`(?i)(https?://|www\.|\b[\w-]+\.(com|net|org|io|tv|gg|me|xyz|live|shop|link)\b|discord\.gg|t\.me|twitch\.tv/|youtu)`)

// spamMarkers are lower-cased substrings that strongly suggest promo/spam even
// in a short message, so they always escalate to the AI.
var spamMarkers = []string{
	"follow me", "followt mir", "folgt mir", "follow4follow", "f4f",
	"check my", "check out my", "schau mein", "gratis", "free followers",
	"free primes", "viewers kaufen", "buy followers", "best viewers",
	"cheap", "promo", "onlyfans", "only fans",
}

// needsAI reports whether text warrants an AI classification call. It is the
// cheap pre-filter that keeps the bulk of clearly-harmless chatter off the paid
// backend. It is deliberately fail-safe: when in doubt it returns true (ask the
// AI) so nothing a human would flag slips through just to save a call. It only
// returns false for messages that are both short and carry no risk signal, or
// that are pure emote/punctuation spam.
func needsAI(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)

	// Risk signals always escalate, regardless of length.
	if urlOrInvite.MatchString(lower) {
		return true
	}
	for _, m := range spamMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}

	// Pure emote / repeated-token shouting (e.g. "KEKW KEKW KEKW") is noise the
	// rule engine already let pass; the AI cannot add anything.
	if isRepeatedTokens(t) {
		return false
	}

	// No risk signal and very short: not enough context to be a real violation.
	if len([]rune(t)) < minPrefilterLen {
		return false
	}

	// No letters at all (pure punctuation/numbers/emoji) carries no judgeable
	// content.
	if !hasLetter(t) {
		return false
	}

	return true
}

// isRepeatedTokens reports whether the message is a single token repeated (chat
// emote spam), where all whitespace-separated tokens are identical.
func isRepeatedTokens(s string) bool {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return false
	}
	first := fields[0]
	for _, f := range fields[1:] {
		if !strings.EqualFold(f, first) {
			return false
		}
	}
	return true
}

// hasLetter reports whether s contains at least one unicode letter.
func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

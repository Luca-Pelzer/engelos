package translate

import (
	"strings"
	"unicode"

	"github.com/abadojack/whatlanggo"
)

// AlreadyInTargetLang reports whether text is confidently already written in
// the language named by targetLang (an ISO 639-1 code such as "en"), so the
// caller can skip a paid/proxied translation call.
//
// It is deliberately conservative: it only returns true when detection is both
// reliable AND agrees with the target. Any uncertainty returns false, which
// means "go ahead and translate" - over-translating a borderline message is
// cheaper and less surprising than dropping a message that needed translation.
//
// Detection has two tiers. First a zero-allocation Unicode-script scan handles
// the unambiguous non-Latin cases (CJK, Hangul, Kana, Cyrillic, ...), where the
// script alone settles the language family far more cheaply and reliably than a
// statistical model on a short chat line. Only Latin-script text falls through
// to whatlanggo for full language identification.
func AlreadyInTargetLang(text, targetLang string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		// Empty input is a no-op for translation; treat as "already there"
		// so the caller skips it.
		return true
	}
	target := normalizeLangCode(targetLang)
	if target == "" {
		target = "en"
	}

	// Tier 1: script families that map cleanly to a target language. When the
	// text's script does not match the target's script family, it is by
	// definition NOT already in the target, so translation should proceed.
	if fam, ok := scriptFamily(t); ok {
		return fam == scriptFamilyForLang(target)
	}

	// Tier 2: Latin (or mixed/undetermined) script. Use whatlanggo and only
	// trust a reliable verdict.
	info := whatlanggo.Detect(t)
	if !info.IsReliable() {
		return false
	}
	return normalizeLangCode(info.Lang.Iso6391()) == target
}

// normalizeLangCode lowercases, trims, and reduces a code like "en-US" to its
// primary subtag "en" for comparison.
func normalizeLangCode(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexByte(c, '-'); i > 0 {
		c = c[:i]
	}
	return c
}

// scriptFamily classifies text by the first strongly-scripted rune it contains,
// returning a coarse family label and ok=false when the text is Latin-only or
// carries no decisive script (so the caller falls through to whatlanggo).
//
// The scan stops at the first decisive rune: a single CJK/Hangul/Kana/Cyrillic/
// Arabic/etc. character is enough to settle the family for a short chat line.
func scriptFamily(text string) (string, bool) {
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			return "han", true
		case unicode.Is(unicode.Hangul, r):
			return "korean", true
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			return "japanese", true
		case unicode.Is(unicode.Cyrillic, r):
			return "cyrillic", true
		case unicode.Is(unicode.Arabic, r):
			return "arabic", true
		case unicode.Is(unicode.Hebrew, r):
			return "hebrew", true
		case unicode.Is(unicode.Greek, r):
			return "greek", true
		case unicode.Is(unicode.Devanagari, r):
			return "devanagari", true
		case unicode.Is(unicode.Thai, r):
			return "thai", true
		}
	}
	return "", false
}

// scriptFamilyForLang maps a primary ISO 639-1 code to the script family it is
// written in, for the non-Latin languages tier 1 can decide. Latin-script
// languages (en, es, de, fr, pt, ...) all map to "latin", which never matches a
// tier-1 family, so they are routed to whatlanggo instead.
func scriptFamilyForLang(lang string) string {
	switch lang {
	case "zh":
		return "han"
	case "ja":
		return "japanese"
	case "ko":
		return "korean"
	case "ru", "uk", "bg", "sr", "mk", "be":
		return "cyrillic"
	case "ar", "fa", "ur":
		return "arabic"
	case "he":
		return "hebrew"
	case "el":
		return "greek"
	case "hi", "mr", "ne":
		return "devanagari"
	case "th":
		return "thai"
	default:
		return "latin"
	}
}

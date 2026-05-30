package automod

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// tldAlternation is the set of top-level domains the bare-domain detector
// recognises, plus a generic two-letter country-code fallback. Kept
// deliberately small to limit false positives on ordinary punctuation like
// "e.g".
const tldAlternation = `com|net|org|io|tv|gg|live|me|co|xyz|info|[a-z]{2}`

// bannedMatcher is a pre-compiled banned-word rule. re is non-nil for
// MatchWord, MatchWildcard and MatchRegex; the substring/exact modes match at
// evaluation time without a regex.
type bannedMatcher struct {
	entry BannedEntry
	re    *regexp.Regexp
}

// Engine is the stateless, concurrency-safe filter runner. After NewEngine
// returns it is read-only, so Evaluate may be called from many goroutines.
type Engine struct {
	cfg Config

	urlRegex        *regexp.Regexp
	ipRegex         *regexp.Regexp
	dotVariantRegex *regexp.Regexp
	banned          []bannedMatcher
}

// NewEngine validates the configuration, pre-compiles every regular
// expression (URL detector, IP detector, dot-variant detector and the banned
// word entries) and returns a ready engine. It returns an error if any
// MatchRegex banned entry contains an invalid Go regular expression.
func NewEngine(cfg Config) (*Engine, error) {
	urlRe := regexp.MustCompile(
		`(?i)\b(?:https?://|www\.)?((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+(?:` +
			tldAlternation + `))\b(?:[:/?#][^\s]*)?`,
	)
	ipRe := regexp.MustCompile(
		`\b(?:https?://)?(?:(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\.){3}` +
			`(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])\b(?:[:/?#][^\s]*)?`,
	)
	dotRe := regexp.MustCompile(
		`(?i)\b[\w-]+\s+(?:dot|\.)\s+(?:com|net|org|tv|gg|io)\b`,
	)

	e := &Engine{
		cfg:             cfg,
		urlRegex:        urlRe,
		ipRegex:         ipRe,
		dotVariantRegex: dotRe,
	}

	for i, entry := range cfg.BannedWords.Entries {
		m := bannedMatcher{entry: entry}
		flag := ""
		if !entry.CaseSensitive {
			flag = "(?i)"
		}
		switch entry.MatchMode {
		case MatchWord:
			re, err := regexp.Compile(flag + `\b` + regexp.QuoteMeta(entry.Phrase) + `\b`)
			if err != nil {
				return nil, fmt.Errorf("automod: banned entry %d (word %q): %w", i, entry.Phrase, err)
			}
			m.re = re
		case MatchWildcard:
			pat := wildcardToRegex(entry.Phrase)
			re, err := regexp.Compile(flag + `\b` + pat + `\b`)
			if err != nil {
				return nil, fmt.Errorf("automod: banned entry %d (wildcard %q): %w", i, entry.Phrase, err)
			}
			m.re = re
		case MatchRegex:
			re, err := regexp.Compile(flag + entry.Phrase)
			if err != nil {
				return nil, fmt.Errorf("automod: banned entry %d (regex %q): %w", i, entry.Phrase, err)
			}
			m.re = re
		case MatchAnywhere, MatchExact:
			// matched at evaluation time without a regex.
		default:
			return nil, fmt.Errorf("automod: banned entry %d: unknown match mode %d", i, entry.MatchMode)
		}
		e.banned = append(e.banned, m)
	}

	return e, nil
}

// Config returns a copy of the configuration the engine was built with.
func (e *Engine) Config() Config { return e.cfg }

// Evaluate runs every enabled, non-exempt filter against the message and
// returns the single most-severe [FilterResult]. Ties between equal verdicts
// are broken by a fixed filter order (BannedWords, Links, Caps, Symbols,
// Emotes, Length, Repetition). When nothing fires it returns a VerdictPass
// result.
//
// Short-circuits:
//   - Mode == ModeOff returns Pass immediately.
//   - Moderators and the broadcaster (Role >= RoleModerator) are globally
//     exempt and always receive Pass.
//
// Evaluate is safe for concurrent use.
func (e *Engine) Evaluate(msg Message, user UserContext) FilterResult {
	pass := FilterResult{Verdict: VerdictPass, FilterName: "none", Reason: "ok"}

	if e.cfg.Mode == ModeOff {
		return pass
	}
	if user.Role >= RoleModerator {
		return pass
	}

	best := pass
	consider := func(res FilterResult) {
		// strictly-greater replaces, so the earliest filter wins on ties.
		if res.Verdict > best.Verdict {
			best = res
		}
	}

	// Evaluate strictly in the stable tie-break order.
	if e.active(e.cfg.BannedWords.Enabled, e.cfg.BannedWords.ExemptMinRole, user) {
		consider(e.evalBannedWords(msg))
	}
	if e.active(e.cfg.Links.Enabled, e.cfg.Links.ExemptMinRole, user) {
		consider(e.evalLinks(msg))
	}
	if e.active(e.cfg.Caps.Enabled, e.cfg.Caps.ExemptMinRole, user) {
		consider(e.evalCaps(msg))
	}
	if e.active(e.cfg.Symbols.Enabled, e.cfg.Symbols.ExemptMinRole, user) {
		consider(e.evalSymbols(msg))
	}
	if e.active(e.cfg.Emotes.Enabled, e.cfg.Emotes.ExemptMinRole, user) {
		consider(e.evalEmotes(msg))
	}
	if e.active(e.cfg.Length.Enabled, e.cfg.Length.ExemptMinRole, user) {
		consider(e.evalLength(msg))
	}
	if e.active(e.cfg.Repetition.Enabled, e.cfg.Repetition.ExemptMinRole, user) {
		consider(e.evalRepetition(msg))
	}

	return best
}

// active reports whether a filter should run for this user: it must be enabled
// and the user must NOT be role-exempt for it.
func (e *Engine) active(enabled bool, exemptMin Role, user UserContext) bool {
	return enabled && !roleExempt(user.Role, exemptMin)
}

// roleExempt reports whether a user at userRole bypasses a filter whose
// per-filter ExemptMinRole is exemptMin.
//
// Semantics: a user is exempt when their role is at or above exemptMin. The
// zero value (RoleEveryone) means "no per-filter exemption" so that a freshly
// enabled filter applies to all non-staff users — moderators and the
// broadcaster are always exempt via the global short-circuit in Evaluate.
func roleExempt(userRole, exemptMin Role) bool {
	if exemptMin <= RoleEveryone {
		return false
	}
	return userRole >= exemptMin
}

// verdictForTimeout maps a configured base-timeout to a verdict: zero seconds
// means "delete only", any positive value means "timeout".
func verdictForTimeout(secs int) Verdict {
	if secs <= 0 {
		return VerdictDelete
	}
	return VerdictTimeout
}

// timeoutDuration is the suggested base timeout carried on a result: only
// meaningful for VerdictTimeout.
func timeoutDuration(v Verdict, secs int) time.Duration {
	if v == VerdictTimeout && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// stripForTextAnalysis removes URLs and IP literals from text and collapses
// runs of whitespace to single spaces. Caps and Symbols use it so that links
// do not skew their character ratios.
//
// LIMITATION: native emotes are provided only as a COUNT (Message.EmoteCount),
// not as positions or text, so their characters cannot be removed here. A
// message that is mostly emote names may therefore read as high-caps or
// high-symbol to those filters. The Emotes filter handles emote spam directly.
func (e *Engine) stripForTextAnalysis(text string) string {
	s := e.urlRegex.ReplaceAllString(text, " ")
	s = e.ipRegex.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// evalCaps implements the ratio-based ALL-CAPS filter.
func (e *Engine) evalCaps(msg Message) FilterResult {
	cfg := e.cfg.Caps
	stripped := e.stripForTextAnalysis(msg.Text)
	if utf8.RuneCountInString(stripped) < cfg.MinLength {
		return FilterResult{Verdict: VerdictPass, FilterName: "caps"}
	}

	var alpha, upper int
	for _, r := range stripped {
		if unicode.IsLetter(r) {
			alpha++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	if alpha == 0 {
		return FilterResult{Verdict: VerdictPass, FilterName: "caps"}
	}

	ratio := float64(upper) / float64(alpha)
	if ratio > cfg.MaxCapsPercent {
		v := verdictForTimeout(cfg.TimeoutSecs)
		return FilterResult{
			Verdict:    v,
			FilterName: "caps",
			Reason:     fmt.Sprintf("%d%% caps", int(ratio*100+0.5)),
			Timeout:    timeoutDuration(v, cfg.TimeoutSecs),
		}
	}
	return FilterResult{Verdict: VerdictPass, FilterName: "caps"}
}

// evalSymbols implements grouped-run, percentage and Zalgo symbol detection.
func (e *Engine) evalSymbols(msg Message) FilterResult {
	cfg := e.cfg.Symbols
	stripped := e.stripForTextAnalysis(msg.Text)
	runes := []rune(stripped)
	n := len(runes)

	var symbolCount, longestRun, curRun int
	for _, r := range runes {
		if isSymbolChar(r) {
			symbolCount++
			curRun++
			if curRun > longestRun {
				longestRun = curRun
			}
		} else {
			curRun = 0
		}
	}

	v := verdictForTimeout(cfg.TimeoutSecs)

	if longestRun > cfg.MaxGroupedSymbols {
		return FilterResult{
			Verdict:    v,
			FilterName: "symbols",
			Reason:     fmt.Sprintf("symbol run of %d", longestRun),
			Timeout:    timeoutDuration(v, cfg.TimeoutSecs),
		}
	}

	if n >= cfg.MinLengthForPercent && n > 0 {
		ratio := float64(symbolCount) / float64(n)
		if ratio > cfg.MaxSymbolPercent {
			return FilterResult{
				Verdict:    v,
				FilterName: "symbols",
				Reason:     fmt.Sprintf("%d%% symbols", int(ratio*100+0.5)),
				Timeout:    timeoutDuration(v, cfg.TimeoutSecs),
			}
		}
	}

	if cfg.BlockZalgo {
		for _, word := range strings.Fields(stripped) {
			marks := 0
			for _, r := range word {
				if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
					marks++
				}
			}
			if marks >= 3 {
				return FilterResult{
					Verdict:     v,
					FilterName:  "symbols",
					Reason:      "zalgo / combining-mark abuse",
					MatchedText: word,
					Timeout:     timeoutDuration(v, cfg.TimeoutSecs),
				}
			}
		}
	}

	return FilterResult{Verdict: VerdictPass, FilterName: "symbols"}
}

// evalLinks implements URL, IP-literal and dot-variant detection with
// allow-list honouring.
func (e *Engine) evalLinks(msg Message) FilterResult {
	cfg := e.cfg.Links
	v := verdictForTimeout(cfg.TimeoutSecs)

	for _, sm := range e.urlRegex.FindAllStringSubmatch(msg.Text, -1) {
		host := sm[1]
		if !hostAllowed(host, cfg.AllowList) {
			return FilterResult{
				Verdict:     v,
				FilterName:  "links",
				Reason:      "link not allow-listed",
				MatchedText: sm[0],
				Timeout:     timeoutDuration(v, cfg.TimeoutSecs),
			}
		}
	}

	if cfg.BlockIPAddresses {
		for _, m := range e.ipRegex.FindAllString(msg.Text, -1) {
			host := ipHost(m)
			if !hostAllowed(host, cfg.AllowList) {
				return FilterResult{
					Verdict:     v,
					FilterName:  "links",
					Reason:      "ip-address link",
					MatchedText: m,
					Timeout:     timeoutDuration(v, cfg.TimeoutSecs),
				}
			}
		}
	}

	if cfg.BlockDotVariants {
		for _, m := range e.dotVariantRegex.FindAllString(msg.Text, -1) {
			if hostAllowed(dotVariantDomain(m), cfg.AllowList) {
				continue
			}
			return FilterResult{
				Verdict:     v,
				FilterName:  "links",
				Reason:      "obfuscated link",
				MatchedText: m,
				Timeout:     timeoutDuration(v, cfg.TimeoutSecs),
			}
		}
	}

	return FilterResult{Verdict: VerdictPass, FilterName: "links"}
}

// evalEmotes implements the native-emote-count filter.
func (e *Engine) evalEmotes(msg Message) FilterResult {
	cfg := e.cfg.Emotes
	if msg.EmoteCount > cfg.MaxEmotes {
		v := verdictForTimeout(cfg.TimeoutSecs)
		return FilterResult{
			Verdict:    v,
			FilterName: "emotes",
			Reason:     fmt.Sprintf("%d emotes", msg.EmoteCount),
			Timeout:    timeoutDuration(v, cfg.TimeoutSecs),
		}
	}
	return FilterResult{Verdict: VerdictPass, FilterName: "emotes"}
}

// evalLength implements the maximum-length filter.
func (e *Engine) evalLength(msg Message) FilterResult {
	cfg := e.cfg.Length
	count := utf8.RuneCountInString(msg.Text)
	if count > cfg.MaxChars {
		v := verdictForTimeout(cfg.TimeoutSecs)
		return FilterResult{
			Verdict:    v,
			FilterName: "length",
			Reason:     fmt.Sprintf("message length %d", count),
			Timeout:    timeoutDuration(v, cfg.TimeoutSecs),
		}
	}
	return FilterResult{Verdict: VerdictPass, FilterName: "length"}
}

// evalRepetition implements within-message token-repetition detection.
func (e *Engine) evalRepetition(msg Message) FilterResult {
	cfg := e.cfg.Repetition
	if utf8.RuneCountInString(msg.Text) < cfg.MinLength {
		return FilterResult{Verdict: VerdictPass, FilterName: "repetition"}
	}
	tokens := strings.Fields(strings.ToLower(msg.Text))
	if len(tokens) < 4 {
		return FilterResult{Verdict: VerdictPass, FilterName: "repetition"}
	}

	counts := make(map[string]int, len(tokens))
	maxTok := ""
	maxCount := 0
	for _, t := range tokens {
		counts[t]++
		if counts[t] > maxCount {
			maxCount = counts[t]
			maxTok = t
		}
	}

	ratio := float64(maxCount) / float64(len(tokens))
	if ratio > cfg.MaxRepeatRatio {
		v := verdictForTimeout(cfg.TimeoutSecs)
		return FilterResult{
			Verdict:     v,
			FilterName:  "repetition",
			Reason:      fmt.Sprintf("%q repeated %d×", maxTok, maxCount),
			MatchedText: maxTok,
			Timeout:     timeoutDuration(v, cfg.TimeoutSecs),
		}
	}
	return FilterResult{Verdict: VerdictPass, FilterName: "repetition"}
}

// evalBannedWords implements the banned-phrase filter across all match modes.
func (e *Engine) evalBannedWords(msg Message) FilterResult {
	text := msg.Text
	trimmed := strings.TrimSpace(text)
	lowerText := strings.ToLower(text)
	lowerTrimmed := strings.ToLower(trimmed)

	for _, m := range e.banned {
		matched, what := matchBanned(m, text, trimmed, lowerText, lowerTrimmed)
		if !matched {
			continue
		}
		v := m.entry.Verdict
		return FilterResult{
			Verdict:     v,
			FilterName:  "banned_words",
			Reason:      "banned phrase",
			MatchedText: what,
			Timeout:     timeoutDuration(v, e.cfg.BannedWords.TimeoutSecs),
		}
	}
	return FilterResult{Verdict: VerdictPass, FilterName: "banned_words"}
}

// matchBanned applies a single compiled banned-word rule. It returns whether
// the rule matched and the offending substring for audit.
func matchBanned(m bannedMatcher, text, trimmed, lowerText, lowerTrimmed string) (bool, string) {
	switch m.entry.MatchMode {
	case MatchAnywhere:
		if m.entry.CaseSensitive {
			if strings.Contains(text, m.entry.Phrase) {
				return true, m.entry.Phrase
			}
			return false, ""
		}
		if strings.Contains(lowerText, strings.ToLower(m.entry.Phrase)) {
			return true, m.entry.Phrase
		}
		return false, ""
	case MatchExact:
		if m.entry.CaseSensitive {
			if trimmed == m.entry.Phrase {
				return true, m.entry.Phrase
			}
			return false, ""
		}
		if lowerTrimmed == strings.ToLower(m.entry.Phrase) {
			return true, m.entry.Phrase
		}
		return false, ""
	case MatchWord, MatchWildcard, MatchRegex:
		if m.re == nil {
			return false, ""
		}
		if hit := m.re.FindString(text); hit != "" {
			return true, hit
		}
		// A regex can legitimately match an empty string; fall back to a
		// boolean test so such rules still fire.
		if m.re.MatchString(text) {
			return true, m.entry.Phrase
		}
		return false, ""
	}
	return false, ""
}

// --- helpers -------------------------------------------------------------

// isSymbolChar reports whether r counts toward symbol-spam. Letters, digits,
// spaces, combining marks and emoji are excluded; remaining punctuation and
// symbols count. Emoji are excluded pragmatically by code-point range so that
// emote/emoji-heavy messages are policed by the Emotes filter instead.
func isSymbolChar(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
		return false
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return false
	}
	if isEmojiish(r) {
		return false
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// isEmojiish reports whether r falls in a common emoji / pictograph range.
func isEmojiish(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF:
		return true
	case r == 0x200D, r >= 0xFE00 && r <= 0xFE0F:
		return true
	}
	return false
}

// wildcardToRegex converts a banned-word wildcard phrase into a regex
// fragment: literal text is escaped and each '*' becomes ".*".
func wildcardToRegex(phrase string) string {
	var b strings.Builder
	for _, part := range strings.Split(phrase, "*") {
		if b.Len() > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(part))
	}
	return b.String()
}

// hostAllowed reports whether host matches any allow-list pattern. Patterns
// support: "*" (everything), "*.suffix" (host==suffix or any subdomain),
// "prefix.*" (host==prefix or any deeper label), and "domain/path..." (the
// path is ignored and the host portion is matched exactly).
func hostAllowed(host string, allow []string) bool {
	host = strings.ToLower(strings.Trim(host, "."))
	if host == "" {
		return false
	}
	for _, p := range allow {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if p == "*" {
			return true
		}
		if i := strings.IndexByte(p, '/'); i >= 0 {
			p = p[:i]
		}
		if p == "" {
			continue
		}
		switch {
		case strings.HasPrefix(p, "*."):
			suffix := p[2:]
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return true
			}
		case strings.HasSuffix(p, ".*"):
			prefix := p[:len(p)-2]
			if host == prefix || strings.HasPrefix(host, prefix+".") {
				return true
			}
		default:
			if host == p {
				return true
			}
		}
	}
	return false
}

// ipHost extracts the bare IPv4 host from a matched IP "URL", dropping any
// scheme, port or path.
func ipHost(match string) string {
	s := match
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/:?#"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// dotVariantDomain reconstructs a normal domain from an obfuscated
// "word dot tld" match so it can be checked against the allow-list.
func dotVariantDomain(match string) string {
	var parts []string
	for _, tok := range strings.Fields(strings.ToLower(match)) {
		if tok == "dot" || tok == "." {
			continue
		}
		parts = append(parts, tok)
	}
	return strings.Join(parts, ".")
}

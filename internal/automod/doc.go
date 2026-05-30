// Package automod implements the pure-logic core of engelOS's chat AutoMod
// filter engine: a stateless, platform-neutral detector that inspects an
// individual chat message and returns the single most-severe moderation
// [Verdict] across a set of independently-configurable filters.
//
// # Scope and decoupling
//
// This package is intentionally self-contained and depends only on the Go
// standard library. It does NOT import the commands engine, the runtime
// dispatcher or any platform adapter, so the privilege ladder ([Role]) is
// redefined locally to avoid an import cycle.
//
// The engine OWNS detection only. It never talks to a chat platform and it
// never mutates any state. A dispatcher calls [Engine.Evaluate] for every
// message BEFORE command routing; the dispatcher (not this package) then
// executes the resulting punishment - delete, timeout or ban - and records
// escalation. Offense counting, audit logging and the link "!permit" flow
// live in a SEPARATE package and are deliberately absent here.
//
// # Filters
//
// [Config] holds seven independent filters, each with an Enabled flag, a
// per-filter ExemptMinRole, a base TimeoutSecs and its own parameters:
//
//   - Caps:        ratio of uppercase to alphabetic characters (URLs stripped
//     first); fixes the classic absolute-count footgun.
//   - Symbols:     longest consecutive symbol run, symbol percentage, and
//     optional Zalgo (combining-mark abuse) blocking.
//   - Links:       URL, IPv4-literal and "dot-variant" detection with an
//     allow-list (subdomain, path and "*" wildcards supported).
//   - Emotes:      native emote-instance count (Message.EmoteCount).
//   - Length:      maximum rune count.
//   - Repetition:  within-message repeated-token ratio (no cross-message
//     state).
//   - BannedWords: phrase list with substring, word, exact, wildcard and
//     regex match modes, each carrying its own per-entry verdict.
//
// [DefaultConfig] returns every filter DISABLED but pre-filled with sensible
// defaults so enabling any single filter "just works".
//
// # Evaluation
//
// [Engine.Evaluate] short-circuits to [VerdictPass] when [Config.Mode] is
// [ModeOff] or the author is a moderator/broadcaster (globally exempt). It
// otherwise runs every enabled, non-exempt filter and returns the result with
// the highest [Verdict]; ties are broken by a fixed filter order (BannedWords,
// Links, Caps, Symbols, Emotes, Length, Repetition). [ModeDryRun] still
// evaluates - the caller decides not to act.
//
// # Concurrency
//
// [NewEngine] pre-compiles all regular expressions (returning an error on a
// bad MatchRegex banned entry). The resulting [Engine] is read-only, holds no
// mutable state and is safe for concurrent [Engine.Evaluate] calls.
//
// # Known limitation
//
// Native emotes are provided only as a count, not as text or positions, so the
// Caps and Symbols filters cannot strip emote characters before computing
// their ratios. Emote-heavy messages are therefore policed by the Emotes
// filter rather than by Caps/Symbols.
package automod

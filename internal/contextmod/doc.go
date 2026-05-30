// Package contextmod is an AI escalation layer for moderation: it asks a Claude
// backend to judge borderline chat messages that the cheap rule-based AutoMod
// cannot decide (sarcasm, context-dependent phrasing), using the channel's
// plain-language rules.
//
// # Fail-open and decoupled
//
// The [Escalator] returns a [Verdict] of allow, delete, timeout, or unknown.
// Any backend error, unparseable answer, or rate-limited call yields
// VerdictUnknown, which the caller MUST treat as "keep my existing behaviour"
// rather than acting on it. This keeps AI moderation strictly additive: it can
// catch things the rules miss but never weakens the rule-based path.
//
// A global rate limit bounds AI calls so a flood of borderline messages cannot
// run up unbounded cost. The Claude client is injected as a [Backend]; the
// package performs no HTTP, owns no credentials, and imports nothing under
// engelos/internal.
package contextmod

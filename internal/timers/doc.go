// Package timers persists and fires periodic auto-announcements - the
// "!addtimer rules 600 Follow the rules!" feature familiar from Nightbot
// and StreamElements.
//
// The store is SQLite-backed (pure-Go via modernc.org/sqlite). Timers are
// scoped to (tenant, channel); name is unique inside that scope and stored
// lower-cased with a leading "!" stripped, so callers may pass either
// "!rules" or "rules". Intervals are persisted as INTEGER nanoseconds and
// reject anything below [MinInterval] to prevent chat spam.
//
// The [Scheduler] loads enabled timers and posts each timer's message to
// chat via a narrow [Sender] every [Timer.Interval], optionally gated
// behind [Timer.MinChatLines] of chat activity so the bot never talks to
// an empty room. See the [Scheduler] type doc for the precise firing
// semantics (startup lastFired, per-channel activity gate, send-error
// handling).
//
// # Decoupling
//
// This package imports nothing under engelos/internal: the wiring layer in
// main adapts the platform adapters to the [Sender] interface, and the
// commands engine talks to a narrow management interface declared in
// internal/commands - not to [Store] directly. Keeping the dependency
// one-way avoids import cycles and lets the persistence and scheduling
// layers evolve independently.
package timers

// Package quotes persists memorable chat lines as numbered quotes - the
// "!addquote" / "!quote" feature familiar from Nightbot and StreamElements.
//
// The store is SQLite-backed (pure-Go via modernc.org/sqlite). Quotes are
// scoped to (tenant, channel). Each quote carries a per-(tenant, channel)
// Number: a 1-based sequence shown to users (e.g. "!quote 3"). The Number
// is NOT a global primary key - the internal key is a ULID [Quote.ID].
//
// # Per-channel numbering and gaps
//
// On [Store.Add] the new quote's Number is assigned as
// MAX(number)+1 for its (tenant, channel), computed under the store mutex
// so concurrent Adds cannot collide. [Store.Delete] does NOT renumber the
// remaining quotes: gaps are expected and intentional, so "!quote 5"
// always refers to the same quote for the life of the channel.
//
// # Decoupling
//
// This package imports nothing under engelos/internal. The commands engine
// talks to a narrow management interface declared in internal/commands -
// not to [Store] directly; main wires a thin adapter. Keeping the
// dependency one-way avoids import cycles and lets the persistence layer
// evolve independently.
package quotes

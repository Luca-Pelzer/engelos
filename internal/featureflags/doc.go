// Package featureflags is a small SQLite-backed store of per-channel
// feature on/off toggles for engelOS.
//
// A feature flag is one boolean per (tenant, channel, feature-key) — for
// example the "economy" mini-game being enabled for a single channel. Only
// EXPLICIT overrides are persisted: an unset flag has no row, and callers
// supply their own default for that case (see Store.GetOrDefault). This
// keeps the table to exactly the toggles an operator has deliberately
// flipped, rather than a dense matrix of every feature for every channel.
//
// The implementation mirrors internal/counters: a pure-Go modernc.org/sqlite
// database opened with WAL journal mode, foreign_keys ON,
// synchronous=NORMAL and a 5s busy_timeout, restricted to a single
// connection so writes serialise. Set performs an atomic upsert via
// INSERT ... ON CONFLICT DO UPDATE, so concurrent writers never collide.
// Feature keys are lower-cased, trimmed and validated against
// [a-z0-9_]+ (max 40 chars); channels are lower-cased, trimmed and have a
// leading "#" stripped. Invalid input wraps ErrInvalid (compare with
// errors.Is).
//
// Use "file::memory:?cache=shared" (or a unique file:...?mode=memory&cache=shared
// DSN) for tests and a file path in production.
package featureflags

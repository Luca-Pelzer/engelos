// Package automodstate is the stateful memory layer for engelOS AutoMod.
//
// It holds the two pieces of moderation state that must outlive any single
// message but that the detection filters themselves are deliberately ignorant
// of. The filters (a separate package) answer "is this message a violation?";
// this package answers "given a violation just happened, what do we do, and how
// do we remember it?".
//
// # Escalation
//
// Escalator is an in-memory, concurrency-safe tracker of per-(channel, user,
// filter) offense counts. Each new violation is mapped onto an escalation
// ladder - by default warn, then 60s, 10m and 24h timeouts, then a permanent
// ban - so repeat offenders are punished progressively. A decay window lets a
// user who behaves for long enough start over with a clean record.
//
// PermitTracker implements the link `!permit <user>` flow: a moderator grants a
// user a short window in which they may post one link without tripping the link
// filter. The grant is consumed exactly once.
//
// # Audit log
//
// AuditStore is a SQLite-backed log of every enforcement action AutoMod takes,
// including dry-run (shadow) actions that were decided but not executed. It
// captures the full context - message text, filter, matched substring, action
// and duration - so streamers can review or reverse a decision later. It
// mirrors the persistence conventions of internal/counters (WAL, single
// connection, embedded migrations, ULID ids).
//
// This package imports no other engelOS package; the dispatcher wires the
// escalation decision and the audit record together at the call site.
package automodstate

// Package loyalty provides a spendable points economy scoped to a
// (tenant, channel) - each viewer holds a per-channel balance they earn by
// chatting and spend on games, rewards, or gifts to other viewers.
//
// A [Store] persists accounts in pure-Go SQLite (modernc.org/sqlite,
// CGO-free). Earn is an atomic "INSERT ... ON CONFLICT DO UPDATE SET
// balance = balance + ?" upsert, so concurrent earns never lose an
// increment. Spend and Transfer run inside single-writer transactions
// (the pool is capped at one connection) that read, check, and write the
// balance indivisibly, so an account can never be overdrawn and gifted
// funds are always conserved. Balances are never negative.
//
// The store is self-contained: it imports no other engelOS package and
// exposes only the [Store] interface, which the dispatcher and command
// handlers consume through interfaces they define themselves.
package loyalty

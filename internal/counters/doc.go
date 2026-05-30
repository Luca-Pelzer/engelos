// Package counters provides named integer counters scoped to a
// (tenant, channel) - the classic streamer "death counter" feature
// (e.g. "!deaths" shows the value, mods bump it).
//
// A [Store] persists counters in pure-Go SQLite (modernc.org/sqlite,
// CGO-free). Increments are atomic: Add uses an "INSERT ... ON CONFLICT
// DO UPDATE SET value = value + ?" upsert under a process mutex, so
// concurrent increments of the same counter never lose an update.
package counters

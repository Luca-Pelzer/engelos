// Package liveops provides scheduled Live-Ops events scoped to a
// (tenant, channel) — the streamer "what's next?" feature (e.g. a
// "Double Points Weekend" or "Season 3 starts" the bot counts down to).
//
// A [Store] persists events in pure-Go SQLite (modernc.org/sqlite,
// CGO-free). Each event carries a per-(tenant, channel) 1-based Number
// assigned as MAX(number)+1 under a process mutex, so concurrent Adds to
// the same channel never collide on the unique index. Deletes leave gaps
// in the number sequence on purpose so a given number always refers to the
// same event.
package liveops

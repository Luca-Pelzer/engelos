// Package streak implements engelOS's Streak-System: a Duolingo-style
// watch-streak ledger that rewards viewers for consecutive UTC days of chat
// activity.
//
// Each viewer accumulates a current-streak (DaysCurrent) and a personal-best
// (DaysLongest). A [System.Tick] records activity for a viewer and emits one
// or more append-only events that describe the streak transition:
//
//   - On the first activity, [EventTypeStreakStarted] is emitted.
//   - Repeated ticks within the same UTC day are recorded as
//     [EventTypeStreakContinued] with SameDayReTick=true and do not advance
//     the day count.
//   - The first tick on a new UTC day advances DaysCurrent by 1 and may
//     emit [EventTypeStreakMilestone] when a threshold in
//     [Config.FreezeMilestones] is crossed.
//   - When one or more days are missed but the viewer holds enough
//     freeze credits, freezes are auto-consumed (one per missed day) and
//     [EventTypeStreakFrozen] is emitted; the streak continues.
//   - When missed days exceed freeze credits, [EventTypeStreakBroken] is
//     emitted, the streak resets and a fresh [EventTypeStreakStarted] is
//     emitted in the same batch.
//
// Crossing a milestone (7, 30, 100, 365 days by default) awards freeze credits
// up to [Config.MaxFreezesHeld]. Milestones are recorded in the State so they
// never fire twice for the same streak.
//
// All state transitions are persisted through an
// [github.com/Luca-Pelzer/engelos/internal/eventsourcing.EventStore]. The
// in-memory [ReadModel] is fully derivable from the event log, which makes
// [System.Recover] a pure replay of stored events.
//
// A configurable grace window after UTC midnight ([Config.GraceWindow]) lets
// streamers who go past midnight still count their late-night activity toward
// the previous calendar day. With a 6h grace window, a tick at 03:00 UTC on
// day D counts toward day D-1.
//
// The package owns no I/O outside the event store and no goroutines;
// concurrency is mediated by a single coarse mutex on [System] that protects
// per-viewer transitions.
package streak

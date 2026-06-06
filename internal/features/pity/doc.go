// Package pity implements engelOS's Pity-System: a deterministic, event-sourced
// reward fairness mechanism for chat-driven viewer engagement.
//
// Viewers accumulate Pity Points while chatting. Each [System.Roll] draws against
// an effective win chance that grows with accumulated points:
//
//   - Below the soft-pity threshold (SoftPityFraction × HardPityThreshold) the
//     chance equals [Config.BaseWinChance].
//   - Between soft-pity and hard-pity the chance interpolates linearly toward 1.0.
//   - At hard-pity the chance is exactly 1.0: the next roll is a guaranteed win.
//
// All state transitions emit append-only events through an
// [github.com/Luca-Pelzer/engelos/internal/eventsourcing.EventStore]. The in-memory
// [ReadModel] is fully derivable from the event log, which makes [System.Recover]
// a pure replay of stored events. Production uses crypto/rand; tests inject a
// seeded PCG via [System.WithRng].
//
// The package owns no I/O outside the event store and no goroutines; concurrency
// is mediated by a single coarse mutex on [System] that protects per-viewer
// transitions.
package pity

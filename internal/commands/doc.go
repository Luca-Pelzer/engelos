// Package commands implements engelOS's chat-command engine: a small,
// platform-neutral router that parses prefixed chat messages (e.g. "!pity",
// "!streak", "!commands") and dispatches them to registered handlers.
//
// The engine returns a reply [Reply] to its caller — it never talks to a
// chat platform itself. That decoupling lets the runtime dispatcher own the
// platform-side I/O while this package owns parsing and routing.
//
// # Decoupling
//
// The package does NOT import the feature packages (pity, streak, ...).
// Built-in commands consume narrow read-only querier interfaces declared
// in this package ([PityQuerier], [StreakQuerier]); main.go is responsible
// for wiring small adapters that translate the feature-side Status types
// into the local [PityStatus] / [StreakStatus] shapes. The same philosophy
// is used by [github.com/Luca-Pelzer/engelos/internal/runtime] with its
// PityGranter / StreakTicker / StreakOutcome trio.
//
// # Parsing rules
//
//   - Leading whitespace on the raw message text is trimmed.
//   - The remainder must start with the configured prefix ("!" by default);
//     otherwise the message is NOT a command and [Engine.Handle] returns
//     (Reply{}, false).
//   - The prefix is stripped and the rest is split on whitespace via
//     [strings.Fields]. The first token is the command name (lower-cased
//     for case-insensitive lookup) and the remaining tokens are passed to
//     the handler as args.
//   - A bare prefix ("!") with no command name is treated as not-a-command.
//   - An unknown command (prefixed but no registered name/alias matches) is
//     silently ignored: [Engine.Handle] returns (Reply{}, false) so the
//     dispatcher does NOT spam the chat with "unknown command" replies.
//
// # Concurrency
//
// [Engine.Handle] is safe for concurrent use. [Engine.Register] should
// typically only be called during startup, before the dispatcher begins
// consuming events, but Register and Handle are themselves race-safe via
// an internal RWMutex so late registration does not corrupt the routing
// table.
//
// # Panic recovery
//
// A panic in a handler is recovered, logged, and reported to the caller as
// (Reply{}, true) — i.e. the message WAS a command but produced no reply.
// One bad command can never crash the dispatcher.
package commands

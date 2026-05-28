// Package adapters defines the platform-agnostic abstraction layer that
// isolates the rest of engelOS from the volatility of streaming-platform APIs.
//
// # Why
//
// Twitch ships breaking API changes roughly every two years (PubSub →
// EventSub, IRC → Helix). Discord, YouTube Live, Kick and others follow their
// own cadences. If business logic spoke directly to each platform's wire
// format, every protocol churn would ripple through the entire bot.
//
// The adapter layer pins down two things:
//
//   - [Event] — a normalized, platform-neutral representation of *something
//     that happened* on a connected platform (a message, a sub, a ban, a
//     raid, a connection state change).
//   - [Action] — a normalized representation of *something the bot wants to
//     do* on a platform (send a message, ban a user, delete a message).
//
// Both are stable contracts. Concrete adapters under
// internal/adapters/<platform>/ translate between these contracts and the
// real platform protocol. When Twitch ships EventSub-v3 in 2027, exactly one
// adapter needs rewriting — automod rules, loyalty systems, command engines
// and the web UI never touch platform-specific code.
//
// # Interface
//
// Every concrete adapter implements [Platform]. The interface is deliberately
// minimal: connect, disconnect, expose an Events channel, accept Actions via
// Do. Health-checks are exposed so the supervisor can decide when to
// reconnect.
//
// # Channel semantics
//
// [Platform.Events] returns a receive-only channel. The contract is:
//
//   - Events are delivered in the order the platform reported them.
//   - The channel is closed when (and only when) the platform is fully
//     disconnected and will produce no more events on the current
//     connection. Reconnects are surfaced as [EventReconnecting] /
//     [EventConnected] events on the same channel.
//   - Consumers MUST read promptly; a slow consumer applies backpressure to
//     the adapter, but adapters are free to drop events if their internal
//     buffer overflows. The mock implementation under
//     internal/adapters/mock does *not* drop and is meant for tests.
//
// # Testing
//
// Concrete platform adapters cannot be unit-tested without live credentials
// or extensive HTTP/WebSocket mocking. The in-memory [mock] subpackage
// provides a fully driveable [Platform] implementation that tests can use to
// inject arbitrary [Event] sequences and to assert which [Action] values the
// bot tried to send.
package adapters

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
// # Permissions
//
// Each [Command] carries a MinRole ([Role]) that gates who may invoke it.
// [Message] exposes IsBroadcaster / IsModerator / IsVIP / IsSubscriber
// flags filled by the platform adapter; [Engine.Handle] checks them
// before calling the handler. Roles are ordered RoleEveryone <
// RoleSubscriber < RoleVIP < RoleModerator < RoleBroadcaster, and higher
// roles imply every lower role (a broadcaster passes a sub-only gate, a
// mod passes a VIP-only gate, etc.). The default MinRole zero value is
// [RoleEveryone] so commands remain open by default.
//
// A request that fails the permission gate is silently consumed:
// [Engine.Handle] returns (Reply{}, true). The "handled but empty"
// shape stops the dispatcher from falling through to other handlers
// AND avoids spamming chat with "you lack permission" replies (which
// also leak which commands exist).
//
// # Cooldowns
//
// Each [Command] carries a Cooldown (global, per channel) and a
// UserCooldown (per (command, user)). Either zero disables that
// dimension. Cooldowns are checked AFTER the permission gate and BEFORE
// the handler runs. If EITHER dimension is currently within its window,
// the invocation is silently suppressed: [Engine.Handle] returns
// (Reply{}, true) — same "handled but empty" shape as a denied
// invocation, for the same anti-spam reasons.
//
// Cooldown timers are armed only on a SUCCESSFUL handler return.
// Permission-denied and on-cooldown attempts do NOT reset or extend the
// window — only an actual fire updates the last-fire timestamp. The
// clock used for cooldown bookkeeping is taken from Config.Now (default
// [time.Now]); tests can inject a fake clock to drive cooldown
// transitions deterministically.
//
// Broadcaster and moderator privilege does NOT exempt callers from
// cooldowns at this layer — the behaviour is kept simple and predictable.
// Role-based exemptions can be layered on top of the engine later.
//
// # Concurrency
//
// [Engine.Handle] is safe for concurrent use. [Engine.Register] should
// typically only be called during startup, before the dispatcher begins
// consuming events, but Register and Handle are themselves race-safe via
// an internal RWMutex so late registration does not corrupt the routing
// table. Cooldown bookkeeping uses a separate [sync.Mutex] so the
// registration RWMutex is only held while reading the routing table.
//
// # Panic recovery
//
// A panic in a handler is recovered, logged, and reported to the caller as
// (Reply{}, true) — i.e. the message WAS a command but produced no reply.
// One bad command can never crash the dispatcher.
//
// # Custom commands
//
// Streamer-defined "!hello"-style commands are NOT stored in the engine's
// static registration table; they are looked up at invocation time via
// the optional Config.Resolver. The lookup order is:
//
//  1. Static byName table (always wins on a hit).
//  2. Config.Resolver.Resolve(ctx, channel, name) (when configured).
//  3. Otherwise: not a command, (Reply{}, false).
//
// A resolver hit goes through the SAME permission and cooldown gates as
// a static command; the cooldown maps share the same name keyspace, which
// is safe precisely because the static lookup is checked first (a name
// can never be both static and dynamic for any given Handle call).
//
// The resolved Response is passed through [ExpandVariables] which
// substitutes $user, $channel and $args before the reply is returned.
// Substitution is case-sensitive and non-recursive; unknown $-tokens
// are left untouched. A panicking Resolver is recovered, logged, and
// treated as a miss — never crashes the dispatcher.
//
// Mods manage custom commands through the [NewAddCommand],
// [NewEditCommand] and [NewDeleteCommand] built-ins, which speak to a
// narrow [CustomCommandStore] interface so this package stays
// decoupled from the persistence-side
// [github.com/Luca-Pelzer/engelos/internal/customcommands] package
// (main.go wires the adapter).
package commands

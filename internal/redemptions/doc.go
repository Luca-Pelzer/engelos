// Package redemptions provides a pure-Go SQLite store that maps a Twitch
// Channel-Points Custom-Reward to a bot action - the Firebot-style
// "when reward X is redeemed, do Y" binding table.
//
// A [Store] persists [Binding] rows in pure-Go SQLite (modernc.org/sqlite,
// CGO-free), each scoped to a (tenant, channel). The trigger key is the
// Twitch reward_id (a Custom-Reward UUID); a UNIQUE (tenant_id, channel,
// reward_id) constraint enforces exactly one binding per reward so the
// future executor can resolve a redemption to a single action via
// [Store.GetByReward]. Concurrent Creates of the same reward collapse to
// one winner ([ErrConflict] for the losers); the [Store] serialises the
// check-then-insert with a process mutex on top of the constraint.
//
// This package is the persistence layer ONLY: it does not talk to Twitch,
// open WebSockets, or execute actions, and it imports nothing under
// engelos/internal. Later steps (an EventSub WebSocket client, reward CRUD
// in the twitch adapter, and an action executor) plug in through the
// [Store] interface.
package redemptions

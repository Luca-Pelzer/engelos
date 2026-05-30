// Package channelpoints is the executor that turns a Twitch Channel-Points
// redemption into a bot action. It glues together the three leaf pieces of
// the integration: the reward->action binding store
// ([github.com/Luca-Pelzer/engelos/internal/redemptions]), the EventSub
// WebSocket client's neutral
// [github.com/Luca-Pelzer/engelos/internal/adapters/twitch/eventsub.RedemptionEvent],
// and a redemption [Fulfiller].
//
// An [Executor] receives one [eventsub.RedemptionEvent] per redemption via
// [Executor.Handle], looks up the binding for the redeemed reward, runs the
// bound action (chat message, counter increment/reset, or none), and — when
// the binding opts into auto-fulfilment — marks the redemption FULFILLED on
// success or CANCELED (refund) on failure.
//
// To stay decoupled and testable this package depends only on the narrow
// capability interfaces declared here ([ChatSender], [CounterAdmin],
// [Fulfiller], [BindingStore]); the concrete *twitch.Adapter and platform
// sender are injected by main. It deliberately does NOT import
// engelos/internal/adapters/twitch.
package channelpoints

// Package twitch implements the Twitch platform adapter for engelOS.
//
// The adapter speaks the Twitch IRC protocol (via gempir/go-twitch-irc) for
// receiving chat events and joining channels, and the Twitch Helix REST API
// (via nicklaw5/helix) for moderation actions such as bans, timeouts and
// message deletions.
//
// EventSub WebSocket is intentionally not implemented in this package; if
// future engelOS features need stream-online, follow or channel-point events
// they should be added behind a separate sub-package or this adapter
// extended in a follow-up.
//
// Two operating modes are supported:
//
//   - Anonymous mode (no OAuthToken): the adapter logs in as a justinfan
//     guest user. Chat events are received but every [adapters.Action] is
//     rejected with an error because anonymous accounts cannot send.
//   - Authenticated mode (OAuthToken + ClientID): the adapter joins as the
//     configured bot user. [adapters.ActionSendMessage] uses IRC PRIVMSG and
//     the moderation actions use the Helix API.
//
// The adapter is safe for concurrent use: every mutation of internal state
// is guarded by a mutex and all helix/IRC calls are issued from goroutines
// that never block the IRC reader.
package twitch

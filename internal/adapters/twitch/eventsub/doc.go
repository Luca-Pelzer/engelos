// Package eventsub implements a pure transport client for Twitch's EventSub
// WebSocket transport (wss://eventsub.wss.twitch.tv/ws).
//
// The client connects, performs the session handshake (waiting for the
// session_welcome message), and processes the EventSub message stream:
// session_keepalive, notification, session_reconnect, and revocation. It
// decodes Channel-Points custom-reward redemption notifications into the
// neutral [RedemptionEvent] struct and hands them to a caller-supplied
// handler.
//
// Scope and boundaries:
//
//   - It does NOT create EventSub subscriptions. Subscriptions require the
//     Helix API plus the session id; the caller performs that work inside the
//     OnSession callback (see [Config.OnSession]), keeping this package free of
//     any Helix dependency.
//   - It does NOT execute actions or persist anything. It is a one-way pipe
//     from Twitch's socket to the handler.
//   - It imports ONLY the standard library and github.com/coder/websocket.
//
// Reconnect semantics follow the Twitch EventSub WebSocket documentation:
//
//   - A dropped connection loses all subscriptions, so [Client.Run] redials
//     from scratch (re-invoking OnSession to resubscribe) using exponential
//     backoff.
//   - A session_reconnect message is a graceful, in-place handoff: the new
//     connection is established and its welcome received BEFORE the old
//     connection is closed, and OnSession is NOT called again because Twitch
//     carries the existing subscriptions over to the new session.
package eventsub

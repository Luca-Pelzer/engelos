// Package kick implements the Kick.com platform adapter for engelOS.
//
// Kick is a hybrid adapter and differs from the other platforms in this
// repository. Twitch, Discord and YouTube are outbound clients that dial a
// connection and pull events; Kick has no official WebSocket or IRC surface
// for chat. The only supported event delivery is inbound webhooks: Kick POSTs
// chat, subscription and moderation events to a public HTTPS endpoint that the
// bot exposes. The send and moderation half is ordinary outbound REST against
// https://api.kick.com/public/v1, which maps cleanly onto Do.
//
// Because the receive path is inverted, Connect does not dial. It validates
// the configuration, prepares the events channel, and starts a background
// goroutine that waits for the HTTP server to be ready before registering the
// webhook subscriptions with Kick. The actual ingestion happens in
// WebhookHandler, an http.HandlerFunc the API router mounts outside its
// session-gated group: the only gate is RSA-SHA256 signature verification plus
// replay protection (timestamp window and a message-id dedup cache).
//
// Two OAuth credentials drive the adapter. An App access token
// (client_credentials) registers the event subscriptions; a User access token
// (authorization-code with PKCE) authorizes send and moderation actions. Both
// are supplied via Config and may be static tokens or refreshing TokenSources.
//
// The adapter is safe for concurrent use: state mutation is guarded by a mutex
// and the webhook handler never blocks on a slow events consumer, so a full
// channel drops the event rather than stalling Kick (which would unsubscribe a
// slow or failing endpoint).
package kick

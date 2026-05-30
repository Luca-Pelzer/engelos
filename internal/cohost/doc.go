// Package cohost implements an opt-in AI co-host: the bot answers chat
// questions that address it, using a Claude backend over the shared
// subscription proxy.
//
// # Two pieces
//
// A per-channel SQLite [Store] (mirroring internal/translate) holds the
// opt-in switch, the bot name viewers address, a persona style instruction and
// a reply-length cap. A stateless [Responder] decides whether a message
// addresses the bot, asks the injected [Backend] for a reply, and applies
// per-user and global rate limits plus the length cap.
//
// # Addressing
//
// A message addresses the co-host when it starts with "!ask " or with the
// configured bot name (optionally prefixed by '@' and followed by a separator),
// matched case-insensitively. The addressing prefix is stripped before the
// question reaches the backend. Everything else is ignored, so the co-host
// never speaks unprompted.
//
// # Decoupling
//
// The package performs no HTTP and owns no credentials: the Claude client is
// injected as a [Backend]. It imports nothing under engelos/internal, so it can
// evolve independently of the rest of the bot. Answering is best-effort: a
// backend error is returned for logging but must never block message handling.
package cohost

// Package claude is a thin, pure-HTTP client for translating short chat
// messages with Anthropic's Claude models.
//
// # Bring your own key
//
// The client targets Anthropic's public API by default (see [DefaultBaseURL]).
// Supply your own Anthropic API key with [WithAPIKey] (wired from the
// ENGELOS_ANTHROPIC_API_KEY env var in main); the key is sent as the x-api-key
// header. Point the client at any Anthropic-compatible endpoint with
// [WithBaseURL], for example a local proxy or an httptest server in tests. The
// request shape is plain Anthropic /v1/messages JSON either way.
//
// # Translation prompt
//
// [Client.Translate] builds an output-only translation prompt: a system
// instruction that tells the model to emit ONLY the translated text (no
// preamble, no quotes) and to pass the input through unchanged when it is
// already in the target language. temperature is pinned to 0 for deterministic,
// cache-friendly output.
//
// # Errors
//
// Non-2xx responses map onto sentinel errors comparable with [errors.Is]:
// [ErrUnauthorized] (401, a missing or invalid API key) and [ErrAPI] for any
// other non-2xx, wrapping the upstream error message when present. The client
// imports nothing under engelos/internal and depends only on the Go standard
// library, so it can evolve independently of the rest of the bot.
package claude

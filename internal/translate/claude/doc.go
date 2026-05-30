// Package claude is a thin, pure-HTTP client for translating short chat
// messages with Anthropic's Claude models.
//
// # Talks to a proxy, not the API directly
//
// The client targets a configurable base URL that defaults to the local
// Anthropic OAuth proxy (see [DefaultBaseURL]). That proxy owns all of the
// credential handling: it reads the shared Claude subscription token, refreshes
// it, injects the "You are Claude Code" system identity and the required
// anthropic-beta / anthropic-version headers, and forwards the request to
// api.anthropic.com. Pointing engelOS at the proxy means the bot consumes the
// streamer's existing Claude subscription at no per-token cost and never has to
// reimplement (or race with) the token-refresh logic the proxy already centralises.
//
// Self-hosted deployments that prefer their own credentials can point the
// client at any Anthropic-compatible endpoint via [WithBaseURL] and supply a
// key with [WithAPIKey]; the request shape is plain Anthropic /v1/messages JSON
// either way.
//
// # No credentials by default
//
// With no API key the client sends no x-api-key header, which is exactly what
// the proxy expects (it validates nothing and supplies the OAuth bearer itself).
// [WithAPIKey] is provided for the bring-your-own-key path and, when set, sends
// the value as the x-api-key header.
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
// [ErrUnauthorized] (401, the proxy token is stale or the supplied key is bad)
// and [ErrAPI] for any other non-2xx, wrapping the upstream error message when
// present. The client imports nothing under engelos/internal and depends only
// on the Go standard library, so it can evolve independently of the rest of the
// bot.
package claude

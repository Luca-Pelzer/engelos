// Package openai is a thin, pure-HTTP client for single-turn completions
// against any OpenAI-compatible chat-completions endpoint.
//
// It is a structural mirror of the anthropic client (internal/aibackend/anthropic)
// and satisfies the same backend interfaces, so it is a 1:1 drop-in behind the
// co-host, clipper/titler and translator consumers. The difference is the wire
// format: OpenAI /v1/chat/completions, with the system prompt carried as the
// first message (role "system") and the reply read from
// choices[0].message.content.
//
// # Bring your own key
//
// The client targets OpenAI's public API by default (see [DefaultBaseURL]).
// Supply your own API key with [WithAPIKey]; the key is sent as the
// Authorization: Bearer header. Point the client at any OpenAI-compatible
// endpoint with [WithBaseURL], for example a local model runner, a compatible
// proxy or an httptest server in tests. The client is provider-neutral: the
// concrete deployment endpoint is supplied at wiring time via configuration,
// never hardcoded beyond the public default.
//
// # Completions and translation
//
// [Client.Complete] sends a system prompt plus a single user message and
// returns the model's text reply. [Client.Translate] is a thin wrapper that
// builds an output-only translation prompt (emit ONLY the translated text, pass
// the input through unchanged when it is already in the target language) and
// delegates to Complete. temperature is pinned to 0 for deterministic,
// cache-friendly output.
//
// # Errors
//
// Non-2xx responses map onto sentinel errors comparable with [errors.Is]:
// [ErrUnauthorized] (401, a missing or invalid API key) and [ErrAPI] for any
// other non-2xx, wrapping the upstream error message when present. These mirror
// the Claude client's sentinels so consumers that fail open on any error keep
// working unchanged. The client depends only on the Go standard library and the
// small internal/aibackend/usage sink (used to report per-response token
// counts), so it can otherwise evolve independently of the rest of the bot.
package openai

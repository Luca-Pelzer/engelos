// Package anthropic is a thin, pure-HTTP client for single-turn completions and
// short-text translation against Anthropic's Messages API.
//
// It is the Anthropic provider behind the aibackend selector and a structural
// mirror of the openai client (internal/aibackend/openai): both satisfy the
// same backend interfaces (Complete and Translate), so they are 1:1
// interchangeable behind the context-moderation, co-host, clipper/titler and
// translator consumers. The difference is the wire format: Anthropic
// /v1/messages, with the system prompt carried in the top-level "system" field
// and the reply read from the content text blocks.
//
// # Bring your own key
//
// The client targets Anthropic's public API by default (see [DefaultBaseURL]).
// Supply your own API key with [WithAPIKey]; the key is sent as the x-api-key
// header. Point the client at any Anthropic-compatible endpoint with
// [WithBaseURL], for example a compatible proxy or an httptest server in tests.
// The client is provider-neutral: the concrete deployment endpoint is supplied
// at wiring time via configuration, never hardcoded beyond the public default.
//
// # Completions and translation
//
// [Client.Complete] sends a system prompt plus a single user message and
// returns the model's text reply. [Client.Translate] is a thin wrapper that
// builds an output-only translation prompt (emit ONLY the translated text, pass
// the input through unchanged when it is already in the target language) and
// delegates to the same request path. temperature is pinned to 0 for
// deterministic, cache-friendly output.
//
// # Prompt caching
//
// By default the client marks the static system-prompt prefix with
// cache_control: ephemeral (sent as a one-element content-block array) so
// Anthropic can serve the repeated prefix from its prompt cache. Disable it with
// [WithPromptCaching](false), in which case the system prompt is sent as a plain
// string exactly as before. If a server rejects cache_control with a 4xx (an
// older proxy), the client retries the request once without it and disables
// caching for the rest of the process.
//
// # Errors
//
// Non-2xx responses map onto sentinel errors comparable with [errors.Is]:
// [ErrUnauthorized] (401, a missing or invalid API key) and [ErrAPI] for any
// other non-2xx, wrapping the upstream error message when present. These mirror
// the openai client's sentinels so consumers that fail open on any error keep
// working unchanged. The client depends only on the Go standard library and the
// small internal/aibackend/usage sink (used to report per-response token
// counts), so it can otherwise evolve independently of the rest of the bot.
package anthropic

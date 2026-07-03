// Package aibackend selects and constructs the AI completion backend shared by
// every AI feature in engelOS: context-moderation escalation, chat translation,
// the AI co-host and clip titling. It exposes one provider-neutral [Backend]
// interface and a [New] selector so main wires a single value into all four
// consumers, and swapping providers is an env change rather than a code change.
//
// # Providers
//
// Two providers ship today, each a thin pure-HTTP client under this package:
// [anthropic] (Anthropic /v1/messages, the default) and [openai]
// (OpenAI-compatible /v1/chat/completions). Both satisfy [Backend], proven by
// the compile-time assertions below.
//
// # Environment resolution
//
// [LoadConfig] reads the configuration main passes to [New]. Neutral,
// provider-agnostic variables are read first:
//
//	ENGELOS_AI_PROVIDER   anthropic (default) | openai
//	ENGELOS_AI_BASE_URL   endpoint root override
//	ENGELOS_AI_API_KEY    provider API key
//	ENGELOS_AI_MODEL      model id override
//
// For backward compatibility each of base URL / API key / model falls back to a
// legacy variable ONLY when its neutral form is unset:
//
//	ENGELOS_AI_BASE_URL -> ENGELOS_TRANSLATE_BASE_URL
//	ENGELOS_AI_API_KEY  -> ENGELOS_ANTHROPIC_API_KEY
//	ENGELOS_AI_MODEL    -> ENGELOS_TRANSLATE_MODEL
//
// # Unknown providers
//
// An empty provider selects Anthropic. An unrecognised provider also selects
// Anthropic, after logging a warning, so a typo degrades to the safe default
// (which fails open on any error) instead of crashing the daemon.
package aibackend

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/aibackend/anthropic"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/openai"
)

// Backend is the provider-neutral surface every AI feature depends on. The
// anthropic and openai clients both satisfy it, so a single value from [New] is
// shared across the context-moderation escalator, the translator, the co-host
// responder and the clip titler; each consumer keeps its own narrower interface
// and only ever checks err != nil, so any backend error fails open.
type Backend interface {
	Complete(ctx context.Context, systemPrompt, userText string) (string, error)
	Translate(ctx context.Context, text, targetLang string) (string, error)
}

// Provider identifiers accepted in ENGELOS_AI_PROVIDER (case-insensitive).
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
)

// Both providers must satisfy Backend; this fails the build if a signature
// drifts.
var (
	_ Backend = (*anthropic.Client)(nil)
	_ Backend = (*openai.Client)(nil)
)

// Config selects and configures the backend. The zero value is valid and yields
// the default provider (Anthropic) on its public default endpoint with no key.
// BaseURL, APIKey and Model are provider-neutral: each is forwarded to whichever
// provider Provider names, and an empty field keeps that provider's own default.
type Config struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
}

// LoadConfig reads the backend configuration from the environment, preferring
// the neutral ENGELOS_AI_* variables and falling back to the legacy names only
// when the neutral form is unset. See the package doc for the full variable map.
func LoadConfig() Config {
	return Config{
		Provider: env("ENGELOS_AI_PROVIDER"),
		BaseURL:  firstNonEmpty(env("ENGELOS_AI_BASE_URL"), env("ENGELOS_TRANSLATE_BASE_URL")),
		APIKey:   firstNonEmpty(env("ENGELOS_AI_API_KEY"), env("ENGELOS_ANTHROPIC_API_KEY")),
		Model:    firstNonEmpty(env("ENGELOS_AI_MODEL"), env("ENGELOS_TRANSLATE_MODEL")),
	}
}

// New constructs the [Backend] selected by cfg. An empty or unrecognised
// Provider selects Anthropic (the latter after a warning), so an operator typo
// degrades to the safe default rather than crashing. A nil logger defaults to
// [slog.Default]. Empty BaseURL/APIKey/Model keep the chosen provider's own
// defaults.
func New(cfg Config, logger *slog.Logger) Backend {
	if logger == nil {
		logger = slog.Default()
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case ProviderOpenAI:
		return openai.New(
			openai.WithLogger(logger),
			openai.WithBaseURL(cfg.BaseURL),
			openai.WithAPIKey(cfg.APIKey),
			openai.WithModel(cfg.Model),
		)
	case "", ProviderAnthropic:
		// Explicit or default Anthropic: fall through to the shared build below.
	default:
		logger.Warn("aibackend: unknown ENGELOS_AI_PROVIDER, falling back to anthropic",
			"provider", cfg.Provider, "supported", []string{ProviderAnthropic, ProviderOpenAI})
	}
	return anthropic.New(
		anthropic.WithLogger(logger),
		anthropic.WithBaseURL(cfg.BaseURL),
		anthropic.WithAPIKey(cfg.APIKey),
		anthropic.WithModel(cfg.Model),
	)
}

// env reads and trims an environment variable.
func env(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

// firstNonEmpty returns the first non-empty argument, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

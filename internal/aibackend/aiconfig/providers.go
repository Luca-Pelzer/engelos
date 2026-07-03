package aiconfig

import (
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/aibackend"
)

// Auth kinds a provider preset requires.
const (
	AuthAPIKey = "api_key"
	AuthNone   = "none"
)

// Catalog provider ids. anthropic and openai map 1:1 onto the selector's own
// providers; groq and ollama are OpenAI-wire presets; custom is a UI affordance
// for an arbitrary endpoint on either wire.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderGroq      = "groq"
	ProviderOllama    = "ollama"
	ProviderCustom    = "custom"
)

// Preset base URLs for the OpenAI-wire presets.
const (
	groqBaseURL   = "https://api.groq.com/openai"
	ollamaBaseURL = "http://localhost:11434"
)

// ProviderInfo is one entry in the static provider catalog the dashboard UI
// renders to build an AI configuration. It is presentation metadata only; the
// runtime selector understands the concrete wire (see [ProviderInfo.Wire]).
type ProviderInfo struct {
	// ID is the catalog identifier (for example "anthropic", "groq").
	ID string `json:"id"`
	// Label is the human-friendly display name.
	Label string `json:"label"`
	// Auth is the credential kind the preset needs: "api_key" or "none".
	Auth string `json:"auth"`
	// Wire is the concrete request format the selector uses: "anthropic" or
	// "openai". Empty for "custom", where the operator selects it (see
	// WireOptions).
	Wire string `json:"wire,omitempty"`
	// WireOptions lists the selectable wires for a "custom" endpoint.
	WireOptions []string `json:"wire_options,omitempty"`
	// DefaultModel pre-fills the model field; empty when the operator must
	// choose (Groq, Ollama, custom).
	DefaultModel string `json:"default_model,omitempty"`
	// DefaultBaseURL pre-fills the endpoint; empty means the wire client's own
	// public default.
	DefaultBaseURL string `json:"default_base_url,omitempty"`
	// Note is a short UI hint, for example that an Ollama model must be pulled
	// locally first.
	Note string `json:"note,omitempty"`
}

// Providers returns a fresh copy of the static provider catalog. Callers may
// mutate the returned slice without affecting the canonical catalog.
func Providers() []ProviderInfo {
	return []ProviderInfo{
		{
			ID:             ProviderAnthropic,
			Label:          "Anthropic",
			Auth:           AuthAPIKey,
			Wire:           aibackend.ProviderAnthropic,
			DefaultModel:   "claude-haiku-4-5",
			DefaultBaseURL: "https://api.anthropic.com",
		},
		{
			ID:             ProviderOpenAI,
			Label:          "OpenAI",
			Auth:           AuthAPIKey,
			Wire:           aibackend.ProviderOpenAI,
			DefaultModel:   "gpt-4o-mini",
			DefaultBaseURL: "https://api.openai.com",
		},
		{
			ID:             ProviderGroq,
			Label:          "Groq",
			Auth:           AuthAPIKey,
			Wire:           aibackend.ProviderOpenAI,
			DefaultBaseURL: groqBaseURL,
			Note:           "OpenAI-compatible. Choose a Groq-hosted model (for example llama-3.1-8b-instant).",
		},
		{
			ID:             ProviderOllama,
			Label:          "Ollama (local)",
			Auth:           AuthNone,
			Wire:           aibackend.ProviderOpenAI,
			DefaultBaseURL: ollamaBaseURL,
			Note:           "Local runner. The model must be pulled locally first (for example: ollama pull llama3.1).",
		},
		{
			ID:          ProviderCustom,
			Label:       "Custom endpoint",
			Auth:        AuthAPIKey,
			WireOptions: []string{aibackend.ProviderOpenAI, aibackend.ProviderAnthropic},
			Note:        "Point at any OpenAI- or Anthropic-compatible endpoint and select the matching wire.",
		},
	}
}

// wireFor maps a stored/submitted provider id onto the concrete wire provider
// the [aibackend] selector understands, plus the preset default base URL for
// that id. anthropic, openai, custom and any unknown id pass through unchanged
// (the selector defaults unknown providers to anthropic); groq and ollama are
// rewritten to the OpenAI wire with their preset base URL.
func wireFor(provider string) (wire, baseURL string) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ProviderGroq:
		return aibackend.ProviderOpenAI, groqBaseURL
	case ProviderOllama:
		return aibackend.ProviderOpenAI, ollamaBaseURL
	default:
		return provider, ""
	}
}

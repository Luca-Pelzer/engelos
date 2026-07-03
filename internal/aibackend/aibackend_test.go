package aibackend

import (
	"io"
	"log/slog"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/aibackend/anthropic"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/openai"
	"github.com/stretchr/testify/assert"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNew_DefaultProviderIsAnthropic(t *testing.T) {
	b := New(Config{}, quietLogger())
	_, ok := b.(*anthropic.Client)
	assert.True(t, ok, "empty provider should select the anthropic client")
}

func TestNew_ExplicitAnthropicProvider(t *testing.T) {
	b := New(Config{Provider: "anthropic"}, quietLogger())
	_, ok := b.(*anthropic.Client)
	assert.True(t, ok)
}

func TestNew_OpenAIProvider(t *testing.T) {
	b := New(Config{Provider: "openai"}, quietLogger())
	_, ok := b.(*openai.Client)
	assert.True(t, ok, "provider openai should select the openai client")
}

func TestNew_ProviderIsCaseAndSpaceInsensitive(t *testing.T) {
	b := New(Config{Provider: "  OpenAI  "}, quietLogger())
	_, ok := b.(*openai.Client)
	assert.True(t, ok, "provider matching ignores case and surrounding space")
}

func TestNew_UnknownProviderFallsBackToAnthropic(t *testing.T) {
	b := New(Config{Provider: "gemini"}, quietLogger())
	_, ok := b.(*anthropic.Client)
	assert.True(t, ok, "an unknown provider must degrade to the anthropic default")
}

func TestNew_NilLoggerIsSafe(t *testing.T) {
	assert.NotPanics(t, func() { _ = New(Config{Provider: "unknown-so-it-warns"}, nil) })
}

func TestLoadConfig_NeutralVarsWinOverLegacy(t *testing.T) {
	t.Setenv("ENGELOS_AI_PROVIDER", "openai")
	t.Setenv("ENGELOS_AI_BASE_URL", "https://neutral.example")
	t.Setenv("ENGELOS_TRANSLATE_BASE_URL", "https://legacy.example")
	t.Setenv("ENGELOS_AI_API_KEY", "neutral-key")
	t.Setenv("ENGELOS_ANTHROPIC_API_KEY", "legacy-key")
	t.Setenv("ENGELOS_AI_MODEL", "neutral-model")
	t.Setenv("ENGELOS_TRANSLATE_MODEL", "legacy-model")

	cfg := LoadConfig()
	assert.Equal(t, "openai", cfg.Provider)
	assert.Equal(t, "https://neutral.example", cfg.BaseURL)
	assert.Equal(t, "neutral-key", cfg.APIKey)
	assert.Equal(t, "neutral-model", cfg.Model)
}

func TestLoadConfig_LegacyFallbackWhenNeutralUnset(t *testing.T) {
	// Neutral vars explicitly empty so only the legacy names carry a value.
	t.Setenv("ENGELOS_AI_BASE_URL", "")
	t.Setenv("ENGELOS_AI_API_KEY", "")
	t.Setenv("ENGELOS_AI_MODEL", "")
	t.Setenv("ENGELOS_TRANSLATE_BASE_URL", "https://legacy.example")
	t.Setenv("ENGELOS_ANTHROPIC_API_KEY", "legacy-key")
	t.Setenv("ENGELOS_TRANSLATE_MODEL", "legacy-model")

	cfg := LoadConfig()
	assert.Equal(t, "https://legacy.example", cfg.BaseURL)
	assert.Equal(t, "legacy-key", cfg.APIKey)
	assert.Equal(t, "legacy-model", cfg.Model)
}

func TestLoadConfig_TrimsWhitespace(t *testing.T) {
	t.Setenv("ENGELOS_AI_PROVIDER", "  openai  ")
	t.Setenv("ENGELOS_AI_BASE_URL", "  https://trim.example  ")

	cfg := LoadConfig()
	assert.Equal(t, "openai", cfg.Provider)
	assert.Equal(t, "https://trim.example", cfg.BaseURL)
}

func TestLoadConfig_ThenNewSelectsConfiguredProvider(t *testing.T) {
	t.Setenv("ENGELOS_AI_PROVIDER", "openai")

	b := New(LoadConfig(), quietLogger())
	_, ok := b.(*openai.Client)
	assert.True(t, ok, "LoadConfig -> New should honour ENGELOS_AI_PROVIDER end to end")
}

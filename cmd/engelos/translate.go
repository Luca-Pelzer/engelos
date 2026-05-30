package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/translate"
	"github.com/Luca-Pelzer/engelos/internal/translate/claude"
)

// translateConfigAdapter adapts translate.Store to commands.TranslateConfigStore
// so the mods-only !translate command can read and change the per-channel
// translation settings without internal/commands importing internal/translate.
// It owns the tenant id and lets the store normalise the channel.
type translateConfigAdapter struct {
	store    translate.Store
	tenantID string
	logger   *slog.Logger
}

func (a translateConfigAdapter) TranslateStatus(ctx context.Context, channel string) (bool, string) {
	cfg, err := a.store.GetOrDefault(ctx, a.tenantID, channel)
	if err != nil {
		a.logger.WarnContext(ctx, "translate: status read failed", "channel", channel, "err", err)
		return false, "en"
	}
	return cfg.Enabled, cfg.TargetLang
}

func (a translateConfigAdapter) SetTranslateEnabled(ctx context.Context, channel string, enabled bool) error {
	cfg, err := a.store.GetOrDefault(ctx, a.tenantID, channel)
	if err != nil {
		return err
	}
	cfg.Enabled = enabled
	_, err = a.store.Set(ctx, cfg)
	return err
}

func (a translateConfigAdapter) SetTranslateLang(ctx context.Context, channel, lang string) error {
	cfg, err := a.store.GetOrDefault(ctx, a.tenantID, channel)
	if err != nil {
		return err
	}
	cfg.TargetLang = lang
	_, err = a.store.Set(ctx, cfg)
	return err
}

// messageTranslator adapts the per-channel config store plus a shared
// translate.Translator orchestrator to runtime.MessageTranslator. For each
// message it looks up the channel config; when translation is disabled it
// returns ok=false so the dispatcher posts nothing. The orchestrator owns the
// skip heuristics, language detection, caching and rate limiting; this adapter
// only resolves per-channel target language and gate.
type messageTranslator struct {
	store    translate.Store
	tr       *translate.Translator
	tenantID string
	logger   *slog.Logger
}

func (m messageTranslator) Maybe(ctx context.Context, channel, userID, text string) (string, bool) {
	cfg, err := m.store.GetOrDefault(ctx, m.tenantID, channel)
	if err != nil {
		m.logger.WarnContext(ctx, "translate: config read failed", "channel", channel, "err", err)
		return "", false
	}
	if !cfg.Enabled {
		return "", false
	}
	res, err := m.tr.Translate(ctx, userID, text, cfg.TargetLang)
	if err != nil {
		m.logger.WarnContext(ctx, "translate: backend failed", "channel", channel, "err", err)
		return "", false
	}
	if res.Skipped || res.Translated == "" {
		return "", false
	}
	return res.Translated, true
}

// newMessageTranslator builds the dispatcher-facing translator. The Claude
// backend targets the local Anthropic OAuth proxy by default (the shared
// subscription, no per-token cost) and can be repointed with
// ENGELOS_TRANSLATE_BASE_URL; ENGELOS_TRANSLATE_API_KEY enables a
// bring-your-own-key endpoint instead, and ENGELOS_TRANSLATE_MODEL overrides
// the model id.
func newMessageTranslator(store translate.Store, tenantID string, logger *slog.Logger) messageTranslator {
	opts := []claude.Option{claude.WithLogger(logger)}
	if base := strings.TrimSpace(os.Getenv("ENGELOS_TRANSLATE_BASE_URL")); base != "" {
		opts = append(opts, claude.WithBaseURL(base))
	}
	if key := strings.TrimSpace(os.Getenv("ENGELOS_TRANSLATE_API_KEY")); key != "" {
		opts = append(opts, claude.WithAPIKey(key))
	}
	if model := strings.TrimSpace(os.Getenv("ENGELOS_TRANSLATE_MODEL")); model != "" {
		opts = append(opts, claude.WithModel(model))
	}
	backend := claude.New(opts...)
	tr := translate.New(backend, translate.DefaultOptions())
	return messageTranslator{store: store, tr: tr, tenantID: tenantID, logger: logger}
}

// compile-time interface checks.
var (
	_ commands.TranslateConfigStore = translateConfigAdapter{}
)

package main

import (
	"context"
	"log/slog"

	"github.com/Luca-Pelzer/engelos/internal/cohost"
	"github.com/Luca-Pelzer/engelos/internal/commands"
)

// cohostConfigAdapter adapts cohost.Store to commands.CoHostConfigStore so the
// mods-only !cohost command can read and change the per-channel co-host
// settings without internal/commands importing internal/cohost. It owns the
// tenant id and lets the store normalise the channel.
type cohostConfigAdapter struct {
	store    cohost.Store
	tenantID string
	logger   *slog.Logger
}

func (a cohostConfigAdapter) CoHostStatus(ctx context.Context, channel string) (bool, string) {
	cfg, err := a.store.GetOrDefault(ctx, a.tenantID, channel)
	if err != nil {
		a.logger.WarnContext(ctx, "cohost: status read failed", "channel", channel, "err", err)
		return false, "bot"
	}
	return cfg.Enabled, cfg.BotName
}

func (a cohostConfigAdapter) SetCoHostEnabled(ctx context.Context, channel string, enabled bool) error {
	cfg, err := a.store.GetOrDefault(ctx, a.tenantID, channel)
	if err != nil {
		return err
	}
	cfg.Enabled = enabled
	_, err = a.store.Set(ctx, cfg)
	return err
}

func (a cohostConfigAdapter) SetCoHostName(ctx context.Context, channel, name string) error {
	cfg, err := a.store.GetOrDefault(ctx, a.tenantID, channel)
	if err != nil {
		return err
	}
	cfg.BotName = name
	_, err = a.store.Set(ctx, cfg)
	return err
}

func (a cohostConfigAdapter) SetCoHostPersona(ctx context.Context, channel, persona string) error {
	cfg, err := a.store.GetOrDefault(ctx, a.tenantID, channel)
	if err != nil {
		return err
	}
	cfg.Persona = persona
	_, err = a.store.Set(ctx, cfg)
	return err
}

// coHostResponder adapts the per-channel config store plus a shared
// cohost.Responder to runtime.CoHost. For each message it looks up the channel
// config; when the co-host is disabled it returns ok=false so the dispatcher
// posts nothing. The responder owns the addressing detection, rate limiting and
// reply-length cap.
type coHostResponder struct {
	store    cohost.Store
	resp     *cohost.Responder
	tenantID string
	logger   *slog.Logger
}

func (m coHostResponder) Maybe(ctx context.Context, channel, userID, username, text string) (string, bool) {
	cfg, err := m.store.GetOrDefault(ctx, m.tenantID, channel)
	if err != nil {
		m.logger.WarnContext(ctx, "cohost: config read failed", "channel", channel, "err", err)
		return "", false
	}
	if !cfg.Enabled {
		return "", false
	}
	reply, answered, err := m.resp.Respond(ctx, cfg, userID, username, text)
	if err != nil {
		m.logger.WarnContext(ctx, "cohost: backend failed", "channel", channel, "err", err)
		return "", false
	}
	if !answered {
		return "", false
	}
	return reply, true
}

// newCoHostResponder builds the dispatcher-facing co-host around a Claude
// backend (the same proxy-backed client used for translation).
func newCoHostResponder(store cohost.Store, backend cohost.Backend, tenantID string, logger *slog.Logger) coHostResponder {
	return coHostResponder{
		store:    store,
		resp:     cohost.NewResponder(backend, cohost.DefaultOptions()),
		tenantID: tenantID,
		logger:   logger,
	}
}

// compile-time interface check.
var _ commands.CoHostConfigStore = cohostConfigAdapter{}

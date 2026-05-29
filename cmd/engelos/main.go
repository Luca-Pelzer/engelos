// Package main is the entry point of the engelOS daemon.
//
// engelOS is an open-source streaming bot. This binary starts the core daemon,
// which connects to streaming platforms (Twitch, Discord, YouTube, Kick) and
// exposes an HTTP/WebSocket API on :8080 for the TUI, web dashboard, and
// native GUI to talk to.
//
// License: AGPL-3.0 (see LICENSE in repo root).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
	"github.com/Luca-Pelzer/engelos/internal/adapters/discord"
	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch"
	"github.com/Luca-Pelzer/engelos/internal/api"
	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/api/ws"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
	"github.com/Luca-Pelzer/engelos/internal/server"
	"github.com/Luca-Pelzer/engelos/internal/web"
)

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "0.0.0-dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("engelOS starting",
		"version", Version,
		"phase", "1B — adapters + auth + web + dispatcher",
	)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGTERM,
	)
	defer cancel()

	if err := run(ctx, logger); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
	slog.Info("engelOS stopped cleanly")
}

// defaultTenantID is the single-tenant identifier used by the OSS daemon.
// Multi-tenant clouds override this via configuration in a later phase.
const defaultTenantID = "default"

func run(ctx context.Context, logger *slog.Logger) error {
	dataDir, err := dataDirectory()
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	logger.Info("data directory ready", "path", dataDir)

	authDSN := filepath.Join(dataDir, "auth.db")
	authStore, err := auth.OpenSQLiteStore(ctx, authDSN, logger)
	if err != nil {
		return fmt.Errorf("open auth store %s: %w", authDSN, err)
	}
	defer func() {
		if cerr := authStore.Close(); cerr != nil {
			logger.Warn("auth store close failed", "err", cerr)
		}
	}()
	logger.Info("auth store opened", "dsn", authDSN)

	eventsDSN := filepath.Join(dataDir, "events.db")
	eventStore, err := eventsourcing.OpenSQLite(ctx, eventsDSN)
	if err != nil {
		return fmt.Errorf("open event store %s: %w", eventsDSN, err)
	}
	defer func() {
		if cerr := eventStore.Close(); cerr != nil {
			logger.Warn("event store close failed", "err", cerr)
		}
	}()
	logger.Info("event store opened", "dsn", eventsDSN)

	pitySystem, err := pity.New(pity.DefaultConfig(), eventStore, logger)
	if err != nil {
		return fmt.Errorf("init pity system: %w", err)
	}
	if err := pitySystem.Recover(ctx, defaultTenantID); err != nil {
		return fmt.Errorf("recover pity read model: %w", err)
	}
	logger.Info("pity system ready",
		"hard_pity_threshold", pitySystem.Config().HardPityThreshold,
		"soft_pity_fraction", pitySystem.Config().SoftPityFraction,
	)

	streakSystem, err := streak.New(streak.DefaultConfig(), eventStore, logger)
	if err != nil {
		return fmt.Errorf("init streak system: %w", err)
	}
	if err := streakSystem.Recover(ctx, defaultTenantID); err != nil {
		return fmt.Errorf("recover streak read model: %w", err)
	}
	logger.Info("streak system ready",
		"max_freezes_held", streakSystem.Config().MaxFreezesHeld,
		"grace_window", streakSystem.Config().GraceWindow,
	)

	hub := ws.NewHub(logger)
	go hub.Run(ctx)

	platforms, cleanupPlatforms := startPlatforms(ctx, logger)
	defer cleanupPlatforms()

	dispatcher := runtime.New(runtime.Config{
		TenantID:         defaultTenantID,
		Platforms:        platforms,
		Pity:             pitySystem,
		PointsPerMessage: pitySystem.Config().PointsPerMessage,
		Streak:           streakTickAdapter{sys: streakSystem},
		Broadcaster:      runtime.NewWSBroadcaster(hub, logger),
		Logger:           logger,
	})
	go func() {
		if err := dispatcher.Run(ctx); err != nil {
			logger.Error("dispatcher exited", "err", err)
		}
	}()

	webHandler := web.Handler(http.HandlerFunc(handlers.Index))
	if webHandler != nil {
		logger.Info("embedded web dashboard available")
	} else {
		logger.Info("no embedded web dashboard; serving JSON landing page at /")
	}

	router := api.NewRouter(api.Deps{
		Logger: logger,
		Version: handlers.Version{
			Version: Version,
			Phase:   "1B",
		},
		WS:            hub,
		Web:           webHandler,
		AuthStore:     authStore,
		TenantID:      defaultTenantID,
		CookieSecure:  false,
		Pity:          pitySystem,
		Streak:        streakSystem,
		StatsProvider: dispatcherStatsAdapter{d: dispatcher},
	})

	srv := server.New(server.Config{
		Addr:     "127.0.0.1:8080",
		AllowLAN: false,
		Logger:   logger,
	}, router)

	return srv.Run(ctx)
}

// streakTickAdapter wraps streak.System to satisfy runtime.StreakTicker
// without forcing the runtime package to import internal/features/streak
// (and its concrete Result type). It maps the concrete streak.Result onto
// the decoupled runtime.StreakOutcome so the dispatcher can broadcast
// feature events without depending on the streak package.
type streakTickAdapter struct{ sys *streak.System }

func (s streakTickAdapter) TickStreak(ctx context.Context, tenantID, channel, viewerID, username string) (runtime.StreakOutcome, error) {
	res, err := s.sys.Tick(ctx, tenantID, channel, viewerID, username)
	if err != nil {
		return runtime.StreakOutcome{}, err
	}
	return runtime.StreakOutcome{
		DaysCurrent:    res.DaysCurrent,
		DaysLongest:    res.DaysLongest,
		Milestone:      res.Milestone,
		BrokenFromDays: res.BrokenFromDays,
		SameDayReTick:  res.SameDayReTick,
	}, nil
}

// dispatcherStatsAdapter wraps runtime.Dispatcher to satisfy
// handlers.StatsProvider. Decoupling lets the api/handlers package stay
// independent of the runtime package.
type dispatcherStatsAdapter struct{ d *runtime.Dispatcher }

func (a dispatcherStatsAdapter) Snapshot() any { return a.d.Stats() }

// startPlatforms inspects environment variables and starts every platform
// adapter that is enabled. Returns the connected platforms and a cleanup
// function that disconnects them in reverse order.
//
// Twitch is controlled by:
//   - ENGELOS_TWITCH_CHANNELS  comma-separated channel list (e.g. "engelswtf").
//     If empty, the Twitch adapter is not started.
//   - ENGELOS_TWITCH_OAUTH     optional oauth token for authenticated mode.
//   - ENGELOS_TWITCH_USERNAME  optional bot username (required with OAUTH).
//   - ENGELOS_TWITCH_CLIENT_ID optional Helix client id (required with OAUTH).
//
// Discord is controlled by:
//   - ENGELOS_DISCORD_TOKEN     bot token. If empty, Discord is not started
//     (Discord has no anonymous mode).
//   - ENGELOS_DISCORD_CHANNELS  optional comma-separated channel-id allowlist;
//     empty means every channel the bot can see.
func startPlatforms(ctx context.Context, logger *slog.Logger) ([]adapters.Platform, func()) {
	var (
		started []adapters.Platform
		closers []func()
	)
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}

	channels := splitCSV(os.Getenv("ENGELOS_TWITCH_CHANNELS"))
	if len(channels) > 0 {
		cfg := twitch.Config{
			Channels:   channels,
			Username:   os.Getenv("ENGELOS_TWITCH_USERNAME"),
			OAuthToken: os.Getenv("ENGELOS_TWITCH_OAUTH"),
			ClientID:   os.Getenv("ENGELOS_TWITCH_CLIENT_ID"),
			Logger:     logger.With("platform", "twitch"),
		}
		tw := twitch.New(cfg)
		if err := tw.Connect(ctx); err != nil {
			logger.Error("twitch adapter connect failed", "err", err)
		} else {
			started = append(started, tw)
			closers = append(closers, func() {
				disconnectCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
				defer c()
				if err := tw.Disconnect(disconnectCtx); err != nil {
					logger.Warn("twitch disconnect", "err", err)
				}
			})
			anon := cfg.OAuthToken == ""
			logger.Info("twitch adapter connected",
				"channels", channels, "anonymous", anon)
		}
	}

	if token := os.Getenv("ENGELOS_DISCORD_TOKEN"); token != "" {
		cfg := discord.Config{
			Token:    token,
			Channels: splitCSV(os.Getenv("ENGELOS_DISCORD_CHANNELS")),
			Logger:   logger.With("platform", "discord"),
		}
		dc := discord.New(cfg)
		if err := dc.Connect(ctx); err != nil {
			logger.Error("discord adapter connect failed", "err", err)
		} else {
			started = append(started, dc)
			closers = append(closers, func() {
				disconnectCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
				defer c()
				if err := dc.Disconnect(disconnectCtx); err != nil {
					logger.Warn("discord disconnect", "err", err)
				}
			})
			logger.Info("discord adapter connected",
				"channel_allowlist", len(cfg.Channels))
		}
	}

	return started, cleanup
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// dataDirectory returns the on-disk location where the daemon stores its
// SQLite databases and other state. ENGELOS_DATA_DIR overrides everything;
// otherwise XDG_DATA_HOME/engelos is used, falling back to
// $HOME/.local/share/engelos.
func dataDirectory() (string, error) {
	if dir := os.Getenv("ENGELOS_DATA_DIR"); dir != "" {
		return dir, nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "engelos"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "engelos"), nil
}

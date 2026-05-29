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
	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/oauthrefresh"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
	"github.com/Luca-Pelzer/engelos/internal/secrets"
	"github.com/Luca-Pelzer/engelos/internal/server"
	"github.com/Luca-Pelzer/engelos/internal/web"
	"golang.org/x/oauth2"
	twitchoauth "golang.org/x/oauth2/twitch"
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

	// Optional encryption-at-rest key. When ENGELOS_SECRETS_KEY is a valid
	// 32-byte base64 key, OAuth token storage is enabled; when unset, the
	// daemon still runs but OAuth/login-with-Twitch is disabled. A malformed
	// key is fatal so misconfiguration cannot silently drop encryption.
	var cryptoBox *secrets.Box
	if raw := os.Getenv("ENGELOS_SECRETS_KEY"); raw != "" {
		box, berr := secrets.NewBoxFromBase64(raw)
		if berr != nil {
			return fmt.Errorf("ENGELOS_SECRETS_KEY invalid: %w", berr)
		}
		cryptoBox = box
		logger.Info("encryption-at-rest enabled")
	} else {
		logger.Warn("ENGELOS_SECRETS_KEY not set; OAuth token storage disabled")
	}

	authDSN := filepath.Join(dataDir, "auth.db")
	var authOpts []auth.StoreOption
	if cryptoBox != nil {
		authOpts = append(authOpts, auth.WithCrypto(cryptoBox))
	}
	authStore, err := auth.OpenSQLiteStore(ctx, authDSN, logger, authOpts...)
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

	platforms, twitchAdapter, cleanupPlatforms := startPlatforms(ctx, logger, authStore, defaultTenantID)
	defer cleanupPlatforms()

	cmdRouter := buildCommandRouter(defaultTenantID, pitySystem, streakSystem, logger)

	dispatcher := runtime.New(runtime.Config{
		TenantID:         defaultTenantID,
		Platforms:        platforms,
		Pity:             pitySystem,
		PointsPerMessage: pitySystem.Config().PointsPerMessage,
		Streak:           streakTickAdapter{sys: streakSystem},
		Broadcaster:      runtime.NewWSBroadcaster(hub, logger),
		Commands:         cmdRouter,
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

	twitchOAuthCfg := buildTwitchOAuthConfig(cryptoBox, logger)
	var oauthTwitch *handlers.OAuth
	if twitchOAuthCfg != nil {
		oauthTwitch = handlers.NewOAuth(authStore, defaultTenantID, logger, twitchOAuthCfg).
			WithCookieSecure(false)
		// Twitch user-access tokens expire ~4h after issuance; without
		// proactive refresh the stored bot token goes stale and Helix
		// calls 401. Live re-application to the connected adapter is
		// Phase 5b — here only persistence + the OnRefresh hook run.
		refresher, rerr := oauthrefresh.New(oauthrefresh.Config{
			Store:  refreshStoreAdapter{store: authStore},
			Tokens: twitchTokenSource{cfg: twitchOAuthCfg},
			Logger: logger,
			OnRefresh: func(ev oauthrefresh.RefreshEvent) {
				logger.Info("oauth token refreshed",
					"provider", ev.Identity.Provider,
					"purpose", ev.Identity.Purpose,
					"login", ev.Identity.ProviderLogin,
					"expires_at", ev.ExpiresAt.UTC())
				// Live-apply the rotated bot token to the running Twitch
				// adapter so chat/Helix keep working without a restart.
				// SetToken stages the IRC token and updates Helix in place;
				// it never reconnects, so the dispatcher event stream is
				// unaffected.
				if ev.Identity.Provider == auth.ProviderTwitch &&
					ev.Identity.Purpose == auth.OAuthPurposeBot &&
					twitchAdapter != nil {
					if err := twitchAdapter.SetToken(ev.AccessToken); err != nil {
						logger.Warn("twitch live token rotation failed",
							"login", ev.Identity.ProviderLogin, "err", err)
					}
				}
			},
		})
		if rerr != nil {
			logger.Warn("oauth refresher disabled", "err", rerr)
		} else {
			go func() {
				if err := refresher.Run(ctx); err != nil {
					logger.Error("oauth refresher exited", "err", err)
				}
			}()
			logger.Info("oauth token refresher started")
		}
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
		OAuthTwitch:   oauthTwitch,
	})

	addr := os.Getenv("ENGELOS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := server.New(server.Config{
		Addr:     addr,
		AllowLAN: envBool("ENGELOS_ALLOW_LAN"),
		Logger:   logger,
	}, router)

	return srv.Run(ctx)
}

// envBool reports whether the named environment variable is set to a truthy
// value ("1", "true", "yes", "on", case-insensitive). Anything else — including
// unset — is false, so the daemon keeps its loopback-only default unless the
// operator explicitly opts in.
func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// buildTwitchOAuthConfig assembles the Twitch *oauth2.Config shared by the
// "Login with Twitch" handler and the background token refresher, or returns
// nil (OAuth disabled) when its prerequisites are missing. It requires an
// encryption box (so tokens can be stored encrypted) and the three
// ENGELOS_TWITCH_CLIENT_ID/SECRET/REDIRECT_URL env vars; absence of any is a
// normal, non-fatal "feature off" state, not an error.
func buildTwitchOAuthConfig(box *secrets.Box, logger *slog.Logger) *oauth2.Config {
	clientID := os.Getenv("ENGELOS_TWITCH_CLIENT_ID")
	clientSecret := os.Getenv("ENGELOS_TWITCH_CLIENT_SECRET")
	redirectURL := os.Getenv("ENGELOS_TWITCH_REDIRECT_URL")
	if box == nil || clientID == "" || clientSecret == "" || redirectURL == "" {
		logger.Info("twitch oauth disabled",
			"has_key", box != nil, "has_client_id", clientID != "",
			"has_secret", clientSecret != "", "has_redirect", redirectURL != "")
		return nil
	}
	logger.Info("twitch oauth enabled", "redirect_url", redirectURL)
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"user:read:email"},
		Endpoint:     twitchoauth.Endpoint,
	}
}

// refreshStoreAdapter maps auth.Store onto the narrow oauthrefresh.Store
// surface, keeping the refresher package free of any auth import.
type refreshStoreAdapter struct{ store auth.Store }

func (a refreshStoreAdapter) ListExpiring(ctx context.Context, cutoff time.Time) ([]oauthrefresh.Identity, error) {
	ids, err := a.store.ListOAuthIdentitiesExpiringBefore(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	out := make([]oauthrefresh.Identity, len(ids))
	for i, id := range ids {
		out[i] = oauthrefresh.Identity{
			ID:            id.ID,
			Provider:      id.Provider,
			ProviderLogin: id.ProviderLogin,
			Purpose:       id.Purpose,
			RefreshToken:  id.RefreshToken,
			ExpiresAt:     id.ExpiresAt,
		}
	}
	return out, nil
}

func (a refreshStoreAdapter) UpdateTokens(ctx context.Context, id, accessToken, refreshToken string, expiresAt time.Time) error {
	return a.store.UpdateOAuthTokens(ctx, id, accessToken, refreshToken, expiresAt)
}

// twitchTokenSource adapts *oauth2.Config onto oauthrefresh.TokenSource. It
// exchanges a stored refresh token for a fresh token via the oauth2 library's
// TokenSource, which performs the provider round-trip. A zero-expiry token
// (provider returned no expires_in) is passed through unchanged.
type twitchTokenSource struct{ cfg *oauth2.Config }

func (t twitchTokenSource) Refresh(ctx context.Context, refreshToken string) (string, string, time.Time, error) {
	src := t.cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	tok, err := src.Token()
	if err != nil {
		return "", "", time.Time{}, err
	}
	return tok.AccessToken, tok.RefreshToken, tok.Expiry, nil
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

// buildCommandRouter assembles the chat-command engine with the built-in
// !pity, !streak and !commands commands wired to the live feature systems,
// and returns it as a runtime.CommandRouter. Registration failures are
// fatal-free: they are logged and the command is skipped, so a wiring bug
// degrades to "command missing" rather than crashing the daemon.
func buildCommandRouter(tenantID string, pity *pity.System, streak *streak.System, logger *slog.Logger) runtime.CommandRouter {
	engine := commands.New(commands.Config{Logger: logger})
	register := func(c commands.Command) {
		if err := engine.Register(c); err != nil {
			logger.Warn("command registration failed", "command", c.Name, "err", err)
		}
	}
	register(commands.NewPityCommand(tenantID, pityQuerier{sys: pity}))
	register(commands.NewStreakCommand(tenantID, streakQuerier{sys: streak}))
	register(commands.NewHelpCommand(engine))
	return commandRouterAdapter{engine: engine}
}

// commandRouterAdapter maps the commands.Engine onto runtime.CommandRouter,
// keeping the runtime package free of any commands import.
type commandRouterAdapter struct{ engine *commands.Engine }

func (a commandRouterAdapter) Route(ctx context.Context, inv runtime.CommandInvocation) (runtime.CommandReply, bool) {
	reply, handled := a.engine.Handle(ctx, commands.Message{
		Platform: inv.Platform,
		Channel:  inv.Channel,
		UserID:   inv.UserID,
		Username: inv.Username,
		Text:     inv.Text,
	})
	return runtime.CommandReply{Text: reply.Text}, handled
}

// pityQuerier maps pity.System onto commands.PityQuerier (translating the
// concrete pity.Status into the decoupled commands.PityStatus).
type pityQuerier struct{ sys *pity.System }

func (q pityQuerier) Status(tenantID, channel, viewerID string) commands.PityStatus {
	s := q.sys.Status(tenantID, channel, viewerID)
	return commands.PityStatus{
		Points:          s.Points,
		SoftPityHit:     s.SoftPityHit,
		NearGuaranteed:  s.NearGuaranteed,
		EffectiveChance: s.EffectiveChance,
	}
}

// streakQuerier maps streak.System onto commands.StreakQuerier.
type streakQuerier struct{ sys *streak.System }

func (q streakQuerier) Status(tenantID, channel, viewerID string) commands.StreakStatus {
	s := q.sys.Status(tenantID, channel, viewerID)
	return commands.StreakStatus{
		DaysCurrent:      s.DaysCurrent,
		DaysLongest:      s.DaysLongest,
		FreezesAvailable: s.FreezesAvailable,
		NextMilestone:    s.NextMilestone,
	}
}

// startPlatforms inspects environment variables and starts every platform
// adapter that is enabled. Returns the connected platforms, the Twitch
// adapter handle (nil when Twitch is not started — used so the OAuth
// refresher can live-rotate its token via SetToken), and a cleanup
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
func startPlatforms(ctx context.Context, logger *slog.Logger, store auth.Store, tenantID string) ([]adapters.Platform, *twitch.Adapter, func()) {
	var (
		started      []adapters.Platform
		closers      []func()
		twitchHandle *twitch.Adapter
	)
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}

	channels := splitCSV(os.Getenv("ENGELOS_TWITCH_CHANNELS"))
	if len(channels) > 0 {
		username := os.Getenv("ENGELOS_TWITCH_USERNAME")
		oauthToken := os.Getenv("ENGELOS_TWITCH_OAUTH")
		clientID := os.Getenv("ENGELOS_TWITCH_CLIENT_ID")
		// Prefer a bot token acquired via "Login with Twitch" (?purpose=bot)
		// over the static ENV token: it is stored encrypted and is the path
		// that will gain automatic refresh. The ENV token remains the
		// fallback for first-run/bootstrap before any OAuth has happened.
		if bot, err := store.GetBotIdentity(ctx, tenantID, auth.ProviderTwitch); err == nil {
			oauthToken = bot.AccessToken
			if bot.ProviderLogin != "" {
				username = bot.ProviderLogin
			}
			logger.Info("twitch bot token loaded from store", "login", bot.ProviderLogin)
		} else if !errors.Is(err, auth.ErrOAuthIdentityNotFound) && !errors.Is(err, auth.ErrCryptoRequired) {
			logger.Warn("twitch bot identity lookup failed", "err", err)
		}
		cfg := twitch.Config{
			Channels:   channels,
			Username:   username,
			OAuthToken: oauthToken,
			ClientID:   clientID,
			Logger:     logger.With("platform", "twitch"),
		}
		tw := twitch.New(cfg)
		if err := tw.Connect(ctx); err != nil {
			logger.Error("twitch adapter connect failed", "err", err)
		} else {
			twitchHandle = tw
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

	return started, twitchHandle, cleanup
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

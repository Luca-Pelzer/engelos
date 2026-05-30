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
	"sync"
	"syscall"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
	"github.com/Luca-Pelzer/engelos/internal/adapters/discord"
	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch"
	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch/eventsub"
	"github.com/Luca-Pelzer/engelos/internal/api"
	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/api/ws"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/automodstate"
	"github.com/Luca-Pelzer/engelos/internal/channelpoints"
	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/counters"
	"github.com/Luca-Pelzer/engelos/internal/customcommands"
	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/featureflags"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/liveops"
	"github.com/Luca-Pelzer/engelos/internal/loyalty"
	"github.com/Luca-Pelzer/engelos/internal/moderation"
	"github.com/Luca-Pelzer/engelos/internal/oauthrefresh"
	"github.com/Luca-Pelzer/engelos/internal/overlay"
	"github.com/Luca-Pelzer/engelos/internal/quotes"
	"github.com/Luca-Pelzer/engelos/internal/redemptions"
	"github.com/Luca-Pelzer/engelos/internal/rewards"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
	"github.com/Luca-Pelzer/engelos/internal/secrets"
	"github.com/Luca-Pelzer/engelos/internal/server"
	"github.com/Luca-Pelzer/engelos/internal/timers"
	"github.com/Luca-Pelzer/engelos/internal/web"
	"github.com/coder/websocket"
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

	customDSN := filepath.Join(dataDir, "custom_commands.db")
	customStore, err := customcommands.OpenSQLiteStore(ctx, customDSN, logger)
	if err != nil {
		return fmt.Errorf("open custom command store %s: %w", customDSN, err)
	}
	defer func() {
		if cerr := customStore.Close(); cerr != nil {
			logger.Warn("custom command store close failed", "err", cerr)
		}
	}()
	logger.Info("custom command store opened", "dsn", customDSN)

	timersDSN := filepath.Join(dataDir, "timers.db")
	timerStore, err := timers.OpenSQLiteStore(ctx, timersDSN, logger)
	if err != nil {
		return fmt.Errorf("open timer store %s: %w", timersDSN, err)
	}
	defer func() {
		if cerr := timerStore.Close(); cerr != nil {
			logger.Warn("timer store close failed", "err", cerr)
		}
	}()
	logger.Info("timer store opened", "dsn", timersDSN)

	quotesDSN := filepath.Join(dataDir, "quotes.db")
	quoteStore, err := quotes.OpenSQLiteStore(ctx, quotesDSN, logger)
	if err != nil {
		return fmt.Errorf("open quote store %s: %w", quotesDSN, err)
	}
	defer func() {
		if cerr := quoteStore.Close(); cerr != nil {
			logger.Warn("quote store close failed", "err", cerr)
		}
	}()
	logger.Info("quote store opened", "dsn", quotesDSN)

	countersDSN := filepath.Join(dataDir, "counters.db")
	counterStore, err := counters.OpenSQLiteStore(ctx, countersDSN, logger)
	if err != nil {
		return fmt.Errorf("open counter store %s: %w", countersDSN, err)
	}
	defer func() {
		if cerr := counterStore.Close(); cerr != nil {
			logger.Warn("counter store close failed", "err", cerr)
		}
	}()
	logger.Info("counter store opened", "dsn", countersDSN)

	liveopsDSN := filepath.Join(dataDir, "liveops.db")
	eventStoreLO, err := liveops.OpenSQLiteStore(ctx, liveopsDSN, logger)
	if err != nil {
		return fmt.Errorf("open liveops store %s: %w", liveopsDSN, err)
	}
	defer func() {
		if cerr := eventStoreLO.Close(); cerr != nil {
			logger.Warn("liveops store close failed", "err", cerr)
		}
	}()
	logger.Info("liveops store opened", "dsn", liveopsDSN)

	redemptionsDSN := filepath.Join(dataDir, "redemptions.db")
	redemptionStore, err := redemptions.OpenSQLiteStore(ctx, redemptionsDSN, logger)
	if err != nil {
		return fmt.Errorf("open redemptions store %s: %w", redemptionsDSN, err)
	}
	defer func() {
		if cerr := redemptionStore.Close(); cerr != nil {
			logger.Warn("redemptions store close failed", "err", cerr)
		}
	}()
	logger.Info("redemptions store opened", "dsn", redemptionsDSN)

	automodAuditDSN := filepath.Join(dataDir, "automod_audit.db")
	automodAudit, err := automodstate.OpenSQLiteStore(ctx, automodAuditDSN, logger)
	if err != nil {
		return fmt.Errorf("open automod audit store %s: %w", automodAuditDSN, err)
	}
	defer func() {
		if cerr := automodAudit.Close(); cerr != nil {
			logger.Warn("automod audit store close failed", "err", cerr)
		}
	}()
	logger.Info("automod audit store opened", "dsn", automodAuditDSN)

	loyaltyDSN := filepath.Join(dataDir, "loyalty.db")
	loyaltyStore, err := loyalty.OpenSQLiteStore(ctx, loyaltyDSN, logger)
	if err != nil {
		return fmt.Errorf("open loyalty store %s: %w", loyaltyDSN, err)
	}
	defer func() {
		if cerr := loyaltyStore.Close(); cerr != nil {
			logger.Warn("loyalty store close failed", "err", cerr)
		}
	}()
	logger.Info("loyalty store opened", "dsn", loyaltyDSN)

	featureFlagsDSN := filepath.Join(dataDir, "featureflags.db")
	featureFlagStore, err := featureflags.OpenSQLiteStore(ctx, featureFlagsDSN, logger)
	if err != nil {
		return fmt.Errorf("open feature flags store %s: %w", featureFlagsDSN, err)
	}
	defer func() {
		if cerr := featureFlagStore.Close(); cerr != nil {
			logger.Warn("feature flags store close failed", "err", cerr)
		}
	}()
	logger.Info("feature flags store opened", "dsn", featureFlagsDSN)

	economy := newEconomyAdapter(loyaltyStore, defaultTenantID, defaultEarnAmount, defaultEarnCooldown).
		withFeatureGate(featureGateAdapter{store: featureFlagStore, tenantID: defaultTenantID})

	rewardsDSN := filepath.Join(dataDir, "rewards.db")
	rewardsStore, err := rewards.OpenSQLiteStore(ctx, rewardsDSN, logger)
	if err != nil {
		return fmt.Errorf("open rewards store %s: %w", rewardsDSN, err)
	}
	defer func() {
		if cerr := rewardsStore.Close(); cerr != nil {
			logger.Warn("rewards store close failed", "err", cerr)
		}
	}()
	logger.Info("rewards store opened", "dsn", rewardsDSN)
	rewardCatalog := rewardCatalogAdapter{store: rewardsStore, tenantID: defaultTenantID, logger: logger}

	automodEngine, err := automod.NewEngine(automod.DefaultConfig())
	if err != nil {
		return fmt.Errorf("init automod engine: %w", err)
	}
	moderationSvc := moderation.New(moderation.Config{
		Engine:   automodEngine,
		Audit:    automodAudit,
		TenantID: defaultTenantID,
		Logger:   logger,
	})
	logger.Info("automod ready", "mode", "active (all filters disabled by default)")

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

	allowLAN := envBool("ENGELOS_ALLOW_LAN")
	allowedOrigins := splitCSV(os.Getenv("ENGELOS_ALLOWED_ORIGINS"))

	// Loopback-only (the default) keeps the permissive InsecureSkipVerify hub
	// since the socket is only reachable from localhost (e.g. OBS). Once the
	// daemon is exposed (AllowLAN) and an explicit origin allowlist exists,
	// restrict the WebSocket handshake to those origins so a malicious web page
	// cannot open the chat-event socket.
	var hubOpts []ws.HubOption
	if allowLAN && len(allowedOrigins) > 0 {
		hubOpts = append(hubOpts, ws.WithAcceptOptions(&websocket.AcceptOptions{
			OriginPatterns: allowedOrigins,
		}))
	}
	hub := ws.NewHub(logger, hubOpts...)
	go hub.Run(ctx)

	platforms, twitchAdapter, cleanupPlatforms := startPlatforms(ctx, logger, authStore, defaultTenantID)
	defer cleanupPlatforms()

	economy.withResolver(userProfileProvider{adapter: twitchAdapter}.UserProfile)

	cmdRouter := buildCommandRouter(defaultTenantID, pitySystem, streakSystem, customStore, timerStore, quoteStore, counterStore, eventStoreLO, twitchAdapter, economy, platformSender{platforms: platforms}, rewardCatalog, featureGateAdapter{store: featureFlagStore, tenantID: defaultTenantID}, logger)

	// Channel-Points trigger engine (#13). Gated: it only starts when the
	// Twitch adapter is authenticated (Helix available) AND the broadcaster
	// is an affiliate/partner — custom rewards 403 otherwise. In every other
	// case it logs the reason and stays a no-op, so anonymous/non-affiliate
	// deployments boot cleanly with the rest of the bot unaffected.
	startChannelPoints(ctx, logger, twitchAdapter, redemptionStore,
		platformSender{platforms: platforms},
		channelPointsCounters{admin: counterAdmin{tenantID: defaultTenantID, store: counterStore}},
		defaultTenantID, splitCSV(os.Getenv("ENGELOS_TWITCH_CHANNELS")))

	timerScheduler, err := timers.New(timers.Config{
		Store:    timerStore,
		Sender:   platformSender{platforms: platforms},
		TenantID: defaultTenantID,
		Logger:   logger,
	})
	if err != nil {
		return fmt.Errorf("init timer scheduler: %w", err)
	}
	go func() {
		if rerr := timerScheduler.Run(ctx); rerr != nil {
			logger.Error("timer scheduler exited", "err", rerr)
		}
	}()
	logger.Info("timer scheduler started")

	dispatcher := runtime.New(runtime.Config{
		TenantID:         defaultTenantID,
		Platforms:        platforms,
		Pity:             pitySystem,
		PointsPerMessage: pitySystem.Config().PointsPerMessage,
		Streak:           streakTickAdapter{sys: streakSystem},
		Broadcaster:      runtime.NewWSBroadcaster(hub, logger),
		Commands:         cmdRouter,
		Moderator:        moderationAdapter{svc: moderationSvc},
		Economy:          economy,
		Activity:         timerScheduler,
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
		AllowedOrigins:  allowedOrigins,
		WS:              hub,
		Web:             webHandler,
		Overlay:         overlay.Handler(logger),
		AuthStore:       authStore,
		TenantID:        defaultTenantID,
		CookieSecure:    false,
		Pity:            pitySystem,
		Streak:          streakSystem,
		StatsProvider:   dispatcherStatsAdapter{d: dispatcher},
		OAuthTwitch:     oauthTwitch,
		RedemptionStore: redemptionStore,
		CommandStore:    customStore,
		CounterStore:    counterStore,
		Moderation:      moderationSvc,
		FeatureStore:    featureFlagStore,
	})

	addr := os.Getenv("ENGELOS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := server.New(server.Config{
		Addr:           addr,
		AllowLAN:       allowLAN,
		AllowedOrigins: allowedOrigins,
		Logger:         logger,
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
	scopes := twitchOAuthScopes()
	logger.Info("twitch oauth enabled", "redirect_url", redirectURL, "scopes", scopes)
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
		Endpoint:     twitchoauth.Endpoint,
	}
}

// defaultTwitchScopes lists each requested OAuth scope with the capability
// it unlocks, so the grant stays minimal and auditable:
var defaultTwitchScopes = []string{
	"user:read:email",                // identify the account
	"chat:read",                      // receive chat messages
	"chat:edit",                      // send chat messages
	"moderator:manage:banned_users",  // ban / timeout actions
	"moderator:manage:chat_messages", // delete-message action
	"channel:read:redemptions",       // observe channel-point redemptions
	"channel:manage:redemptions",     // create rewards + fulfill/refund redemptions
	"moderator:read:followers",       // read follower dates for !followage
}

// twitchOAuthScopes returns the scopes to request, allowing an operator to
// override the default set via ENGELOS_TWITCH_SCOPES (comma-separated).
func twitchOAuthScopes() []string {
	if custom := splitCSV(os.Getenv("ENGELOS_TWITCH_SCOPES")); len(custom) > 0 {
		return custom
	}
	return defaultTwitchScopes
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

// Loyalty economy tuning. A viewer earns defaultEarnAmount points at most once
// per defaultEarnCooldown of chatting — the per-viewer cooldown is the
// anti-farming gate (idle lurkers and message-flooding bots cannot accumulate
// faster than this rate).
const (
	defaultEarnAmount   = 10
	defaultEarnCooldown = 60 * time.Second
)

// economyAdapter wires the loyalty store into both the dispatcher (Award, the
// cooldown-gated earn path) and the loyalty chat commands (Balance/Transfer/
// Top). It owns the per-viewer earn-cooldown state. A nil store disables every
// path safely. The profile resolver turns a !give target username into the
// recipient's stable platform id, since the store transfers by viewer id.
type economyAdapter struct {
	store        loyalty.Store
	tenantID     string
	earnAmount   int64
	earnEvery    time.Duration
	resolve      func(ctx context.Context, login string) (commands.UserProfile, error)
	gate         featureGate
	logger       *slog.Logger
	mu           sync.Mutex
	lastEarnedAt map[string]time.Time
}

// featureGate reports whether the per-channel points economy is switched on.
// economyAdapter consults it before every balance-changing operation so a
// streamer can disable the entire economy (earning plus every points game) for
// their channel without restarting the daemon. The economy defaults ON: a nil
// gate, or any lookup error, leaves it enabled so a storage hiccup never
// silently freezes points for everyone.
type featureGate interface {
	EconomyEnabled(ctx context.Context, channel string) bool
}

// featureGateAdapter resolves the "economy" toggle from the featureflags store,
// defaulting to ON when no explicit override is stored (so existing channels
// keep their economy until a mod deliberately turns it off).
type featureGateAdapter struct {
	store    featureflags.Store
	tenantID string
}

// EconomyEnabled returns the channel's "economy" flag, defaulting to true. A
// store error is treated as enabled (fail-open) and is the caller's last line:
// freezing every viewer's points on a transient read error would be a worse
// outcome than briefly honouring a toggle that is being flipped off.
func (a featureGateAdapter) EconomyEnabled(ctx context.Context, channel string) bool {
	if a.store == nil {
		return true
	}
	enabled, err := a.store.GetOrDefault(ctx, a.tenantID, channel, featureEconomy, true)
	if err != nil {
		return true
	}
	return enabled
}

// SetEconomy persists an explicit on/off override for the channel's economy,
// satisfying commands.FeatureToggleStore so the mods-only !economy command can
// flip it from chat.
func (a featureGateAdapter) SetEconomy(ctx context.Context, channel string, enabled bool) error {
	if a.store == nil {
		return nil
	}
	return a.store.Set(ctx, a.tenantID, channel, featureEconomy, enabled)
}

// featureEconomy is the feature key for the per-channel points economy toggle.
// Enabling it turns on earning AND every points-based game at once (gamble,
// slots, duel, heist, rewards); disabling it freezes them all. It is the single
// switch enabling the whole points economy at once.
const featureEconomy = "economy"

func newEconomyAdapter(store loyalty.Store, tenantID string, amount int64, every time.Duration) *economyAdapter {
	return &economyAdapter{
		store:        store,
		tenantID:     tenantID,
		earnAmount:   amount,
		earnEvery:    every,
		logger:       slog.Default(),
		lastEarnedAt: make(map[string]time.Time),
	}
}

// withResolver sets the username→profile resolver used by Transfer and returns
// the adapter for chaining.
func (e *economyAdapter) withResolver(r func(ctx context.Context, login string) (commands.UserProfile, error)) *economyAdapter {
	e.resolve = r
	return e
}

// withFeatureGate sets the per-channel economy toggle source and returns the
// adapter for chaining. A nil gate (the zero value) leaves the economy always
// enabled.
func (e *economyAdapter) withFeatureGate(g featureGate) *economyAdapter {
	e.gate = g
	return e
}

// enabled reports whether the points economy is on for channel, defaulting to
// true when no gate is wired so the adapter behaves exactly as before until a
// toggle is set.
func (e *economyAdapter) enabled(ctx context.Context, channel string) bool {
	if e == nil || e.gate == nil {
		return true
	}
	return e.gate.EconomyEnabled(ctx, channel)
}

// Award credits the viewer once per earn cooldown. The cooldown check and the
// timestamp update are done under the lock so two near-simultaneous messages
// from one viewer cannot both earn.
func (e *economyAdapter) Award(ctx context.Context, tenantID, channel, viewerID, username string) {
	if e == nil || e.store == nil || viewerID == "" {
		return
	}
	if !e.enabled(ctx, channel) {
		return
	}
	key := channel + "|" + viewerID
	now := time.Now()
	e.mu.Lock()
	if last, ok := e.lastEarnedAt[key]; ok && now.Sub(last) < e.earnEvery {
		e.mu.Unlock()
		return
	}
	e.lastEarnedAt[key] = now
	e.mu.Unlock()

	if _, err := e.store.Earn(ctx, tenantID, channel, viewerID, username, e.earnAmount); err != nil {
		e.logger.Warn("loyalty earn failed", "channel", channel, "viewer", viewerID, "err", err)
	}
}

// Balance implements commands.LoyaltyProvider.
func (e *economyAdapter) Balance(ctx context.Context, channel, viewerID string) (int64, commands.LoyaltyError) {
	if e == nil || e.store == nil || !e.enabled(ctx, channel) {
		return 0, commands.LoyaltyUnavailable
	}
	acct, err := e.store.Balance(ctx, e.tenantID, channel, viewerID)
	switch {
	case err == nil:
		return acct.Balance, commands.LoyaltyOK
	case errors.Is(err, loyalty.ErrNotFound):
		return 0, commands.LoyaltyNotFound
	default:
		return 0, commands.LoyaltyUnavailable
	}
}

// Transfer implements commands.LoyaltyProvider. It resolves the target
// username to a stable viewer id (via the profile resolver) before moving
// funds, and reports the recipient's canonical display name back for the reply.
func (e *economyAdapter) Transfer(ctx context.Context, channel, fromViewerID, toUsername string, amount int64) (commands.LoyaltyError, string) {
	if e == nil || e.store == nil || e.resolve == nil || !e.enabled(ctx, channel) {
		return commands.LoyaltyUnavailable, ""
	}
	prof, err := e.resolve(ctx, toUsername)
	if err != nil {
		return commands.LoyaltyInvalid, ""
	}
	toViewerID := prof.Login
	display := prof.DisplayName
	if prof.Login == "" {
		return commands.LoyaltyInvalid, ""
	}
	_, _, err = e.store.Transfer(ctx, e.tenantID, channel, fromViewerID, toViewerID, prof.Login, amount)
	switch {
	case err == nil:
		return commands.LoyaltyOK, display
	case errors.Is(err, loyalty.ErrInsufficient):
		return commands.LoyaltyInsufficient, ""
	case errors.Is(err, loyalty.ErrNotFound):
		return commands.LoyaltyNotFound, ""
	case errors.Is(err, loyalty.ErrInvalid):
		return commands.LoyaltyInvalid, ""
	default:
		return commands.LoyaltyUnavailable, ""
	}
}

// Top implements commands.LoyaltyProvider.
func (e *economyAdapter) Top(ctx context.Context, channel string, n int) []commands.LoyaltyEntry {
	if e == nil || e.store == nil || !e.enabled(ctx, channel) {
		return nil
	}
	accts, err := e.store.Leaderboard(ctx, e.tenantID, channel, n)
	if err != nil {
		return nil
	}
	out := make([]commands.LoyaltyEntry, 0, len(accts))
	for _, a := range accts {
		name := a.Username
		if name == "" {
			name = a.ViewerID
		}
		out = append(out, commands.LoyaltyEntry{Username: name, Balance: a.Balance})
	}
	return out
}

// Wager implements commands.GameBank: it spends the stake first (so a player
// who cannot afford the bet risks nothing), then on a win credits the payout.
// Spend and Earn are each atomic in the store; doing the spend before the
// credit guarantees the balance can never go negative even on a partial
// failure (a failed credit just means the player loses the stake, never more).
func (e *economyAdapter) Wager(ctx context.Context, channel, viewerID string, bet, payout int64) (int64, commands.LoyaltyError) {
	if e == nil || e.store == nil || !e.enabled(ctx, channel) {
		return 0, commands.LoyaltyUnavailable
	}
	spent, err := e.store.Spend(ctx, e.tenantID, channel, viewerID, bet)
	switch {
	case err == nil:
	case errors.Is(err, loyalty.ErrInsufficient):
		return 0, commands.LoyaltyInsufficient
	case errors.Is(err, loyalty.ErrNotFound):
		return 0, commands.LoyaltyNotFound
	case errors.Is(err, loyalty.ErrInvalid):
		return 0, commands.LoyaltyInvalid
	default:
		return 0, commands.LoyaltyUnavailable
	}
	if payout <= 0 {
		return spent.Balance, commands.LoyaltyOK
	}
	won, err := e.store.Earn(ctx, e.tenantID, channel, viewerID, spent.Username, payout)
	if err != nil {
		e.logger.Warn("loyalty wager payout failed", "channel", channel, "viewer", viewerID, "err", err)
		return spent.Balance, commands.LoyaltyOK
	}
	return won.Balance, commands.LoyaltyOK
}

// CanAfford implements commands.DuelBank: reports whether the viewer currently
// holds at least amount points.
func (e *economyAdapter) CanAfford(ctx context.Context, channel, viewerID string, amount int64) bool {
	if e == nil || e.store == nil || !e.enabled(ctx, channel) {
		return false
	}
	acct, err := e.store.Balance(ctx, e.tenantID, channel, viewerID)
	if err != nil {
		return false
	}
	return acct.Balance >= amount
}

// Settle implements commands.DuelBank for a player-vs-player duel: it confirms
// BOTH players can still afford the stake, spends the stake from each, then
// credits the whole pot (amount*2) to the winner. Affordability is checked
// before any spend so a failed duel never leaves one player out of pocket; the
// only window is between the two spends, which is acceptable because the
// preceding CanAfford checks plus the store's atomic single-writer spends make
// a mid-settle shortfall effectively impossible for a non-adversarial chat.
func (e *economyAdapter) Settle(ctx context.Context, channel, winnerID, loserID string, amount int64) (int64, commands.LoyaltyError) {
	if e == nil || e.store == nil || !e.enabled(ctx, channel) {
		return 0, commands.LoyaltyUnavailable
	}
	if !e.CanAfford(ctx, channel, winnerID, amount) || !e.CanAfford(ctx, channel, loserID, amount) {
		return 0, commands.LoyaltyInsufficient
	}
	winnerAcct, err := e.store.Spend(ctx, e.tenantID, channel, winnerID, amount)
	if err != nil {
		return 0, commands.LoyaltyInsufficient
	}
	if _, err := e.store.Spend(ctx, e.tenantID, channel, loserID, amount); err != nil {
		// Refund the winner's stake so a loser-spend failure moves no money.
		if _, rerr := e.store.Earn(ctx, e.tenantID, channel, winnerID, winnerAcct.Username, amount); rerr != nil {
			e.logger.Warn("duel refund failed", "channel", channel, "viewer", winnerID, "err", rerr)
		}
		return 0, commands.LoyaltyInsufficient
	}
	won, err := e.store.Earn(ctx, e.tenantID, channel, winnerID, winnerAcct.Username, amount*2)
	if err != nil {
		e.logger.Warn("duel payout failed", "channel", channel, "viewer", winnerID, "err", err)
		return 0, commands.LoyaltyUnavailable
	}
	return won.Balance, commands.LoyaltyOK
}

// Collect implements commands.HeistBank: it spends a player's stake as they
// join a heist, reporting false (and taking nothing) when they cannot afford it.
func (e *economyAdapter) Collect(ctx context.Context, channel, viewerID string, amount int64) bool {
	if e == nil || e.store == nil || !e.enabled(ctx, channel) {
		return false
	}
	if _, err := e.store.Spend(ctx, e.tenantID, channel, viewerID, amount); err != nil {
		return false
	}
	return true
}

// Payout implements commands.HeistBank: it credits a surviving heist player.
// The username is left empty because the account already exists (the player was
// Collected from), so Earn only adjusts the balance.
func (e *economyAdapter) Payout(ctx context.Context, channel, viewerID string, amount int64) {
	if e == nil || e.store == nil || amount <= 0 || !e.enabled(ctx, channel) {
		return
	}
	if _, err := e.store.Earn(ctx, e.tenantID, channel, viewerID, viewerID, amount); err != nil {
		e.logger.Warn("heist payout failed", "channel", channel, "viewer", viewerID, "err", err)
	}
}

// Spend implements commands.RedeemBank: it deducts a reward's cost from the
// viewer, mapping the loyalty store sentinels onto the command-facing enum.
func (e *economyAdapter) Spend(ctx context.Context, channel, viewerID string, amount int64) commands.LoyaltyError {
	if e == nil || e.store == nil || !e.enabled(ctx, channel) {
		return commands.LoyaltyUnavailable
	}
	_, err := e.store.Spend(ctx, e.tenantID, channel, viewerID, amount)
	switch {
	case err == nil:
		return commands.LoyaltyOK
	case errors.Is(err, loyalty.ErrInsufficient):
		return commands.LoyaltyInsufficient
	case errors.Is(err, loyalty.ErrNotFound):
		return commands.LoyaltyNotFound
	case errors.Is(err, loyalty.ErrInvalid):
		return commands.LoyaltyInvalid
	default:
		return commands.LoyaltyUnavailable
	}
}

// rewardCatalogAdapter maps the rewards SQLite store onto the decoupled
// commands.RewardCatalog interface, translating the store's sentinel errors
// into the command-facing RewardOutcome enum so the commands package never
// imports internal/rewards.
type rewardCatalogAdapter struct {
	store    rewards.Store
	tenantID string
	logger   *slog.Logger
}

func (a rewardCatalogAdapter) Add(ctx context.Context, channel, name string, cost int64, description, createdBy string) commands.RewardOutcome {
	if a.store == nil {
		return commands.RewardUnavailable
	}
	_, err := a.store.Create(ctx, rewards.Reward{
		TenantID: a.tenantID, Channel: channel, Name: name,
		Cost: cost, Description: description, CreatedBy: createdBy,
	})
	return rewardOutcome(err)
}

func (a rewardCatalogAdapter) Remove(ctx context.Context, channel, name string) commands.RewardOutcome {
	if a.store == nil {
		return commands.RewardUnavailable
	}
	return rewardOutcome(a.store.Delete(ctx, a.tenantID, channel, name))
}

func (a rewardCatalogAdapter) Get(ctx context.Context, channel, name string) (commands.RewardItem, commands.RewardOutcome) {
	if a.store == nil {
		return commands.RewardItem{}, commands.RewardUnavailable
	}
	r, err := a.store.Get(ctx, a.tenantID, channel, name)
	if out := rewardOutcome(err); out != commands.RewardOK {
		return commands.RewardItem{}, out
	}
	return commands.RewardItem{Name: r.Name, Description: r.Description, Cost: r.Cost}, commands.RewardOK
}

func (a rewardCatalogAdapter) List(ctx context.Context, channel string) []commands.RewardItem {
	if a.store == nil {
		return nil
	}
	rs, err := a.store.List(ctx, a.tenantID, channel)
	if err != nil {
		return nil
	}
	out := make([]commands.RewardItem, 0, len(rs))
	for _, r := range rs {
		out = append(out, commands.RewardItem{Name: r.Name, Description: r.Description, Cost: r.Cost})
	}
	return out
}

// rewardOutcome maps a rewards-store error onto the command-facing enum.
func rewardOutcome(err error) commands.RewardOutcome {
	switch {
	case err == nil:
		return commands.RewardOK
	case errors.Is(err, rewards.ErrNotFound):
		return commands.RewardNotFound
	case errors.Is(err, rewards.ErrAlreadyExists):
		return commands.RewardExists
	case errors.Is(err, rewards.ErrInvalid):
		return commands.RewardInvalid
	default:
		return commands.RewardUnavailable
	}
}

// redeemSenderAdapter lets the shared platform sender (typed as a HeistSender at
// the call site) also satisfy commands.RedeemSender; the two interfaces are
// structurally identical, this just bridges the nominal type.
type redeemSenderAdapter struct{ sender commands.HeistSender }

func (a redeemSenderAdapter) Send(ctx context.Context, channel, message string) error {
	return a.sender.Send(ctx, channel, message)
}

// moderationAdapter bridges the runtime.Moderator interface (positional, to
// keep the runtime decoupled) to the moderation.Service. A nil svc yields a
// no-op that always passes, so AutoMod can be absent without a nil-check at the
// dispatcher call site.
type moderationAdapter struct{ svc *moderation.Service }

func (a moderationAdapter) Evaluate(ctx context.Context, channel, messageID, userID, username, text string,
	emoteCount int, firstMsg, isMod, isVIP, isSub, isBroadcaster bool) runtime.ModDecision {
	dec := a.svc.Evaluate(ctx, moderation.Message{
		Channel:       channel,
		MessageID:     messageID,
		UserID:        userID,
		Username:      username,
		Text:          text,
		EmoteCount:    emoteCount,
		FirstMsg:      firstMsg,
		IsModerator:   isMod,
		IsVIP:         isVIP,
		IsSubscriber:  isSub,
		IsBroadcaster: isBroadcaster,
	})
	return runtime.ModDecision{
		Action:   runtime.ModAction(dec.Kind),
		Duration: dec.Duration,
		Reason:   dec.Reason,
		DryRun:   dec.DryRun,
	}
}

// dispatcherStatsAdapter wraps runtime.Dispatcher to satisfy
// handlers.StatsProvider. Decoupling lets the api/handlers package stay
// independent of the runtime package.
type dispatcherStatsAdapter struct{ d *runtime.Dispatcher }

func (a dispatcherStatsAdapter) Snapshot() any { return a.d.Stats() }

// buildCommandRouter assembles the chat-command engine wired to the live
// feature systems and the custom-command store, and returns it as a
// runtime.CommandRouter. Static built-ins (!pity, !streak, !leaderboard,
// !commands) plus the mod-gated admin commands (!addcom/!editcom/!delcom)
// are registered; unknown prefixed tokens fall back to the custom-command
// Resolver. Registration failures are fatal-free: they are logged and the
// command is skipped, so a wiring bug degrades to "command missing" rather
// than crashing the daemon.
func buildCommandRouter(tenantID string, pity *pity.System, streak *streak.System, custom customcommands.Store, timerStore timers.Store, quoteStore quotes.Store, counterStore counters.Store, liveopsStore liveops.Store, twitchAdapter *twitch.Adapter, loyaltyProvider commands.LoyaltyProvider, heistSender commands.HeistSender, rewardCatalog commands.RewardCatalog, featureToggle commands.FeatureToggleStore, logger *slog.Logger) runtime.CommandRouter {
	engine := commands.New(commands.Config{
		Logger:   logger,
		Resolver: customResolver{tenantID: tenantID, store: custom},
	})
	register := func(c commands.Command) {
		if err := engine.Register(c); err != nil {
			logger.Warn("command registration failed", "command", c.Name, "err", err)
		}
	}
	register(commands.NewPityCommand(tenantID, pityQuerier{sys: pity}))
	register(commands.NewStreakCommand(tenantID, streakQuerier{sys: streak}))
	register(commands.NewLeaderboardCommand(tenantID, leaderboardQuerier{pity: pity, streak: streak}))
	adminStore := customCommandAdmin{tenantID: tenantID, store: custom}
	register(commands.NewAddCommand(adminStore))
	register(commands.NewEditCommand(adminStore))
	register(commands.NewDeleteCommand(adminStore))
	timerAdminStore := timerAdmin{tenantID: tenantID, store: timerStore}
	register(commands.NewAddTimerCommand(timerAdminStore))
	register(commands.NewDeleteTimerCommand(timerAdminStore))
	register(commands.NewListTimersCommand(timerAdminStore))
	quoteAdminStore := quoteAdmin{tenantID: tenantID, store: quoteStore}
	register(commands.NewAddQuoteCommand(quoteAdminStore))
	register(commands.NewQuoteCommand(quoteAdminStore))
	register(commands.NewDeleteQuoteCommand(quoteAdminStore))
	counterAdminStore := counterAdmin{tenantID: tenantID, store: counterStore}
	register(commands.NewCounterCommand(counterAdminStore))
	register(commands.NewCounterAddCommand(counterAdminStore))
	register(commands.NewCounterSubCommand(counterAdminStore))
	register(commands.NewSetCounterCommand(counterAdminStore))
	register(commands.NewResetCounterCommand(counterAdminStore))
	register(commands.NewUptimeCommand(uptimeProvider{adapter: twitchAdapter}))
	streamProvider := streamStatusProvider{adapter: twitchAdapter}
	register(commands.NewGameCommand(streamProvider))
	register(commands.NewTitleCommand(streamProvider))
	liveopsAdminStore := liveopsAdmin{tenantID: tenantID, store: liveopsStore}
	register(commands.NewNextEventCommand(liveopsAdminStore))
	register(commands.NewScheduleCommand(liveopsAdminStore))
	register(commands.NewAddEventCommand(liveopsAdminStore))
	register(commands.NewDelEventCommand(liveopsAdminStore))

	profileProvider := userProfileProvider{adapter: twitchAdapter}
	register(commands.NewAccountAgeCommand(profileProvider))
	register(commands.NewShoutoutCommand(profileProvider, streamProvider))
	register(commands.NewFollowAgeCommand(profileProvider))

	if featureToggle != nil {
		register(commands.NewEconomyToggleCommand(featureToggle))
	}

	register(commands.NewPointsCommand(loyaltyProvider))
	register(commands.NewGiveCommand(loyaltyProvider))
	register(commands.NewPointsLeaderboardCommand(loyaltyProvider))
	if bank, ok := loyaltyProvider.(commands.GameBank); ok && loyaltyProvider != nil {
		register(commands.NewGambleCommand(bank))
		register(commands.NewSlotsCommand(bank))
	}
	if dbank, ok := loyaltyProvider.(commands.DuelBank); ok && loyaltyProvider != nil {
		duelCmd, acceptCmd := commands.NewDuelGame(dbank)
		register(duelCmd)
		register(acceptCmd)
	}

	for _, c := range commands.NewGiveawayCommands() {
		register(c)
	}

	if hbank, ok := loyaltyProvider.(commands.HeistBank); ok && loyaltyProvider != nil && heistSender != nil {
		register(commands.NewHeistGame(hbank, heistSender))
	}

	if rewardCatalog != nil {
		register(commands.NewRewardCommand(rewardCatalog))
		register(commands.NewRewardsCommand(rewardCatalog))
		if redeemBank, ok := loyaltyProvider.(commands.RedeemBank); ok && loyaltyProvider != nil {
			var redeemSender commands.RedeemSender
			if heistSender != nil {
				redeemSender = redeemSenderAdapter{sender: heistSender}
			}
			register(commands.NewRedeemCommand(rewardCatalog, redeemBank, redeemSender))
		}
	}

	register(commands.NewEightBallCommand())
	register(commands.NewLurkCommand())
	register(commands.NewUnlurkCommand())
	register(commands.NewDiceCommand())
	register(commands.NewRollCommand())
	register(commands.NewLoveCommand())
	register(commands.NewShipCommand())
	register(commands.NewHugCommand())
	register(commands.NewSlapCommand())

	register(commands.NewHelpCommand(engine))
	return commandRouterAdapter{engine: engine}
}

// customResolver maps customcommands.Store onto commands.Resolver: it looks
// up a stored command by (tenant, channel, name) and projects it into a
// commands.ResolvedCommand, translating the stored role string into a
// commands.Role. A miss (or any store error) resolves to ok=false so the
// engine treats the token as a non-command.
type customResolver struct {
	tenantID string
	store    customcommands.Store
}

func (r customResolver) Resolve(ctx context.Context, channel, name string) (commands.ResolvedCommand, bool) {
	cc, err := r.store.Get(ctx, r.tenantID, channel, name)
	if err != nil {
		return commands.ResolvedCommand{}, false
	}
	return commands.ResolvedCommand{
		Response: cc.Response,
		MinRole:  parseRole(cc.MinRole),
	}, true
}

// customCommandAdmin maps customcommands.Store onto the narrow
// commands.CustomCommandStore the !addcom/!editcom/!delcom built-ins need,
// binding the tenant id the daemon serves.
type customCommandAdmin struct {
	tenantID string
	store    customcommands.Store
}

func (a customCommandAdmin) Add(ctx context.Context, channel, name, response, minRole, createdBy string) error {
	_, err := a.store.Create(ctx, customcommands.CustomCommand{
		TenantID:  a.tenantID,
		Channel:   channel,
		Name:      name,
		Response:  response,
		MinRole:   minRole,
		CreatedBy: createdBy,
	})
	return err
}

func (a customCommandAdmin) Edit(ctx context.Context, channel, name, response string) error {
	_, err := a.store.Update(ctx, a.tenantID, channel, name, response, "")
	return err
}

func (a customCommandAdmin) Remove(ctx context.Context, channel, name string) error {
	return a.store.Delete(ctx, a.tenantID, channel, name)
}

// parseRole maps a stored role string onto a commands.Role, defaulting to
// RoleEveryone for empty or unrecognised values.
func parseRole(s string) commands.Role {
	switch s {
	case "subscriber":
		return commands.RoleSubscriber
	case "vip":
		return commands.RoleVIP
	case "moderator":
		return commands.RoleModerator
	case "broadcaster":
		return commands.RoleBroadcaster
	default:
		return commands.RoleEveryone
	}
}

// platformSender adapts the connected platform adapters onto timers.Sender
// so the scheduler can post auto-announcements. It posts to every connected
// platform and reports success when at least one delivered; per-platform
// channel routing is future work (the live deployment is Twitch-only).
type platformSender struct{ platforms []adapters.Platform }

func (s platformSender) Send(ctx context.Context, channel, message string) error {
	var firstErr error
	sent := false
	for _, p := range s.platforms {
		if err := p.Do(ctx, adapters.Action{
			Type:        adapters.ActionSendMessage,
			Channel:     channel,
			SendMessage: &adapters.SendMessageAction{Text: message},
		}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent = true
	}
	if sent {
		return nil
	}
	return firstErr
}

// timerAdmin maps timers.Store onto the narrow commands.TimerStore the
// !addtimer/!deltimer/!timers built-ins need, binding the served tenant id.
type timerAdmin struct {
	tenantID string
	store    timers.Store
}

func (a timerAdmin) AddTimer(ctx context.Context, channel, name, message string, intervalSeconds, minChatLines int, createdBy string) error {
	_, err := a.store.Create(ctx, timers.Timer{
		TenantID:     a.tenantID,
		Channel:      channel,
		Name:         name,
		Message:      message,
		Interval:     time.Duration(intervalSeconds) * time.Second,
		MinChatLines: minChatLines,
		Enabled:      true,
		CreatedBy:    createdBy,
	})
	return err
}

func (a timerAdmin) RemoveTimer(ctx context.Context, channel, name string) error {
	return a.store.Delete(ctx, a.tenantID, channel, name)
}

func (a timerAdmin) ListTimers(ctx context.Context, channel string) ([]commands.TimerInfo, error) {
	rows, err := a.store.List(ctx, a.tenantID, channel)
	if err != nil {
		return nil, err
	}
	out := make([]commands.TimerInfo, len(rows))
	for i, t := range rows {
		out[i] = commands.TimerInfo{
			Name:            t.Name,
			IntervalSeconds: int(t.Interval / time.Second),
			Enabled:         t.Enabled,
		}
	}
	return out, nil
}

// quoteAdmin maps quotes.Store onto the narrow commands.QuoteStore the
// !addquote/!quote/!delquote built-ins need, binding the served tenant id
// and translating the store's sentinel errors into the (view, ok) shape the
// engine consumes so internal/commands stays free of any quotes import.
type quoteAdmin struct {
	tenantID string
	store    quotes.Store
}

func (a quoteAdmin) Add(ctx context.Context, channel, text, createdBy string) (int, error) {
	q, err := a.store.Add(ctx, a.tenantID, channel, text, createdBy)
	if err != nil {
		return 0, err
	}
	return q.Number, nil
}

func (a quoteAdmin) Get(ctx context.Context, channel string, number int) (commands.QuoteView, bool) {
	q, err := a.store.Get(ctx, a.tenantID, channel, number)
	if err != nil {
		return commands.QuoteView{}, false
	}
	return commands.QuoteView{Number: q.Number, Text: q.Text}, true
}

func (a quoteAdmin) Random(ctx context.Context, channel string) (commands.QuoteView, bool) {
	q, err := a.store.GetRandom(ctx, a.tenantID, channel)
	if err != nil {
		return commands.QuoteView{}, false
	}
	return commands.QuoteView{Number: q.Number, Text: q.Text}, true
}

func (a quoteAdmin) Delete(ctx context.Context, channel string, number int) error {
	return a.store.Delete(ctx, a.tenantID, channel, number)
}

// counterAdmin maps counters.Store onto the narrow commands.CounterStore the
// !counter/!counter+/!counter-/!setcounter/!resetcounter built-ins need,
// binding the served tenant id and translating a not-found into the (value,
// ok) shape so internal/commands stays free of any counters import.
type counterAdmin struct {
	tenantID string
	store    counters.Store
}

func (a counterAdmin) Value(ctx context.Context, channel, name string) (int64, bool) {
	c, err := a.store.Get(ctx, a.tenantID, channel, name)
	if err != nil {
		return 0, false
	}
	return c.Value, true
}

func (a counterAdmin) Add(ctx context.Context, channel, name string, delta int64) (int64, error) {
	c, err := a.store.Add(ctx, a.tenantID, channel, name, delta)
	if err != nil {
		return 0, err
	}
	return c.Value, nil
}

func (a counterAdmin) Set(ctx context.Context, channel, name string, value int64) (int64, error) {
	c, err := a.store.Set(ctx, a.tenantID, channel, name, value)
	if err != nil {
		return 0, err
	}
	return c.Value, nil
}

// channelPointsCounters adapts counterAdmin onto channelpoints.CounterAdmin:
// a Channel-Points "counter_increment" action bumps by one and
// "counter_reset" sets the counter back to zero, reusing the same counter
// store the chat !counter commands write to.
type channelPointsCounters struct{ admin counterAdmin }

func (c channelPointsCounters) Increment(ctx context.Context, channel, name string) (int64, error) {
	return c.admin.Add(ctx, channel, name, 1)
}

func (c channelPointsCounters) Reset(ctx context.Context, channel, name string) (int64, error) {
	return c.admin.Set(ctx, channel, name, 0)
}

// startChannelPoints wires the Channel-Points trigger engine (#13) and starts
// its EventSub WebSocket listener in the background. It is gated: custom
// rewards require an authenticated Helix client AND an affiliate/partner
// broadcaster, so anything short of that logs the reason and returns without
// starting the listener (the redemption store stays open for later dashboard
// configuration regardless). The listener's OnSession callback re-subscribes
// every channel on each (re)connect, since a dropped EventSub socket loses
// its subscriptions.
func startChannelPoints(
	ctx context.Context,
	logger *slog.Logger,
	tw *twitch.Adapter,
	store redemptions.Store,
	chat channelpoints.ChatSender,
	counters channelpoints.CounterAdmin,
	tenantID string,
	channels []string,
) {
	if tw == nil {
		logger.Info("channel points disabled", "reason", "twitch adapter not started")
		return
	}
	if len(channels) == 0 {
		logger.Info("channel points disabled", "reason", "no twitch channels configured")
		return
	}

	// The affiliate gate is checked against the broadcaster (first joined
	// channel). A non-affiliate channel cannot own custom rewards, so the
	// feature would only 403 — better to stay off and say why.
	broadcaster := channels[0]
	btype, err := tw.BroadcasterType(ctx, broadcaster)
	if err != nil {
		if errors.Is(err, twitch.ErrHelixUnavailable) {
			logger.Info("channel points disabled",
				"reason", "anonymous mode: needs Login-with-Twitch + channel:manage:redemptions scope")
		} else {
			logger.Warn("channel points disabled",
				"reason", "broadcaster type lookup failed", "channel", broadcaster, "err", err)
		}
		return
	}
	if btype != "affiliate" && btype != "partner" {
		logger.Info("channel points disabled",
			"reason", "channel is not affiliate or partner",
			"channel", broadcaster, "broadcaster_type", btype)
		return
	}

	exec := channelpoints.New(channelpoints.Config{
		TenantID:  tenantID,
		Store:     store,
		Chat:      chat,
		Counters:  counters,
		Fulfiller: tw,
		Logger:    logger,
	})

	client := eventsub.New(eventsub.Config{
		OnSession: func(ctx context.Context, sessionID string) error {
			var firstErr error
			for _, ch := range channels {
				if serr := tw.SubscribeRedemptions(ctx, ch, sessionID); serr != nil {
					logger.Warn("channel points subscribe failed", "channel", ch, "err", serr)
					if firstErr == nil {
						firstErr = serr
					}
				}
			}
			return firstErr
		},
		Handler: exec.Handle,
		Logger:  logger,
	})

	go func() {
		if rerr := client.Run(ctx); rerr != nil && !errors.Is(rerr, context.Canceled) {
			logger.Error("channel points eventsub listener exited", "err", rerr)
		}
	}()
	logger.Info("channel points enabled", "channels", channels, "broadcaster_type", btype)
}

// liveopsAdmin maps liveops.Store onto the narrow commands.EventStore the
// !nextevent/!schedule/!addevent/!delevent built-ins need, binding the
// served tenant id and projecting the rich liveops.Event onto the chat-
// facing commands.ScheduledEvent so internal/commands stays free of any
// liveops import.
type liveopsAdmin struct {
	tenantID string
	store    liveops.Store
}

// toScheduled projects a liveops.Event onto the chat-facing view, marking
// it Active when now sits between its start and (defined) end. now is taken
// once by the caller so a batch of events is judged against one clock.
func toScheduled(e liveops.Event, now time.Time) commands.ScheduledEvent {
	active := !e.StartsAt.After(now) && e.EndsAt != nil && !e.EndsAt.Before(now)
	return commands.ScheduledEvent{
		Number:   e.Number,
		Name:     e.Name,
		StartsAt: e.StartsAt,
		Active:   active,
	}
}

// Next returns the soonest upcoming-or-active event. It queries Upcoming
// with limit 1 (rather than the store's strictly-future Next) so that an
// in-progress event surfaces as Active and the command can say it is
// happening now instead of skipping straight to the following one.
func (a liveopsAdmin) Next(ctx context.Context, channel string) (commands.ScheduledEvent, bool, error) {
	now := time.Now().UTC()
	events, err := a.store.Upcoming(ctx, a.tenantID, channel, now, 1)
	if err != nil {
		return commands.ScheduledEvent{}, false, err
	}
	if len(events) == 0 {
		return commands.ScheduledEvent{}, false, nil
	}
	return toScheduled(events[0], now), true, nil
}

func (a liveopsAdmin) Upcoming(ctx context.Context, channel string, limit int) ([]commands.ScheduledEvent, error) {
	now := time.Now().UTC()
	events, err := a.store.Upcoming(ctx, a.tenantID, channel, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]commands.ScheduledEvent, 0, len(events))
	for _, e := range events {
		out = append(out, toScheduled(e, now))
	}
	return out, nil
}

func (a liveopsAdmin) Add(ctx context.Context, channel, name, description string, startsAt time.Time, endsAt *time.Time) (int, error) {
	e, err := a.store.Add(ctx, a.tenantID, channel, name, description, startsAt, endsAt)
	if err != nil {
		return 0, err
	}
	return e.Number, nil
}

func (a liveopsAdmin) Delete(ctx context.Context, channel string, number int) error {
	return a.store.Delete(ctx, a.tenantID, channel, number)
}

// errNoTwitchAdapter is returned by uptimeProvider when no Twitch adapter is
// configured, so the !uptime command renders its graceful error reply
// instead of crashing.
var errNoTwitchAdapter = errors.New("uptime: twitch adapter not configured")

// uptimeProvider maps the Twitch adapter's StreamInfo onto the narrow
// commands.UptimeProvider, keeping internal/commands free of any twitch
// import. A nil adapter (Twitch not configured) yields an error so the
// command replies "couldn't check uptime" rather than dereferencing nil.
// userProfileProvider adapts the Twitch adapter's UserProfile lookup to the
// commands.UserProfileProvider interface, translating the twitch profile type
// into the decoupled commands type.
type userProfileProvider struct{ adapter *twitch.Adapter }

func (p userProfileProvider) UserProfile(ctx context.Context, login string) (commands.UserProfile, error) {
	if p.adapter == nil {
		return commands.UserProfile{}, errNoTwitchAdapter
	}
	prof, err := p.adapter.UserProfile(ctx, login)
	if err != nil {
		return commands.UserProfile{}, err
	}
	return commands.UserProfile{
		Login:       prof.Login,
		DisplayName: prof.DisplayName,
		CreatedAt:   prof.CreatedAt,
	}, nil
}

// FollowAge implements commands.FollowAgeProvider, translating the twitch
// adapter's not-following sentinel into the command-facing one.
func (p userProfileProvider) FollowAge(ctx context.Context, channel, viewer string) (time.Time, error) {
	if p.adapter == nil {
		return time.Time{}, errNoTwitchAdapter
	}
	since, err := p.adapter.FollowAge(ctx, channel, viewer)
	if errors.Is(err, twitch.ErrNotFollowing) {
		return time.Time{}, commands.ErrNotFollowing
	}
	return since, err
}

type uptimeProvider struct{ adapter *twitch.Adapter }

func (p uptimeProvider) Uptime(ctx context.Context, channel string) (time.Time, bool, error) {
	if p.adapter == nil {
		return time.Time{}, false, errNoTwitchAdapter
	}
	info, err := p.adapter.StreamInfo(ctx, channel)
	if err != nil {
		return time.Time{}, false, err
	}
	return info.StartedAt, info.Live, nil
}

// streamStatusProvider maps the Twitch adapter's StreamInfo onto the narrow
// commands.StreamStatusProvider the !game/!title commands need. Like
// uptimeProvider it is nil-safe so a missing Twitch adapter yields a
// graceful error reply rather than a nil dereference.
type streamStatusProvider struct{ adapter *twitch.Adapter }

func (p streamStatusProvider) Status(ctx context.Context, channel string) (commands.StreamStatus, error) {
	if p.adapter == nil {
		return commands.StreamStatus{}, errNoTwitchAdapter
	}
	info, err := p.adapter.StreamInfo(ctx, channel)
	if err != nil {
		return commands.StreamStatus{}, err
	}
	return commands.StreamStatus{
		Live:        info.Live,
		GameName:    info.GameName,
		Title:       info.Title,
		ViewerCount: info.ViewerCount,
	}, nil
}

// commandRouterAdapter maps the commands.Engine onto runtime.CommandRouter,
// keeping the runtime package free of any commands import.
type commandRouterAdapter struct{ engine *commands.Engine }

func (a commandRouterAdapter) Route(ctx context.Context, inv runtime.CommandInvocation) (runtime.CommandReply, bool) {
	reply, handled := a.engine.Handle(ctx, commands.Message{
		Platform:      inv.Platform,
		Channel:       inv.Channel,
		UserID:        inv.UserID,
		Username:      inv.Username,
		Text:          inv.Text,
		IsBroadcaster: inv.IsBroadcaster,
		IsModerator:   inv.IsModerator,
		IsVIP:         inv.IsVIP,
		IsSubscriber:  inv.IsSubscriber,
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

// leaderboardQuerier maps the pity and streak systems onto
// commands.LeaderboardQuerier, projecting each board's ranking metric
// (pity points / current streak days) into the decoupled Score field.
type leaderboardQuerier struct {
	pity   *pity.System
	streak *streak.System
}

func (q leaderboardQuerier) PityTop(tenantID, channel string, n int) []commands.LeaderboardEntry {
	rows := q.pity.Leaderboard(tenantID, channel, n)
	out := make([]commands.LeaderboardEntry, len(rows))
	for i, r := range rows {
		out[i] = commands.LeaderboardEntry{Username: r.Username, Score: r.Points}
	}
	return out
}

func (q leaderboardQuerier) StreakTop(tenantID, channel string, n int) []commands.LeaderboardEntry {
	rows := q.streak.Leaderboard(tenantID, channel, n)
	out := make([]commands.LeaderboardEntry, len(rows))
	for i, r := range rows {
		out[i] = commands.LeaderboardEntry{Username: r.Username, Score: r.DaysCurrent}
	}
	return out
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

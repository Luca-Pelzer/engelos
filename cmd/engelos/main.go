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
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/actions/runs"
	"github.com/Luca-Pelzer/engelos/internal/adapters"
	"github.com/Luca-Pelzer/engelos/internal/adapters/discord"
	"github.com/Luca-Pelzer/engelos/internal/adapters/kick"
	"github.com/Luca-Pelzer/engelos/internal/adapters/obs"
	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch"
	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch/eventsub"
	ytadapter "github.com/Luca-Pelzer/engelos/internal/adapters/youtube"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/aiconfig"
	"github.com/Luca-Pelzer/engelos/internal/aibackend/usage"
	"github.com/Luca-Pelzer/engelos/internal/api"
	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/api/ws"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/automod"
	"github.com/Luca-Pelzer/engelos/internal/automodstate"
	"github.com/Luca-Pelzer/engelos/internal/avatar"
	"github.com/Luca-Pelzer/engelos/internal/channelpoints"
	"github.com/Luca-Pelzer/engelos/internal/clipper"
	"github.com/Luca-Pelzer/engelos/internal/cohost"
	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/Luca-Pelzer/engelos/internal/contextmod"
	"github.com/Luca-Pelzer/engelos/internal/counters"
	"github.com/Luca-Pelzer/engelos/internal/customcommands"
	"github.com/Luca-Pelzer/engelos/internal/eventsourcing"
	"github.com/Luca-Pelzer/engelos/internal/featureflags"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/integrations"
	"github.com/Luca-Pelzer/engelos/internal/integrations/builtins"
	"github.com/Luca-Pelzer/engelos/internal/integrations/credstore"
	"github.com/Luca-Pelzer/engelos/internal/kb"
	"github.com/Luca-Pelzer/engelos/internal/liveops"
	"github.com/Luca-Pelzer/engelos/internal/loyalty"
	"github.com/Luca-Pelzer/engelos/internal/moderation"
	"github.com/Luca-Pelzer/engelos/internal/moments"
	"github.com/Luca-Pelzer/engelos/internal/oauthrefresh"
	"github.com/Luca-Pelzer/engelos/internal/overlay"
	"github.com/Luca-Pelzer/engelos/internal/plugins"
	"github.com/Luca-Pelzer/engelos/internal/quotes"
	"github.com/Luca-Pelzer/engelos/internal/redemptions"
	"github.com/Luca-Pelzer/engelos/internal/rewards"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
	"github.com/Luca-Pelzer/engelos/internal/sdkbridge"
	"github.com/Luca-Pelzer/engelos/internal/secrets"
	"github.com/Luca-Pelzer/engelos/internal/server"
	"github.com/Luca-Pelzer/engelos/internal/songrequests"
	"github.com/Luca-Pelzer/engelos/internal/songrequests/queue"
	"github.com/Luca-Pelzer/engelos/internal/songrequests/spotify"
	"github.com/Luca-Pelzer/engelos/internal/songrequests/youtube"
	"github.com/Luca-Pelzer/engelos/internal/streamstate"
	"github.com/Luca-Pelzer/engelos/internal/timers"
	"github.com/Luca-Pelzer/engelos/internal/translate"
	"github.com/Luca-Pelzer/engelos/internal/tts"
	"github.com/Luca-Pelzer/engelos/internal/web"
	"github.com/Luca-Pelzer/engelos/internal/workspaces"
	"github.com/Luca-Pelzer/engelos/internal/wrapped"
	"github.com/Luca-Pelzer/engelos/pkg/sdk/examples/greeter"
	"github.com/coder/websocket"
	"github.com/nicklaw5/helix/v2"
	"golang.org/x/oauth2"
	spotifyoauth "golang.org/x/oauth2/spotify"
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
		"phase", "1B - adapters + auth + web + dispatcher",
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

	// Plugin boundary (Phase 4.1): a restart-based enable/disable layer over the
	// legacy nil-guarded-store pattern. The state store persists desired toggles;
	// Active resolves what this process mounts, gating the pilot (quotes) store
	// and its commands below. Defaults come from each manifest, preserving
	// today's per-feature defaults exactly (quotes stays default-on).
	pluginsDSN := filepath.Join(dataDir, "plugins.db")
	pluginState, err := plugins.OpenSQLiteStore(ctx, pluginsDSN, logger)
	if err != nil {
		return fmt.Errorf("open plugin state store %s: %w", pluginsDSN, err)
	}
	defer func() {
		if cerr := pluginState.Close(); cerr != nil {
			logger.Warn("plugin state store close failed", "err", cerr)
		}
	}()
	pluginRegistry := plugins.NewRegistry()
	// The trailing entries are compiled-in SDK extensions (third-party nodes and
	// plugins built against pkg/sdk, wired through internal/sdkbridge). The
	// greeter example ships default-off; add new SDK plugins to this list.
	for _, p := range []plugins.Plugin{
		plugins.Quotes(), plugins.Counters(), plugins.Loyalty(), plugins.Pity(), plugins.Streak(),
		plugins.Liveops(), plugins.Wrapped(), plugins.Moments(), plugins.Songrequests(),
		sdkbridge.SDKPlugin(greeter.Plugin()),
	} {
		if rerr := pluginRegistry.Register(p); rerr != nil {
			logger.Warn("plugin register failed", "err", rerr)
		}
	}
	pluginsActive, err := pluginRegistry.Active(ctx, pluginState, defaultTenantID)
	if err != nil {
		return fmt.Errorf("resolve active plugins: %w", err)
	}
	logger.Info("plugin state store opened", "dsn", pluginsDSN, "active", pluginsActive)

	// quotes is the pilot plugin: its store, routes and chat commands only wire
	// when the plugin is active. Disabled => nil store => routes 404 and the
	// quote commands are never registered (see buildCommandRouter).
	var quoteStore quotes.Store
	if pluginsActive["quotes"] {
		quotesDSN := filepath.Join(dataDir, "quotes.db")
		qs, oerr := quotes.OpenSQLiteStore(ctx, quotesDSN, logger)
		if oerr != nil {
			return fmt.Errorf("open quote store %s: %w", quotesDSN, oerr)
		}
		defer func() {
			if cerr := qs.Close(); cerr != nil {
				logger.Warn("quote store close failed", "err", cerr)
			}
		}()
		quoteStore = qs
		logger.Info("quote store opened", "dsn", quotesDSN)
	} else {
		logger.Info("quotes plugin disabled — store not opened; routes 404, !quote/!addquote/!delquote unregistered")
	}

	// counters plugin: gated like quotes. Disabled => nil store => routes 404,
	// !counter commands unregistered, and the Channel-Points counter actions
	// degrade to an error instead of writing (see counterAdmin).
	var counterStore counters.Store
	if pluginsActive["counters"] {
		countersDSN := filepath.Join(dataDir, "counters.db")
		cs, oerr := counters.OpenSQLiteStore(ctx, countersDSN, logger)
		if oerr != nil {
			return fmt.Errorf("open counter store %s: %w", countersDSN, oerr)
		}
		defer func() {
			if cerr := cs.Close(); cerr != nil {
				logger.Warn("counter store close failed", "err", cerr)
			}
		}()
		counterStore = cs
		logger.Info("counter store opened", "dsn", countersDSN)
	} else {
		logger.Info("counters plugin disabled — store not opened; routes 404, !counter commands unregistered")
	}

	// liveops plugin (template tier): gated like quotes. Disabled => nil store =>
	// routes 404 and !nextevent/!schedule/!addevent/!delevent unregistered.
	var eventStoreLO liveops.Store
	if pluginsActive["liveops"] {
		liveopsDSN := filepath.Join(dataDir, "liveops.db")
		los, oerr := liveops.OpenSQLiteStore(ctx, liveopsDSN, logger)
		if oerr != nil {
			return fmt.Errorf("open liveops store %s: %w", liveopsDSN, oerr)
		}
		defer func() {
			if cerr := los.Close(); cerr != nil {
				logger.Warn("liveops store close failed", "err", cerr)
			}
		}()
		eventStoreLO = los
		logger.Info("liveops store opened", "dsn", liveopsDSN)
	} else {
		logger.Info("liveops plugin disabled — store not opened; routes 404, !nextevent/!schedule/!addevent/!delevent unregistered")
	}

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

	// loyalty plugin: gated like quotes. Disabled => nil store => routes 404 and
	// the !points/!give commands unregistered. The economy adapter still wraps
	// the nil store (it self-guards), so per-message earning and the games
	// degrade to "unavailable" rather than panicking.
	var loyaltyStore loyalty.Store
	if pluginsActive["loyalty"] {
		loyaltyDSN := filepath.Join(dataDir, "loyalty.db")
		ls, oerr := loyalty.OpenSQLiteStore(ctx, loyaltyDSN, logger)
		if oerr != nil {
			return fmt.Errorf("open loyalty store %s: %w", loyaltyDSN, oerr)
		}
		defer func() {
			if cerr := ls.Close(); cerr != nil {
				logger.Warn("loyalty store close failed", "err", cerr)
			}
		}()
		loyaltyStore = ls
		logger.Info("loyalty store opened", "dsn", loyaltyDSN)
	} else {
		logger.Info("loyalty plugin disabled — store not opened; routes 404, !points/!give unregistered, games degrade to unavailable")
	}

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

	// songrequests is quarantined default-off (RES-19, Option A). It is now a
	// plugin: the plugin toggle is the source of truth, while the legacy
	// ENGELOS_FEATURE_SONGREQUESTS env flag still forces it on for backward
	// compatibility (a box that sets the flag behaves exactly as before). When
	// off, the SQLite stores are never opened, leaving songRequestStore and
	// songQueueStore nil. That single nil cascades to every songrequests surface
	// without deleting a line of working code: the /api/v1/songrequests and
	// /api/v1/songqueue routes stay unmounted (404), the !sr / !song / !skipsong
	// commands are not registered, the now-playing overlay poller does not
	// start, and the dashboard "Music Plugin" card is filtered out via
	// /api/v1/capabilities. Enable the plugin (or set the flag) to restore the
	// feature exactly as before (proven reversible).
	var (
		songRequestStore songrequests.Store
		songQueueStore   queue.Store
	)
	songRequestsEnabled := songRequestsActive(pluginsActive["songrequests"], envBool("ENGELOS_FEATURE_SONGREQUESTS"))
	if songRequestsEnabled {
		songRequestDSN := filepath.Join(dataDir, "songrequests.db")
		srStore, oerr := songrequests.OpenSQLiteStore(ctx, songRequestDSN, logger)
		if oerr != nil {
			return fmt.Errorf("open song request store %s: %w", songRequestDSN, oerr)
		}
		defer func() {
			if cerr := srStore.Close(); cerr != nil {
				logger.Warn("song request store close failed", "err", cerr)
			}
		}()
		songRequestStore = srStore
		logger.Info("song request store opened", "dsn", songRequestDSN)

		songQueueDSN := filepath.Join(dataDir, "songqueue.db")
		sqStore, oerr := queue.OpenSQLiteStore(ctx, songQueueDSN, logger)
		if oerr != nil {
			return fmt.Errorf("open song queue store %s: %w", songQueueDSN, oerr)
		}
		defer func() {
			if cerr := sqStore.Close(); cerr != nil {
				logger.Warn("song queue store close failed", "err", cerr)
			}
		}()
		songQueueStore = sqStore
		logger.Info("song queue store opened", "dsn", songQueueDSN)
	} else {
		logger.Info("songrequests feature disabled (ENGELOS_FEATURE_SONGREQUESTS off) — stores not opened; routes 404, !sr/!song/!skipsong unregistered, overlay poller off")
	}

	// wrapped plugin (template tier): gated like quotes. Disabled => nil store =>
	// the public /wrapped card 404s and the dispatcher records nothing (its
	// Wrapped recorder is left unset below so it never dereferences a nil store).
	var wrappedStore wrapped.Store
	if pluginsActive["wrapped"] {
		wrappedDSN := filepath.Join(dataDir, "wrapped.db")
		ws, oerr := wrapped.OpenSQLiteStore(ctx, wrappedDSN, logger)
		if oerr != nil {
			return fmt.Errorf("open wrapped store %s: %w", wrappedDSN, oerr)
		}
		defer func() {
			if cerr := ws.Close(); cerr != nil {
				logger.Warn("wrapped store close failed", "err", cerr)
			}
		}()
		wrappedStore = ws
		logger.Info("wrapped store opened", "dsn", wrappedDSN)
	} else {
		logger.Info("wrapped plugin disabled — store not opened; /wrapped 404, no recap recording")
	}

	// moments plugin (template tier): gated like quotes. Disabled => nil store =>
	// routes 404 and !moment/!here unregistered (momentCtrl is left a nil
	// interface below so the command router skips them).
	var momentsStore moments.Store
	if pluginsActive["moments"] {
		momentsDSN := filepath.Join(dataDir, "moments.db")
		ms, oerr := moments.OpenSQLiteStore(ctx, momentsDSN, logger)
		if oerr != nil {
			return fmt.Errorf("open moments store %s: %w", momentsDSN, oerr)
		}
		defer func() {
			if cerr := ms.Close(); cerr != nil {
				logger.Warn("moments store close failed", "err", cerr)
			}
		}()
		momentsStore = ms
		logger.Info("moments store opened", "dsn", momentsDSN)
	} else {
		logger.Info("moments plugin disabled — store not opened; routes 404, !moment/!here unregistered")
	}

	translateDSN := filepath.Join(dataDir, "translate.db")
	translateStore, err := translate.OpenSQLiteStore(ctx, translateDSN)
	if err != nil {
		return fmt.Errorf("open translate store %s: %w", translateDSN, err)
	}
	defer func() {
		if cerr := translateStore.Close(); cerr != nil {
			logger.Warn("translate store close failed", "err", cerr)
		}
	}()
	logger.Info("translate store opened", "dsn", translateDSN)

	clipperDSN := filepath.Join(dataDir, "clipper.db")
	clipperStore, err := clipper.OpenSQLiteStore(ctx, clipperDSN)
	if err != nil {
		return fmt.Errorf("open clipper store %s: %w", clipperDSN, err)
	}
	defer func() {
		if cerr := clipperStore.Close(); cerr != nil {
			logger.Warn("clipper store close failed", "err", cerr)
		}
	}()
	logger.Info("clipper store opened", "dsn", clipperDSN)

	cohostDSN := filepath.Join(dataDir, "cohost.db")
	cohostStore, err := cohost.OpenSQLiteStore(ctx, cohostDSN)
	if err != nil {
		return fmt.Errorf("open cohost store %s: %w", cohostDSN, err)
	}
	defer func() {
		if cerr := cohostStore.Close(); cerr != nil {
			logger.Warn("cohost store close failed", "err", cerr)
		}
	}()
	logger.Info("cohost store opened", "dsn", cohostDSN)

	ttsDSN := filepath.Join(dataDir, "tts.db")
	ttsStore, err := tts.OpenSQLiteStore(ctx, ttsDSN)
	if err != nil {
		return fmt.Errorf("open tts store %s: %w", ttsDSN, err)
	}
	defer func() {
		if cerr := ttsStore.Close(); cerr != nil {
			logger.Warn("tts store close failed", "err", cerr)
		}
	}()
	logger.Info("tts store opened", "dsn", ttsDSN)

	avatarDSN := filepath.Join(dataDir, "avatar.db")
	avatarStore, err := avatar.OpenSQLiteStore(ctx, avatarDSN)
	if err != nil {
		return fmt.Errorf("open avatar store %s: %w", avatarDSN, err)
	}
	defer func() {
		if cerr := avatarStore.Close(); cerr != nil {
			logger.Warn("avatar store close failed", "err", cerr)
		}
	}()
	avatarHub := avatar.NewHub(logger)
	logger.Info("avatar store opened", "dsn", avatarDSN)

	contextmodDSN := filepath.Join(dataDir, "contextmod.db")
	contextmodStore, err := contextmod.OpenSQLiteStore(ctx, contextmodDSN)
	if err != nil {
		return fmt.Errorf("open contextmod store %s: %w", contextmodDSN, err)
	}
	defer func() {
		if cerr := contextmodStore.Close(); cerr != nil {
			logger.Warn("contextmod store close failed", "err", cerr)
		}
	}()
	logger.Info("contextmod store opened", "dsn", contextmodDSN)

	actionsDSN := filepath.Join(dataDir, "actions.db")
	actionsStore, err := actions.OpenSQLiteStore(ctx, actionsDSN, logger)
	if err != nil {
		return fmt.Errorf("open actions store %s: %w", actionsDSN, err)
	}
	defer func() {
		if cerr := actionsStore.Close(); cerr != nil {
			logger.Warn("actions store close failed", "err", cerr)
		}
	}()
	logger.Info("actions store opened", "dsn", actionsDSN)

	workspacesDSN := filepath.Join(dataDir, "workspaces.db")
	workspacesStore, err := workspaces.OpenSQLiteStore(ctx, workspacesDSN, logger)
	if err != nil {
		return fmt.Errorf("open workspaces store %s: %w", workspacesDSN, err)
	}
	defer func() {
		if cerr := workspacesStore.Close(); cerr != nil {
			logger.Warn("workspaces store close failed", "err", cerr)
		}
	}()
	logger.Info("workspaces store opened", "dsn", workspacesDSN)

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
	logger.Info("automod ready", "mode", "dry-run (shadow; all filters disabled by default)")

	// pity plugin (template tier): gated on the active snapshot. Disabled => nil
	// system => the /pity routes 404, !pity is unregistered, and the dispatcher
	// skips per-message pity grants (its Pity/PointsPerMessage stay unset). The
	// nil system is never passed into an interface so the dispatcher's
	// nil-checks see a genuine nil.
	var pitySystem *pity.System
	if pluginsActive["pity"] {
		pitySystem, err = pity.New(pity.DefaultConfig(), eventStore, logger)
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
	} else {
		logger.Info("pity plugin disabled — system not built; routes 404, !pity unregistered, no per-message pity grants")
	}

	// streak plugin (template tier): gated on the active snapshot. Disabled =>
	// nil system => the /streak routes 404, !streak is unregistered, and the
	// dispatcher skips per-message streak ticks (its Streak field stays unset so
	// it never dereferences a nil-wrapping adapter).
	var streakSystem *streak.System
	if pluginsActive["streak"] {
		streakSystem, err = streak.New(streak.DefaultConfig(), eventStore, logger)
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
	} else {
		logger.Info("streak plugin disabled — system not built; routes 404, !streak unregistered, no per-message streak ticks")
	}

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
	} else if allowLAN {
		logger.Warn("ENGELOS_ALLOW_LAN is set but ENGELOS_ALLOWED_ORIGINS is empty; " +
			"the WebSocket accepts any Origin. Set ENGELOS_ALLOWED_ORIGINS to your " +
			"dashboard host (e.g. bot.engels.wtf) to restrict cross-origin handshakes.")
	}
	hub := ws.NewHub(logger, hubOpts...)
	go hub.Run(ctx)

	platforms, twitchAdapter, discordAdapter, cleanupPlatforms := startPlatforms(ctx, logger, authStore, defaultTenantID)
	defer cleanupPlatforms()

	economy.withResolver(userProfileProvider{adapter: twitchAdapter}.UserProfile)

	// songRequester stays a nil commands.SongRequester interface when the
	// feature is off, so buildCommandRouter's nil-check leaves !sr / !song /
	// !skipsong unregistered. When on it is the multi-provider requester,
	// identical to prior behaviour.
	var songRequester commands.SongRequester
	if songRequestsEnabled {
		spotifyReq := spotifyRequester{
			cfg:      songRequestStore,
			client:   spotify.New(spotify.WithLogger(logger)),
			auth:     authStore,
			tenantID: defaultTenantID,
			logger:   logger,
		}
		youtubeReq := youtubeRequester{
			cfg:      songRequestStore,
			queue:    songQueueStore,
			client:   youtube.New(os.Getenv("ENGELOS_YOUTUBE_API_KEY"), youtube.WithLogger(logger)),
			tenantID: defaultTenantID,
			logger:   logger,
		}
		songRequester = multiProviderRequester{
			cfg:      songRequestStore,
			spotify:  spotifyReq,
			youtube:  youtubeReq,
			tenantID: defaultTenantID,
			logger:   logger,
		}

		// Now-playing overlay poller: when Spotify is configured, poll the
		// streamer's channels and push song.now_playing events to the WS hub so
		// the /overlay/now-playing OBS source updates live.
		if nowPlayingChannels := splitCSV(os.Getenv("ENGELOS_TWITCH_CHANNELS")); len(nowPlayingChannels) > 0 {
			poller := &nowPlayingPoller{
				req:      songRequester,
				sink:     hub,
				channels: nowPlayingChannels,
				logger:   logger,
			}
			go poller.run(ctx)
			logger.Info("now-playing overlay poller started", "channels", len(nowPlayingChannels))
		}
	}

	// moments pilot: a nil momentController interface (plugin disabled) leaves
	// !moment/!here unregistered. A momentController value wrapping a nil store
	// would be a non-nil interface that dereferences the nil store on first use.
	var momentCtrl commands.MomentController
	if momentsStore != nil {
		momentCtrl = newMomentController(momentsStore, runtime.NewWSBroadcaster(hub, logger), defaultTenantID, logger)
	}

	aiConfigDSN := filepath.Join(dataDir, "ai_config.db")
	aiConfigStore, err := aiconfig.OpenSQLiteStore(ctx, aiConfigDSN)
	if err != nil {
		return fmt.Errorf("open ai config store %s: %w", aiConfigDSN, err)
	}
	defer func() {
		if cerr := aiConfigStore.Close(); cerr != nil {
			logger.Warn("ai config store close failed", "err", cerr)
		}
	}()
	logger.Info("ai config store opened", "dsn", aiConfigDSN)

	// In-memory AI usage counters, exposed at GET /api/v1/ai/usage. Each consumer
	// gets the backend wrapped with its own label so call and token counts are
	// attributed automatically.
	aiUsage := usage.NewRegistry()

	// One hot-swappable backend (DB config > env > defaults) feeds every AI consumer below.
	aiManager, err := aiconfig.NewManager(ctx, aiConfigStore, aiCrypto(cryptoBox), defaultTenantID, logger,
		aiconfig.WithUsageRegistry(aiUsage))
	if err != nil {
		return fmt.Errorf("init ai backend manager: %w", err)
	}
	aiBackend := aiManager
	translateCfg := translateConfigAdapter{store: translateStore, tenantID: defaultTenantID, logger: logger}
	msgTranslator := newMessageTranslator(translateStore, usage.Wrap(aiBackend, aiUsage, "translate"), defaultTenantID, logger)
	cohostCfg := cohostConfigAdapter{store: cohostStore, tenantID: defaultTenantID, logger: logger}
	coHost := newCoHostResponder(cohostStore, usage.Wrap(aiBackend, aiUsage, "cohost"), defaultTenantID, logger)
	autoClip := newAutoClipper(clipperStore, clipper.DefaultOptions(), defaultTenantID,
		twitchAdapter, usage.Wrap(aiBackend, aiUsage, "clipper"), platformSender{platforms: platforms},
		splitCSV(os.Getenv("ENGELOS_CLIPPER_CHANNELS")), logger)

	// Context-AI moderation resolves its rules per channel from the contextmod
	// store, falling back to the global ENGELOS_CONTEXTMOD_RULES env value when
	// a channel has no row. The escalator is always created; escalation simply
	// no-ops for channels whose resolved rules are empty, so the dashboard can
	// turn it on per channel without a restart. It escalates only messages the
	// rule engine passed, and fails open to the existing behaviour.
	contextEnvRules := strings.TrimSpace(os.Getenv("ENGELOS_CONTEXTMOD_RULES"))
	contextModOpts := contextmod.DefaultOptions()
	contextModOpts.TenantID = defaultTenantID
	contextEscalator := contextmod.NewEscalator(usage.Wrap(aiBackend, aiUsage, "contextmod"), contextModOpts)
	contextRules := &contextRulesProvider{
		store:    contextmodStore,
		tenantID: defaultTenantID,
		envRules: contextEnvRules,
		logger:   logger,
	}

	// When loyalty is disabled the command router gets a nil provider, so the
	// !points/!give commands and the economy games are left unregistered; the
	// dispatcher still gets the self-guarding economy adapter for earning.
	var loyaltyCmdProvider commands.LoyaltyProvider
	if loyaltyStore != nil {
		loyaltyCmdProvider = economy
	}
	cmdRouter := buildCommandRouter(defaultTenantID, pitySystem, streakSystem, customStore, timerStore, quoteStore, counterStore, eventStoreLO, twitchAdapter, loyaltyCmdProvider, platformSender{platforms: platforms}, rewardCatalog, featureGateAdapter{store: featureFlagStore, tenantID: defaultTenantID}, predictionController{adapter: twitchAdapter, logger: logger}, songRequester, momentCtrl, translateCfg, cohostCfg, logger)

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

	var obsController actions.OBSController
	if obsHost := strings.TrimSpace(os.Getenv("ENGELOS_OBS_HOST")); obsHost != "" {
		ctl := obs.New(obs.Config{Host: obsHost, Password: os.Getenv("ENGELOS_OBS_PASSWORD")})
		defer func() {
			if cerr := ctl.Close(); cerr != nil {
				logger.Warn("obs controller close failed", "err", cerr)
			}
		}()
		obsController = ctl
		logger.Info("obs control enabled", "host", obsHost)
	}

	var ttsService *tts.Service
	if cryptoBox != nil {
		ttsService = tts.NewService(ttsStore, cryptoBox,
			runtime.NewWSBroadcaster(hub, logger), defaultTenantID, logger)
	} else {
		logger.Info("tts disabled: no secrets key configured")
	}

	var actionsTTS actions.TTSSpeaker
	if ttsService != nil {
		actionsTTS = ttsService
	}
	var actionsAvatarSynth actions.AvatarSynth
	if ttsService != nil {
		actionsAvatarSynth = ttsService
	}
	var actionsDiscord actions.DiscordPoster
	if discordAdapter != nil {
		actionsDiscord = discordPostAdapter{adapter: discordAdapter}
	}
	var actionsTwitchMod actions.TwitchModerator
	var actionsTwitchChan actions.TwitchChannel
	if twitchAdapter != nil {
		actionsTwitchMod = twitchModAdapter{adapter: twitchAdapter}
		actionsTwitchChan = twitchChannelAdapter{adapter: twitchAdapter}
	}

	// streamState tracks per-channel live/offline state, fed by the Twitch
	// EventSub stream.online/stream.offline handler; cond:stream-state reads it.
	streamState := streamstate.New()

	// Run history recorder (Phase 2.2). ENGELOS_RUNS_RETENTION caps stored runs
	// per (tenant, channel); 0 disables recording entirely (nil recorder + no
	// store, so the run-history API routes stay unmounted).
	var runsRecorder actions.RunRecorder
	var runsSource handlers.RunSource
	runsRetention := 500
	if raw := strings.TrimSpace(os.Getenv("ENGELOS_RUNS_RETENTION")); raw != "" {
		if n, perr := strconv.Atoi(raw); perr == nil && n >= 0 {
			runsRetention = n
		}
	}
	if runsRetention > 0 {
		runsDSN := filepath.Join(dataDir, "runs.db")
		runsStore, rerr := runs.OpenStore(ctx, runsDSN, runsRetention, logger)
		if rerr != nil {
			return fmt.Errorf("open runs store %s: %w", runsDSN, rerr)
		}
		defer func() {
			if cerr := runsStore.Close(); cerr != nil {
				logger.Warn("runs store close failed", "err", cerr)
			}
		}()
		runsRecorder = runsStore
		runsSource = runsStore
		logger.Info("run history enabled", "dsn", runsDSN, "retention", runsRetention)
	} else {
		logger.Info("run history disabled", "reason", "ENGELOS_RUNS_RETENTION=0")
	}

	kbDSN := filepath.Join(dataDir, "kb.db")
	kbStore, err := kb.OpenSQLiteStore(ctx, kbDSN, logger)
	if err != nil {
		return fmt.Errorf("open kb store %s: %w", kbDSN, err)
	}
	defer func() {
		if cerr := kbStore.Close(); cerr != nil {
			logger.Warn("kb store close failed", "err", cerr)
		}
	}()
	logger.Info("kb store opened", "dsn", kbDSN)

	// actionsAI powers the ai:generate/ai:classify nodes, which the AI-backend
	// integration contributes below (the engine no longer registers them).
	actionsAI := usage.Wrap(aiBackend, aiUsage, "actions")

	actionsEngine, actionsRuleEngine, actionsRegistry, actionsStop, err := newActionsEngine(actionsStore,
		platformSender{platforms: platforms}, actionsDiscord,
		actionsTwitchMod, actionsTwitchChan, streamState, kbSearchAdapter{store: kbStore}, runsRecorder, defaultTenantID, logger)
	if err != nil {
		return fmt.Errorf("init actions engine: %w", err)
	}
	defer actionsStop()
	logger.Info("actions engine started")

	// Integrations framework: every integration declares its manifest and
	// contributes its workflow nodes via RegisterAllNodes below. The three
	// first-party integrations always register so their nodes stay in the
	// catalog regardless of config; each reports "connected" from its own
	// source (TTS store, OBS wiring, AI config) through a probe.
	integrationsRegistry := integrations.NewRegistry()
	registerIntegration := func(i integrations.Integration) {
		if rerr := integrationsRegistry.Register(i); rerr != nil {
			logger.Warn("integration register failed", "err", rerr)
		}
	}
	registerIntegration(builtins.ElevenLabs(actionsTTS, func(pctx context.Context, tenant string) bool {
		if ttsStore == nil {
			return false
		}
		cfgs, cerr := ttsStore.List(pctx, tenant)
		if cerr != nil {
			return false
		}
		for _, c := range cfgs {
			if len(c.APIKeyCiphertext) > 0 {
				return true
			}
		}
		return false
	}))
	registerIntegration(builtins.OBS(obsController, func(context.Context, string) bool {
		return obsController != nil
	}))
	registerIntegration(builtins.AIBackend(actionsAI, func(context.Context, string) bool {
		snap := aiManager.Snapshot()
		return snap.APIKeySet && snap.Provider != ""
	}))
	registerIntegration(builtins.Kofi())
	registerIntegration(builtins.Discord(actionsDiscord, func(context.Context, string) bool {
		return discordAdapter != nil && discordAdapter.Health() == nil
	}))
	registerIntegration(builtins.Avatar(avatarHub, actionsAvatarSynth, func(context.Context, string) bool {
		return avatarHub.SubscriberCount() > 0
	}))
	if err := integrationsRegistry.RegisterAllNodes(actionsRegistry); err != nil {
		return fmt.Errorf("register integration nodes: %w", err)
	}
	// Compiled-in SDK extension nodes (built against pkg/sdk, wired via the
	// bridge). The greeter example's node joins the catalog only when its
	// default-off plugin is enabled, matching the plugin-boundary pattern.
	if pluginsActive["greeter"] {
		if rerr := sdkbridge.RegisterSDKAction(actionsRegistry, greeter.HelloAction()); rerr != nil {
			logger.Warn("sdk action register failed", "id", "greeter:hello", "err", rerr)
		}
	}
	integrationCreds, err := credstore.OpenStore(ctx,
		filepath.Join(dataDir, "integrations.db"), integrationsCrypto(cryptoBox), logger)
	if err != nil {
		return fmt.Errorf("open integrations credential store: %w", err)
	}
	defer func() {
		if cerr := integrationCreds.Close(); cerr != nil {
			logger.Warn("integrations credential store close failed", "err", cerr)
		}
	}()
	logger.Info("integrations framework enabled")

	// The timer scheduler arms one ticker per enabled timer rule and fires it
	// through the engine; it is re-armed by the actions handler after any rule
	// mutation. Stop drains its goroutines at shutdown before the engine stops.
	actionsScheduler := actions.NewScheduler(actionsRuleEngine, actionsStore, defaultTenantID, logger)
	if serr := actionsScheduler.Start(ctx); serr != nil {
		logger.Warn("actions timer scheduler start failed", "err", serr)
	}
	defer actionsScheduler.Stop()
	logger.Info("actions timer scheduler started")

	// Channel-Points trigger engine (#13). Gated: it only starts when the
	// Twitch adapter is authenticated (Helix available) AND the broadcaster
	// is an affiliate/partner; custom rewards 403 otherwise. In every other
	// case it logs the reason and stays a no-op, so anonymous/non-affiliate
	// deployments boot cleanly with the rest of the bot unaffected. It runs
	// after the Action-Engine so a redemption can also fire dashboard rules.
	startChannelPoints(ctx, logger, twitchAdapter, redemptionStore,
		platformSender{platforms: platforms},
		channelPointsCounters{admin: counterAdmin{tenantID: defaultTenantID, store: counterStore}},
		actionsEngine,
		overlayRedemptionNotifier{bc: runtime.NewWSBroadcaster(hub, logger)},
		channelPointsSpeaker(ttsService),
		streamState,
		defaultTenantID, splitCSV(os.Getenv("ENGELOS_TWITCH_CHANNELS")))

	// Pity/PointsPerMessage and Streak are set only when their plugin is on.
	// Reading pitySystem.Config() on a nil system would panic; assigning a nil
	// *pity.System into the Pity interface, or a streakTickAdapter wrapping a nil
	// system into the Streak interface, would present a non-nil interface and
	// defeat the dispatcher's nil-check (then panic on first use). Leaving them
	// unset makes the dispatcher skip the corresponding per-message grant/tick.
	dispatcherCfg := runtime.Config{
		TenantID:      defaultTenantID,
		Platforms:     platforms,
		Broadcaster:   runtime.NewWSBroadcaster(hub, logger),
		TTS:           ttsNotifier(ttsService),
		Commands:      cmdRouter,
		Moderator:     moderationAdapter{svc: moderationSvc, escalator: contextEscalator, rules: contextRules},
		Economy:       economy,
		Activity:      timerScheduler,
		Translator:    msgTranslator,
		CoHost:        coHost,
		CoHostSpeaker: cohostSpeaker(ttsService),
		ClipDetector:  autoClip,
		Actions:       actionsEngine,
		Logger:        logger,
	}
	if pitySystem != nil {
		dispatcherCfg.Pity = pitySystem
		dispatcherCfg.PointsPerMessage = pitySystem.Config().PointsPerMessage
	}
	if streakSystem != nil {
		dispatcherCfg.Streak = streakTickAdapter{sys: streakSystem}
	}
	// Wrapped recording is set only when the plugin is on: newWrappedRecorder
	// wrapping a nil store would present a non-nil recorder to the dispatcher's
	// nil-check and then dereference the nil store on the first event.
	if wrappedStore != nil {
		dispatcherCfg.Wrapped = newWrappedRecorder(wrappedStore, defaultTenantID, logger)
	}
	// Discord messages carry no native workspace channel, so they fan out to
	// every channel with an enabled discord.message rule (same store method the
	// Ko-fi donation fan-out uses). Only wired when the Discord bot is running.
	if discordAdapter != nil {
		dispatcherCfg.DiscordChannels = func(fctx context.Context) []string {
			chans, cerr := actionsStore.ListEventChannels(fctx, defaultTenantID, string(adapters.EventDiscordMessage))
			if cerr != nil {
				logger.Warn("discord channel resolve failed", "err", cerr)
				return nil
			}
			return chans
		}
	}
	dispatcher := runtime.New(dispatcherCfg)
	go func() {
		if err := dispatcher.Run(ctx); err != nil {
			logger.Error("dispatcher exited", "err", err)
		}
	}()

	if ttsService != nil {
		ttsService.Start(ctx)
		defer ttsService.Stop()
		logger.Info("tts service started")
	}

	webHandler := web.Handler(http.HandlerFunc(handlers.Index))
	if webHandler != nil {
		logger.Info("embedded web dashboard available")
	} else {
		logger.Info("no embedded web dashboard; serving JSON landing page at /")
	}

	twitchOAuthCfg := buildTwitchOAuthConfig(cryptoBox, logger)
	var oauthTwitch *handlers.OAuth
	if twitchOAuthCfg != nil {
		// Behind the public HTTPS reverse proxy the session cookie must be
		// Secure; for a plain-HTTP LAN deployment it must not be (or the
		// browser drops it). ENGELOS_COOKIE_SECURE controls this, defaulting
		// to true so the safe, production-behind-TLS setting needs no env.
		oauthTwitch = handlers.NewOAuth(authStore, defaultTenantID, logger, twitchOAuthCfg).
			WithCookieSecure(envBoolDefault("ENGELOS_COOKIE_SECURE", true)).
			WithOnLogin(func(ev handlers.LoginEvent) {
				// Live-apply a freshly authorized BOT token to the running
				// adapter so "Login with Twitch" takes effect immediately,
				// with no restart and nothing to paste. The broadcaster
				// (purpose=user) token is only persisted for Helix calls.
				logger.Info("oauth login",
					"provider", ev.Provider, "purpose", ev.Purpose,
					"login", ev.Login, "scopes", len(ev.Scopes))
				if ev.Provider == auth.ProviderTwitch &&
					ev.Purpose == auth.OAuthPurposeBot &&
					twitchAdapter != nil {
					if err := twitchAdapter.SetToken(ev.AccessToken); err != nil {
						logger.Warn("twitch live token apply failed",
							"login", ev.Login, "err", err)
					} else {
						logger.Info("twitch bot token applied live", "login", ev.Login)
					}
				}
			}).
			WithOwnerLogins(splitCSV(os.Getenv("ENGELOS_OWNER_TWITCH_LOGINS"))).
			WithOwnerDiscordLogins(splitCSV(os.Getenv("ENGELOS_OWNER_DISCORD_LOGINS"))).
			WithWorkspaces(workspacesStore).
			WithOpenLogin(envBoolDefault("ENGELOS_OPEN_LOGIN", false)).
			WithModVerify(twitchModVerifyFactory(authStore, twitchOAuthCfg.ClientID, defaultTenantID, logger))
		// Twitch user-access tokens expire ~4h after issuance; without
		// proactive refresh the stored bot token goes stale and Helix
		// calls 401. Live re-application to the connected adapter is
		// Phase 5b - here only persistence + the OnRefresh hook run.
		refresher, rerr := oauthrefresh.New(oauthrefresh.Config{
			Store:  providerScopedStore{inner: refreshStoreAdapter{store: authStore}, provider: auth.ProviderTwitch},
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

	// Discord "Login with Discord" dashboard SSO. It shares an OAuth core
	// (store, cookie settings, owner allowlist) so Discord and Twitch issue
	// identical owner-gated sessions. When Twitch is disabled there is no
	// shared core, so build a dedicated one carrying just the Discord owner
	// allowlist.
	discordOAuthCfg := buildDiscordOAuthConfig(logger)
	var oauthDiscord *handlers.DiscordOAuth
	if discordOAuthCfg != nil {
		core := oauthTwitch
		if core == nil {
			core = handlers.NewOAuth(authStore, defaultTenantID, logger, nil).
				WithCookieSecure(envBoolDefault("ENGELOS_COOKIE_SECURE", true)).
				WithOwnerDiscordLogins(splitCSV(os.Getenv("ENGELOS_OWNER_DISCORD_LOGINS"))).
				WithOpenLogin(envBoolDefault("ENGELOS_OPEN_LOGIN", false))
		}
		oauthDiscord = handlers.NewDiscordOAuth(core, discordOAuthCfg)
	}

	// Spotify song-request token refresher: keeps the bot's stored Spotify
	// access token fresh so song requests never 401 mid-stream. Scoped to
	// Spotify identities only, so it never touches Twitch tokens.
	spotifyOAuthCfg := buildSpotifyOAuthConfig(cryptoBox, logger)
	var oauthSpotify *handlers.SpotifyOAuth
	if spotifyOAuthCfg != nil {
		oauthSpotify = handlers.NewSpotifyOAuth(authStore, defaultTenantID, logger, spotifyOAuthCfg).
			WithCookieSecure(false)
		spotifyRefresher, rerr := oauthrefresh.New(oauthrefresh.Config{
			Store:  providerScopedStore{inner: refreshStoreAdapter{store: authStore}, provider: auth.ProviderSpotify},
			Tokens: spotifyTokenSource{cfg: spotifyOAuthCfg},
			Logger: logger,
		})
		if rerr != nil {
			logger.Warn("spotify oauth refresher disabled", "err", rerr)
		} else {
			go func() {
				if err := spotifyRefresher.Run(ctx); err != nil {
					logger.Error("spotify oauth refresher exited", "err", err)
				}
			}()
			logger.Info("spotify oauth token refresher started")
		}
	}

	// YouTube "Connect YouTube" flow: links a Google/YouTube account to the
	// logged-in dashboard user as the tenant's bot identity so the live-chat
	// adapter can read and write chat. Off by default until the three
	// ENGELOS_YOUTUBE_CLIENT_ID/SECRET/REDIRECT_URL env vars are set.
	youtubeOAuthCfg := buildYouTubeOAuthConfig(cryptoBox, logger)
	var oauthYouTube *handlers.YouTubeOAuth
	if youtubeOAuthCfg != nil {
		oauthYouTube = handlers.NewYouTubeOAuth(authStore, defaultTenantID, logger, youtubeOAuthCfg).
			WithCookieSecure(false)
	}

	kickOAuthCfg := buildKickOAuthConfig(cryptoBox, logger)
	var oauthKick *handlers.KickOAuth
	if kickOAuthCfg != nil {
		oauthKick = handlers.NewKickOAuth(authStore, defaultTenantID, logger, kickOAuthCfg).
			WithCookieSecure(false)
	}

	router := api.NewRouter(api.Deps{
		Logger: logger,
		Version: handlers.Version{
			Version: Version,
			Phase:   "1B",
		},
		AllowedOrigins:     allowedOrigins,
		WS:                 hub,
		Web:                webHandler,
		Overlay:            overlay.Handler(logger),
		AvatarWS:           avatar.NewWSHandler(avatarHub, avatarStore, defaultTenantID, logger),
		AvatarToken:        avatar.NewTokenHandler(avatarStore, defaultTenantID, logger),
		AuthStore:          authStore,
		TenantID:           defaultTenantID,
		CookieSecure:       false,
		Pity:               pitySystem,
		Streak:             streakSystem,
		StatsProvider:      dispatcherStatsAdapter{d: dispatcher},
		OAuthTwitch:        oauthTwitch,
		OAuthDiscord:       oauthDiscord,
		OAuthSpotify:       oauthSpotify,
		OAuthYouTube:       oauthYouTube,
		OAuthKick:          oauthKick,
		RedemptionStore:    redemptionStore,
		CommandStore:       customStore,
		TimerStore:         timerStore,
		CounterStore:       counterStore,
		Moderation:         moderationSvc,
		FeatureStore:       featureFlagStore,
		SongRequestStore:   songRequestStore,
		TranslateStore:     translateStore,
		TTSStore:           ttsStore,
		TTSSecrets:         ttsSecrets(cryptoBox),
		ClipperStore:       clipperStore,
		CoHostStore:        cohostStore,
		MomentsStore:       momentsStore,
		MomentsBroadcaster: runtime.NewWSBroadcaster(hub, logger),
		ContextModStore:    contextmodStore,
		AIManager:          aiManager,
		Integrations:       integrationsRegistry,
		IntegrationCreds:   integrationCreds,
		SongQueueStore:     songQueueStore,
		WrappedStore:       wrappedStore,
		WrappedRanker:      wrappedRankerAdapter{loyalty: loyaltyStore, streak: streakSystem, tenantID: defaultTenantID},
		Chat:               newChatController(platforms),
		WebhookProviders:   webhookProviders(platforms),
		QuoteStore:         quoteStore,
		KBStore:            kbStore,
		PluginRegistry:     pluginRegistry,
		PluginState:        pluginState,
		PluginsActive:      pluginsActive,
		RewardStore:        rewardsStore,
		TimersStore:        timerStore,
		ActionsStore:       actionsStore,
		ActionsRegistry:    actionsRegistry,
		ActionsRunner:      actionsRuleEngine,
		ActionsScheduler:   actionsScheduler,
		ActionsRuns:        runsSource,
		KofiCreds:          integrationCreds,
		Dispatcher:         dispatcher,
		WorkspacesStore:    workspacesStore,
		LiveOpsStore:       eventStoreLO,
		LoyaltyStore:       loyaltyStore,
		LoyaltyResolver:    userProfileProvider{adapter: twitchAdapter}.UserProfile,
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
// value ("1", "true", "yes", "on", case-insensitive). Anything else - including
// unset - is false, so the daemon keeps its loopback-only default unless the
// operator explicitly opts in.
func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// songRequestsActive resolves whether the songrequests feature runs this
// process. The plugin toggle (pluginEnabled, default off per RES-19) is the
// source of truth; the legacy ENGELOS_FEATURE_SONGREQUESTS env flag (envFlag)
// forces it on for backward compatibility. A box that sets the flag behaves
// exactly as before, and a box with neither stays default-off.
func songRequestsActive(pluginEnabled, envFlag bool) bool {
	return pluginEnabled || envFlag
}

// envBoolDefault parses a boolean env var, returning def when the var is
// unset or empty so security-sensitive defaults (like Secure cookies) hold
// without requiring the operator to set anything.
func envBoolDefault(name string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

// buildTwitchOAuthConfig assembles the Twitch *oauth2.Config shared by the
// "Login with Twitch" handler and the background token refresher, or returns
// nil (OAuth disabled) when its prerequisites are missing. It requires an
// encryption box (so tokens can be stored encrypted) and the three
// ENGELOS_TWITCH_CLIENT_ID/SECRET/REDIRECT_URL env vars; absence of any is a
// normal, non-fatal "feature off" state, not an error.
// discordOAuthEndpoint pins Discord's OAuth2 authorize and token URLs.
// Discord's token endpoint only accepts application/x-www-form-urlencoded
// and HTTP Basic client auth, which AuthStyleInHeader selects.
var discordOAuthEndpoint = oauth2.Endpoint{
	AuthURL:   "https://discord.com/oauth2/authorize",
	TokenURL:  "https://discord.com/api/oauth2/token",
	AuthStyle: oauth2.AuthStyleInHeader,
}

// buildDiscordOAuthConfig assembles the Discord "Login with Discord"
// dashboard SSO config from env, or returns nil (feature disabled) when
// any required value is missing. Only identify+email scopes are requested
// because this flow just identifies the operator; the Discord BOT uses a
// separate static token, not this OAuth app.
func buildDiscordOAuthConfig(logger *slog.Logger) *oauth2.Config {
	clientID := os.Getenv("ENGELOS_DISCORD_CLIENT_ID")
	clientSecret := os.Getenv("ENGELOS_DISCORD_CLIENT_SECRET")
	redirectURL := os.Getenv("ENGELOS_DISCORD_REDIRECT_URL")
	if clientID == "" || clientSecret == "" || redirectURL == "" {
		logger.Info("discord oauth disabled",
			"has_client_id", clientID != "",
			"has_secret", clientSecret != "", "has_redirect", redirectURL != "")
		return nil
	}
	logger.Info("discord oauth enabled", "redirect_url", redirectURL)
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"identify", "email"},
		Endpoint:     discordOAuthEndpoint,
	}
}

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

// defaultTwitchScopes lists each requested OAuth scope with the capability it
// unlocks, so the grant stays auditable. The same set is requested for both
// the bot login (purpose=bot) and the broadcaster login (purpose=user); each
// account only grants what it owns, so channel-scoped reads (subs, vips, hype
// train) take effect on the broadcaster token while chat/clip/mod actions run
// on the bot token. Destructive or secret scopes (stream key, commercials, VOD
// deletion, moderator management) are deliberately excluded.
var defaultTwitchScopes = []string{
	"user:read:email", // identify the account

	// Chat
	"chat:read", // receive chat messages (IRC)
	"chat:edit", // send chat messages (IRC)

	// Moderation
	"moderator:manage:banned_users",  // ban / timeout actions
	"moderator:manage:chat_messages", // delete-message action
	"moderator:manage:chat_settings", // toggle slow / sub-only / emote-only mode
	"moderator:manage:announcements", // post /announce messages
	"moderator:manage:shoutouts",     // auto /shoutout on raid
	"moderator:manage:warnings",      // warn a user before escalating
	"moderator:read:followers",       // read follower dates for !followage
	"moderator:read:chatters",        // list who is currently in chat
	"moderation:read",                // read the channel's moderator list to auto-verify mod workspace access

	// Channel points
	"channel:read:redemptions",   // observe channel-point redemptions
	"channel:manage:redemptions", // create rewards + fulfill/refund redemptions

	// Predictions / polls
	"channel:read:predictions",   // read the active prediction (id + outcomes)
	"channel:manage:predictions", // create / lock / resolve / cancel predictions
	"channel:read:polls",         // read poll state + events
	"channel:manage:polls",       // create / end polls

	// Subscriptions / VIPs (viewer knowledge)
	"channel:read:subscriptions", // subscriber list + sub/resub/giftsub events
	"channel:read:vips",          // read VIP list
	"channel:manage:vips",        // grant / revoke VIP

	// Bits / hype train / goals / charity / ads (engagement signals)
	"bits:read",               // cheer events + bits leaderboard
	"channel:read:hype_train", // hype train begin/progress/end + contributors
	"channel:read:goals",      // creator goal progress
	"channel:read:charity",    // charity campaign donations + progress
	"channel:read:ads",        // ad schedule + ad-break events

	// Stream info
	"channel:manage:broadcast", // edit title / category / tags, create markers

	// Clips
	"clips:edit", // create clips for the auto-clipper
}

// twitchModVerifyFactory builds the per-workspace Helix-client factory that the
// OAuth callback uses to auto-verify Twitch moderators. For a workspace it loads
// the owner's Twitch user identity, requires a moderation:read grant, and builds
// a Helix client bound to that broadcaster token. It returns ok=false (skip this
// workspace) whenever the owner has no usable broadcaster token with the scope,
// or the client cannot be built, so a misconfigured workspace never blocks login.
func twitchModVerifyFactory(store auth.Store, clientID, tenantID string, logger *slog.Logger) func(context.Context, workspaces.Workspace) (*helix.Client, bool) {
	return func(ctx context.Context, ws workspaces.Workspace) (*helix.Client, bool) {
		idents, err := store.GetOAuthIdentitiesByUser(ctx, tenantID, ws.OwnerUserID)
		if err != nil {
			logger.Warn("mod-verify: owner identities lookup failed", "workspace", ws.ID, "err", err)
			return nil, false
		}
		for _, id := range idents {
			if id.Provider != auth.ProviderTwitch || id.Purpose != auth.OAuthPurposeUser {
				continue
			}
			if id.AccessToken == "" || !slices.Contains(id.Scopes, "moderation:read") {
				continue
			}
			c, cerr := helix.NewClient(&helix.Options{ClientID: clientID, UserAccessToken: id.AccessToken})
			if cerr != nil {
				logger.Warn("mod-verify: helix client build failed", "workspace", ws.ID, "err", cerr)
				return nil, false
			}
			return c, true
		}
		return nil, false
	}
}

// twitchOAuthScopes returns the scopes to request, allowing an operator to
// override the default set via ENGELOS_TWITCH_SCOPES (comma-separated).
func twitchOAuthScopes() []string {
	if custom := splitCSV(os.Getenv("ENGELOS_TWITCH_SCOPES")); len(custom) > 0 {
		return custom
	}
	return defaultTwitchScopes
}

// defaultSpotifyScopes are the OAuth scopes the Spotify song-request feature
// needs: queue/skip playback control and reading the current track. Playlist
// scopes cover the "playlist as queue" approach (add/remove tracks).
var defaultSpotifyScopes = []string{
	"user-modify-playback-state",  // skip / queue control
	"user-read-playback-state",    // read active device + playback
	"user-read-currently-playing", // read the current track
	"playlist-modify-public",      // add/remove tracks on a public SR playlist
	"playlist-modify-private",     // add/remove tracks on a private SR playlist
}

// buildSpotifyOAuthConfig assembles the Spotify *oauth2.Config shared by the
// "Connect Spotify" handler and the Spotify token refresher, or returns nil
// (feature off) when its prerequisites are missing. Like the Twitch builder it
// requires an encryption box plus the three
// ENGELOS_SPOTIFY_CLIENT_ID/SECRET/REDIRECT_URL env vars; absence of any is a
// normal, non-fatal "feature off" state.
func buildSpotifyOAuthConfig(box *secrets.Box, logger *slog.Logger) *oauth2.Config {
	clientID := os.Getenv("ENGELOS_SPOTIFY_CLIENT_ID")
	clientSecret := os.Getenv("ENGELOS_SPOTIFY_CLIENT_SECRET")
	redirectURL := os.Getenv("ENGELOS_SPOTIFY_REDIRECT_URL")
	if box == nil || clientID == "" || clientSecret == "" || redirectURL == "" {
		logger.Info("spotify oauth disabled",
			"has_key", box != nil, "has_client_id", clientID != "",
			"has_secret", clientSecret != "", "has_redirect", redirectURL != "")
		return nil
	}
	logger.Info("spotify oauth enabled", "redirect_url", redirectURL)
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       defaultSpotifyScopes,
		Endpoint:     spotifyoauth.Endpoint,
	}
}

// defaultYouTubeScopes are the OAuth scopes the YouTube live-chat adapter
// needs: youtube.force-ssl covers reading, sending, and moderating live chat,
// and userinfo.profile lets the connect callback fetch which Google account
// was linked.
var defaultYouTubeScopes = []string{
	"https://www.googleapis.com/auth/youtube.force-ssl",
	"https://www.googleapis.com/auth/userinfo.profile",
}

// googleOAuthEndpoint mirrors golang.org/x/oauth2/google.Endpoint. It is
// inlined rather than imported because the google subpackage pulls in
// cloud.google.com/go/compute/metadata, which is not in this module's go.sum.
var googleOAuthEndpoint = oauth2.Endpoint{
	AuthURL:       "https://accounts.google.com/o/oauth2/auth",
	TokenURL:      "https://oauth2.googleapis.com/token",
	DeviceAuthURL: "https://oauth2.googleapis.com/device/code",
	AuthStyle:     oauth2.AuthStyleInParams,
}

// buildYouTubeOAuthConfig assembles the YouTube *oauth2.Config used by the
// "Connect YouTube" handler, or returns nil (feature off) when its
// prerequisites are missing. Like the Spotify builder it requires an
// encryption box plus the three
// ENGELOS_YOUTUBE_CLIENT_ID/SECRET/REDIRECT_URL env vars; absence of any is a
// normal, non-fatal "feature off" state.
func buildYouTubeOAuthConfig(box *secrets.Box, logger *slog.Logger) *oauth2.Config {
	clientID := os.Getenv("ENGELOS_YOUTUBE_CLIENT_ID")
	clientSecret := os.Getenv("ENGELOS_YOUTUBE_CLIENT_SECRET")
	redirectURL := os.Getenv("ENGELOS_YOUTUBE_REDIRECT_URL")
	if box == nil || clientID == "" || clientSecret == "" || redirectURL == "" {
		logger.Info("youtube oauth disabled",
			"has_key", box != nil, "has_client_id", clientID != "",
			"has_secret", clientSecret != "", "has_redirect", redirectURL != "")
		return nil
	}
	logger.Info("youtube oauth enabled", "redirect_url", redirectURL)
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       defaultYouTubeScopes,
		Endpoint:     googleOAuthEndpoint,
	}
}

// defaultKickScopes are the OAuth scopes the Kick adapter needs: read and send
// chat, register webhook event subscriptions, and moderate (ban/timeout and
// delete messages).
var defaultKickScopes = []string{
	"chat:write",
	"events:subscribe",
	"moderation:ban",
	"moderation:chat_message:manage",
}

// kickOAuthEndpoint is Kick's OAuth 2.1 authorization server. Kick requires
// PKCE; the handler supplies the S256 challenge and verifier.
var kickOAuthEndpoint = oauth2.Endpoint{
	AuthURL:   "https://id.kick.com/oauth/authorize",
	TokenURL:  "https://id.kick.com/oauth/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// buildKickOAuthConfig assembles the Kick *oauth2.Config used by the "Connect
// Kick" handler, or returns nil (feature off) when its prerequisites are
// missing. It requires an encryption box plus the three
// ENGELOS_KICK_CLIENT_ID/SECRET/REDIRECT_URL env vars; absence of any is a
// normal, non-fatal "feature off" state.
func buildKickOAuthConfig(box *secrets.Box, logger *slog.Logger) *oauth2.Config {
	clientID := os.Getenv("ENGELOS_KICK_CLIENT_ID")
	clientSecret := os.Getenv("ENGELOS_KICK_CLIENT_SECRET")
	redirectURL := os.Getenv("ENGELOS_KICK_REDIRECT_URL")
	if box == nil || clientID == "" || clientSecret == "" || redirectURL == "" {
		logger.Info("kick oauth disabled",
			"has_key", box != nil, "has_client_id", clientID != "",
			"has_secret", clientSecret != "", "has_redirect", redirectURL != "")
		return nil
	}
	logger.Info("kick oauth enabled", "redirect_url", redirectURL)
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       defaultKickScopes,
		Endpoint:     kickOAuthEndpoint,
	}
}

// spotifyTokenSource adapts the Spotify *oauth2.Config onto
// oauthrefresh.TokenSource, identical in shape to twitchTokenSource.
type spotifyTokenSource struct{ cfg *oauth2.Config }

func (t spotifyTokenSource) Refresh(ctx context.Context, refreshToken string) (string, string, time.Time, error) {
	src := t.cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	tok, err := src.Token()
	if err != nil {
		return "", "", time.Time{}, err
	}
	return tok.AccessToken, tok.RefreshToken, tok.Expiry, nil
}

// providerScopedStore wraps a refresh store so a refresher only ever sees
// identities for ONE provider. This is essential once more than one provider
// exists: each provider's refresher must refresh ONLY its own tokens (a Twitch
// refresher must never attempt a Spotify token against the Twitch endpoint).
type providerScopedStore struct {
	inner    oauthrefresh.Store
	provider string
}

func (s providerScopedStore) ListExpiring(ctx context.Context, cutoff time.Time) ([]oauthrefresh.Identity, error) {
	all, err := s.inner.ListExpiring(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, id := range all {
		if id.Provider == s.provider {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s providerScopedStore) UpdateTokens(ctx context.Context, id, accessToken, refreshToken string, expiresAt time.Time) error {
	return s.inner.UpdateTokens(ctx, id, accessToken, refreshToken, expiresAt)
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
// per defaultEarnCooldown of chatting - the per-viewer cooldown is the
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
	// Credit by the stable numeric id, not the login: the rest of the economy
	// reads balances by numeric id, so a login key strands the gift unspendably.
	toViewerID := prof.ID
	if toViewerID == "" {
		return commands.LoyaltyInvalid, ""
	}
	display := prof.DisplayName
	if display == "" {
		display = prof.Login
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
// contextRulesProvider resolves the active context-moderation rules for a
// channel, preferring a per-channel store row and falling back to the global
// env rules when no row exists. Results are cached for a short TTL so the
// moderation hot path does not hit the database on every message.
//
// Returning empty rules disables AI escalation for that channel, so an absent
// store, a disabled row, or an empty rule set all collapse to the safe "no
// escalation" behaviour.
type contextRulesProvider struct {
	store    contextmod.Store
	tenantID string
	envRules string
	logger   *slog.Logger

	mu    sync.Mutex
	cache map[string]contextRulesEntry
}

type contextRulesEntry struct {
	rules    string
	loadedAt time.Time
}

const contextRulesTTL = 30 * time.Second

// rulesFor returns the rules to apply for channel, or "" to skip escalation.
func (p *contextRulesProvider) rulesFor(ctx context.Context, channel string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if e, ok := p.cache[channel]; ok && now.Sub(e.loadedAt) < contextRulesTTL {
		return e.rules
	}

	rules := p.envRules
	if p.store != nil {
		cfg, err := p.store.Get(ctx, p.tenantID, channel)
		switch {
		case err == nil:
			if cfg.Enabled {
				rules = cfg.Rules
			} else {
				rules = ""
			}
		case errors.Is(err, contextmod.ErrNotFound):
			// No per-channel row: keep the env fallback.
		default:
			p.logger.WarnContext(ctx, "contextmod rules load failed, using env fallback",
				"channel", channel, "err", err)
		}
	}
	if p.cache == nil {
		p.cache = make(map[string]contextRulesEntry)
	}
	p.cache[channel] = contextRulesEntry{rules: rules, loadedAt: now}
	return rules
}

// aiModService is the narrow moderation.Service contract the adapter needs,
// declared as an interface so the AI branch is unit-testable with a fake.
type aiModService interface {
	Evaluate(ctx context.Context, msg moderation.Message) moderation.Decision
	DryRun() bool
	LogExternal(ctx context.Context, msg moderation.Message, dec moderation.Decision)
	EscalateExternal(ctx context.Context, msg moderation.Message, ai *moderation.AIVerdict) (moderation.ActionKind, time.Duration, bool)
}

// aiClassifier is the narrow contextmod.Escalator contract the adapter needs:
// classify a message with its channel's recent history, and observe every
// processed message into that per-channel rolling history.
type aiClassifier interface {
	ClassifyInChannel(ctx context.Context, channel, rules, username, text string) contextmod.Decision
	Observe(channel, username, text string)
}

type channelRulesProvider interface {
	rulesFor(ctx context.Context, channel string) string
}

type moderationAdapter struct {
	svc       aiModService
	escalator aiClassifier
	rules     channelRulesProvider
}

func (a moderationAdapter) Evaluate(ctx context.Context, channel, messageID, userID, username, text string,
	emoteCount int, firstMsg, isMod, isVIP, isSub, isBroadcaster bool) runtime.ModDecision {
	// Feed the rolling chat-context window for EVERY processed message, before
	// any early return below, so history exists ahead of the first escalation.
	// Deferred so the message is recorded AFTER it is classified and never
	// appears in its own recent-chat history.
	if a.escalator != nil {
		defer a.escalator.Observe(channel, username, text)
	}
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
	out := runtime.ModDecision{
		Action:   runtime.ModAction(dec.Kind),
		Duration: dec.Duration,
		Reason:   dec.Reason,
		DryRun:   dec.DryRun,
	}
	// AI escalation is strictly additive: only consult it for messages the
	// rule engine let pass, and never for privileged users. A non-actionable
	// verdict (unknown/allow) leaves the original pass untouched. Rules are
	// resolved per channel; empty rules skip escalation entirely.
	if out.Action != runtime.ModActionNone || a.escalator == nil || a.rules == nil {
		return out
	}
	if isMod || isBroadcaster {
		return out
	}
	rules := a.rules.rulesFor(ctx, channel)
	if rules == "" {
		return out
	}

	d := a.escalator.ClassifyInChannel(ctx, channel, rules, username, text)
	outcome := contextmod.ApplyPolicy(d, contextmod.DefaultPolicy())
	dryRun := a.svc.DryRun()

	if outcome.Action == contextmod.VerdictAllow && !outcome.AuditOnly {
		return out
	}

	aiMsg := moderation.Message{
		Channel:   channel,
		MessageID: messageID,
		UserID:    userID,
		Username:  username,
		Text:      text,
	}
	reason := "AI: " + d.Reason
	if d.Category != contextmod.CategoryNone && d.Category != "" {
		reason = "AI[" + string(d.Category) + "]: " + d.Reason
	}

	// Carry the classifier's verdict onto every AI audit row (both the
	// audit-only and enforced paths, and the ladder row) so the operator can
	// review exactly what the model decided.
	aiVerdict := &moderation.AIVerdict{
		Category:   string(d.Category),
		Severity:   d.Severity,
		Confidence: d.Confidence,
		Consulted:  d.Consulted,
	}

	// Low-confidence or joke (severity 0/1 below the confidence floor): record
	// for review but never punish. Logged exactly once here, then return the
	// original pass.
	if outcome.AuditOnly {
		a.svc.LogExternal(ctx, aiMsg, moderation.Decision{
			Kind:   moderation.ActionNone,
			Reason: reason,
			Filter: "contextmod",
			DryRun: dryRun,
			AI:     aiVerdict,
		})
		return out
	}

	// Medium/high severity feeds the repeat-offender ladder; spam and low
	// severity stay single-shot (hit-and-run: delete only, no ladder, no ban
	// build-up).
	var ladderAction moderation.ActionKind
	var ladderDur time.Duration
	if outcome.FeedLadder {
		ladderAction, ladderDur, _ = a.svc.EscalateExternal(ctx, aiMsg, aiVerdict)
	}
	aiDec, _ := aiPolicyDecision(outcome, ladderAction, ladderDur, dryRun, reason)

	// Audit the final enforced/previewed decision. The dispatcher gate honours
	// DryRun so shadow mode still audits without enforcing (the M2 invariant).
	a.svc.LogExternal(ctx, aiMsg, moderation.Decision{
		Kind:     moderationKind(aiDec.Action),
		Duration: aiDec.Duration,
		Reason:   aiDec.Reason,
		Filter:   "contextmod",
		DryRun:   dryRun,
		AI:       aiVerdict,
	})
	return aiDec
}

// aiPolicyDecision is the pure mapping from a policy outcome (plus, when the
// policy feeds the ladder, the escalation result) to the final ModDecision. It
// merges by taking the MORE severe action and the LONGER timeout: a repeat
// offender can climb from the policy timeout to a ban, while a first offence
// stays at the policy action because a warn-level ladder rung maps to delete
// and never downgrades below the policy floor. The returned bool echoes
// outcome.FeedLadder so callers and tests can assert ladder participation.
func aiPolicyDecision(outcome contextmod.PolicyOutcome, ladderAction moderation.ActionKind, ladderDur time.Duration, dryRun bool, reason string) (runtime.ModDecision, bool) {
	base := runtime.ModActionDelete
	dur := time.Duration(0)
	if outcome.Action == contextmod.VerdictTimeout {
		base = runtime.ModActionTimeout
		dur = outcome.Timeout
	}
	if !outcome.FeedLadder {
		return runtime.ModDecision{Action: base, Duration: dur, Reason: reason, DryRun: dryRun}, false
	}

	merged := base
	if la := runtime.ModAction(ladderAction); la > merged {
		merged = la
	}
	finalDur := time.Duration(0)
	if merged == runtime.ModActionTimeout {
		finalDur = max(dur, ladderDur)
	}
	return runtime.ModDecision{Action: merged, Duration: finalDur, Reason: reason, DryRun: dryRun}, true
}

// moderationKind maps a runtime.ModAction back onto the moderation.ActionKind
// the audit writer expects, so an AI verdict is logged with the same action
// vocabulary ("delete"/"timeout") as the rule-engine rows.
func moderationKind(a runtime.ModAction) moderation.ActionKind {
	switch a {
	case runtime.ModActionDelete:
		return moderation.ActionDelete
	case runtime.ModActionTimeout:
		return moderation.ActionTimeout
	case runtime.ModActionBan:
		return moderation.ActionBan
	default:
		return moderation.ActionNone
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
func buildCommandRouter(tenantID string, pity *pity.System, streak *streak.System, custom customcommands.Store, timerStore timers.Store, quoteStore quotes.Store, counterStore counters.Store, liveopsStore liveops.Store, twitchAdapter *twitch.Adapter, loyaltyProvider commands.LoyaltyProvider, heistSender commands.HeistSender, rewardCatalog commands.RewardCatalog, featureToggle commands.FeatureToggleStore, predictions commands.PredictionController, songRequester commands.SongRequester, momentCtrl commands.MomentController, translateConfig commands.TranslateConfigStore, cohostConfig commands.CoHostConfigStore, logger *slog.Logger) runtime.CommandRouter {
	engine := commands.New(commands.Config{
		Logger:   logger,
		Resolver: customResolver{tenantID: tenantID, store: custom},
	})
	register := func(c commands.Command) {
		if err := engine.Register(c); err != nil {
			logger.Warn("command registration failed", "command", c.Name, "err", err)
		}
	}
	// pity/streak plugins: the querier adapters dereference their systems, so a
	// command is registered only when its system is present. The leaderboard
	// spans both boards, so it needs pity AND streak.
	if pity != nil {
		register(commands.NewPityCommand(tenantID, pityQuerier{sys: pity}))
	}
	if streak != nil {
		register(commands.NewStreakCommand(tenantID, streakQuerier{sys: streak}))
	}
	if pity != nil && streak != nil {
		register(commands.NewLeaderboardCommand(tenantID, leaderboardQuerier{pity: pity, streak: streak}))
	}
	adminStore := customCommandAdmin{tenantID: tenantID, store: custom}
	register(commands.NewAddCommand(adminStore))
	register(commands.NewEditCommand(adminStore))
	register(commands.NewDeleteCommand(adminStore))
	timerAdminStore := timerAdmin{tenantID: tenantID, store: timerStore}
	register(commands.NewAddTimerCommand(timerAdminStore))
	register(commands.NewDeleteTimerCommand(timerAdminStore))
	register(commands.NewListTimersCommand(timerAdminStore))
	// quotes pilot: with the plugin disabled the store is nil, so the quote
	// commands are not registered at all - the router reports them unhandled
	// rather than replying "unavailable", which is true unregistration.
	if quoteStore != nil {
		quoteAdminStore := quoteAdmin{tenantID: tenantID, store: quoteStore}
		register(commands.NewAddQuoteCommand(quoteAdminStore))
		register(commands.NewQuoteCommand(quoteAdminStore))
		register(commands.NewDeleteQuoteCommand(quoteAdminStore))
	}
	// counters pilot: with the plugin disabled the store is nil, so the counter
	// commands are not registered at all (the router reports them unhandled).
	if counterStore != nil {
		counterAdminStore := counterAdmin{tenantID: tenantID, store: counterStore}
		register(commands.NewCounterCommand(counterAdminStore))
		register(commands.NewCounterAddCommand(counterAdminStore))
		register(commands.NewCounterSubCommand(counterAdminStore))
		register(commands.NewSetCounterCommand(counterAdminStore))
		register(commands.NewResetCounterCommand(counterAdminStore))
	}
	register(commands.NewUptimeCommand(uptimeProvider{adapter: twitchAdapter}))
	streamProvider := streamStatusProvider{adapter: twitchAdapter}
	register(commands.NewGameCommand(streamProvider))
	register(commands.NewTitleCommand(streamProvider))
	// liveops pilot: with the plugin disabled the store is nil, so the event
	// commands are not registered (the router reports them unhandled).
	if liveopsStore != nil {
		liveopsAdminStore := liveopsAdmin{tenantID: tenantID, store: liveopsStore}
		register(commands.NewNextEventCommand(liveopsAdminStore))
		register(commands.NewScheduleCommand(liveopsAdminStore))
		register(commands.NewAddEventCommand(liveopsAdminStore))
		register(commands.NewDelEventCommand(liveopsAdminStore))
	}

	profileProvider := userProfileProvider{adapter: twitchAdapter}
	register(commands.NewAccountAgeCommand(profileProvider))
	register(commands.NewShoutoutCommand(profileProvider, streamProvider))
	register(commands.NewFollowAgeCommand(profileProvider))

	if featureToggle != nil {
		register(commands.NewEconomyToggleCommand(featureToggle))
	}

	if translateConfig != nil {
		register(commands.NewTranslateToggleCommand(translateConfig))
	}

	if cohostConfig != nil {
		register(commands.NewCoHostToggleCommand(cohostConfig))
	}

	if predictions != nil {
		register(commands.NewPredictionCommand(predictions))
		register(commands.NewLockPredictionCommand(predictions))
		register(commands.NewResolvePredictionCommand(predictions))
		register(commands.NewCancelPredictionCommand(predictions))
	}

	if songRequester != nil {
		register(commands.NewSongRequestCommand(songRequester))
		register(commands.NewNowPlayingCommand(songRequester))
		register(commands.NewSkipSongCommand(songRequester))
	}

	if momentCtrl != nil {
		register(commands.NewMomentCommand(momentCtrl))
		register(commands.NewHereCommand(momentCtrl))
	}

	// loyalty pilot: a nil provider (plugin disabled) leaves !points/!give and
	// the leaderboard unregistered; the game blocks below already guard on
	// loyaltyProvider != nil, so they skip too.
	if loyaltyProvider != nil {
		register(commands.NewPointsCommand(loyaltyProvider))
		register(commands.NewGiveCommand(loyaltyProvider))
		register(commands.NewPointsLeaderboardCommand(loyaltyProvider))
	}
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
func newChatController(platforms []adapters.Platform) *handlers.ChatController {
	channels := splitCSV(os.Getenv("ENGELOS_TWITCH_CHANNELS"))
	channel := ""
	if len(channels) > 0 {
		channel = channels[0]
	}
	return &handlers.ChatController{Platforms: platforms, Channel: channel}
}

func ttsNotifier(s *tts.Service) runtime.TTSNotifier {
	if s == nil {
		return nil
	}
	return s
}

func ttsSecrets(box *secrets.Box) handlers.TTSSecrets {
	if box == nil {
		return nil
	}
	return box
}

// aiCrypto adapts the optional encryption box to the aiconfig.Crypto surface,
// returning a true-nil interface (not a typed nil) when no secrets key is
// configured so the manager's nil check behaves.
func aiCrypto(box *secrets.Box) aiconfig.Crypto {
	if box == nil {
		return nil
	}
	return box
}

// integrationsCrypto adapts the optional encryption box to the credstore.Crypto
// surface, returning a true-nil interface when no secrets key is configured so
// the credential store refuses writes rather than persisting plaintext.
func integrationsCrypto(box *secrets.Box) credstore.Crypto {
	if box == nil {
		return nil
	}
	return box
}

func channelPointsSpeaker(s *tts.Service) channelpoints.Speaker {
	if s == nil {
		return nil
	}
	return s
}

func cohostSpeaker(s *tts.Service) runtime.CoHostSpeaker {
	if s == nil {
		return nil
	}
	return s
}

func webhookProviders(platforms []adapters.Platform) []api.WebhookProvider {
	var out []api.WebhookProvider
	for _, p := range platforms {
		if wp, ok := p.(api.WebhookProvider); ok {
			out = append(out, wp)
		}
	}
	return out
}

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
// errCountersDisabled is returned by counterAdmin when the counters plugin is
// off (nil store), so the event-driven Channel-Points counter actions degrade
// to a logged error instead of dereferencing a nil store.
var errCountersDisabled = errors.New("counters plugin disabled")

type counterAdmin struct {
	tenantID string
	store    counters.Store
}

func (a counterAdmin) Value(ctx context.Context, channel, name string) (int64, bool) {
	if a.store == nil {
		return 0, false
	}
	c, err := a.store.Get(ctx, a.tenantID, channel, name)
	if err != nil {
		return 0, false
	}
	return c.Value, true
}

func (a counterAdmin) Add(ctx context.Context, channel, name string, delta int64) (int64, error) {
	if a.store == nil {
		return 0, errCountersDisabled
	}
	c, err := a.store.Add(ctx, a.tenantID, channel, name, delta)
	if err != nil {
		return 0, err
	}
	return c.Value, nil
}

func (a counterAdmin) Set(ctx context.Context, channel, name string, value int64) (int64, error) {
	if a.store == nil {
		return 0, errCountersDisabled
	}
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

// overlayRedemptionNotifier adapts the runtime WS broadcaster to
// channelpoints.OverlayNotifier so a redemption surfaces on the OBS overlays
// as a {type, data} envelope, without channelpoints importing the ws/runtime
// layer. The broadcaster is nil-safe, so an unconfigured hub drops silently.
type overlayRedemptionNotifier struct {
	bc *runtime.WSBroadcaster
}

func (n overlayRedemptionNotifier) Notify(eventType string, alert channelpoints.RedemptionAlert) {
	n.bc.Broadcast(eventType, struct {
		Redemption channelpoints.RedemptionAlert `json:"redemption"`
	}{Redemption: alert})
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
	rules channelpoints.RuleEngine,
	overlay channelpoints.OverlayNotifier,
	speaker channelpoints.Speaker,
	streamState *streamstate.Tracker,
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
	// feature would only 403 - better to stay off and say why.
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
		Speaker:   speaker,
		Logger:    logger,
	})

	// Each redemption fans out to three independent consumers: the binding
	// executor (fixed action + auto-fulfilment), the Action-Engine bridge
	// (user-authored rules), and the overlay notifier (on-stream alert). The
	// bridge and notifier are nil-safe, so an unwired engine or hub simply
	// skips that path.
	bridge := channelpoints.NewBridge(rules, logger)
	notifier := channelpoints.NewNotifier(overlay)
	handler := func(ctx context.Context, evt eventsub.RedemptionEvent) {
		exec.Handle(ctx, evt)
		bridge.Handle(ctx, evt)
		notifier.Handle(ctx, evt)
	}

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
				// Stream events ride the same session but never fail the
				// connection: a stream-sub error only loses live-state tracking.
				if serr := tw.SubscribeStreamOnline(ctx, ch, sessionID); serr != nil {
					logger.Warn("stream.online subscribe failed", "channel", ch, "err", serr)
				}
				if serr := tw.SubscribeStreamOffline(ctx, ch, sessionID); serr != nil {
					logger.Warn("stream.offline subscribe failed", "channel", ch, "err", serr)
				}
			}
			return firstErr
		},
		Handler: handler,
		StreamHandler: func(_ context.Context, evt eventsub.StreamEvent) {
			streamState.Set(evt.BroadcasterUserLogin, evt.Online)
			tw.EmitStreamEvent(evt.Online, evt.BroadcasterUserLogin, evt.StartedAt)
		},
		Logger: logger,
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
// predictionController maps the Twitch adapter's prediction methods onto the
// decoupled commands.PredictionController, translating affiliate/no-active
// sentinels and Helix 403s onto the command-facing PredictionOutcome enum so
// internal/commands never imports the twitch/helix packages. It is nil-safe: a
// missing adapter yields PredictionUnavailable rather than a nil dereference.
type predictionController struct {
	adapter *twitch.Adapter
	logger  *slog.Logger
}

// Create gates on affiliate/partner status (predictions are affiliate-only)
// before opening one, so a non-affiliate gets a clear reply instead of an
// opaque Helix 403.
func (p predictionController) Create(ctx context.Context, channel, title string, outcomes []string, windowSeconds int) (commands.PredictionInfo, commands.PredictionOutcome) {
	if p.adapter == nil {
		return commands.PredictionInfo{}, commands.PredictionUnavailable
	}
	bt, err := p.adapter.BroadcasterType(ctx, channel)
	if err != nil {
		p.logger.WarnContext(ctx, "prediction: broadcaster type lookup failed", "channel", channel, "err", err)
		return commands.PredictionInfo{}, commands.PredictionUnavailable
	}
	if bt != "affiliate" && bt != "partner" {
		return commands.PredictionInfo{}, commands.PredictionNotAffiliate
	}
	// Reject a second prediction up front: Twitch returns an opaque 400 when one
	// is already ACTIVE/LOCKED, so probe first to give the mod a clear reply.
	if _, err := p.adapter.ActivePrediction(ctx, channel); err == nil {
		return commands.PredictionInfo{}, commands.PredictionActiveExists
	} else if !errors.Is(err, twitch.ErrNoActivePrediction) {
		p.logger.WarnContext(ctx, "prediction: active-check failed", "channel", channel, "err", err)
		return commands.PredictionInfo{}, commands.PredictionUnavailable
	}
	view, err := p.adapter.CreatePrediction(ctx, channel, title, outcomes, windowSeconds)
	if err != nil {
		p.logger.WarnContext(ctx, "prediction: create failed", "channel", channel, "err", err)
		return commands.PredictionInfo{}, commands.PredictionUnavailable
	}
	return toPredictionInfo(view), commands.PredictionOK
}

func (p predictionController) Lock(ctx context.Context, channel string) (commands.PredictionInfo, commands.PredictionOutcome) {
	return p.endVerb(ctx, channel, p.adapterLock)
}

func (p predictionController) Cancel(ctx context.Context, channel string) (commands.PredictionInfo, commands.PredictionOutcome) {
	return p.endVerb(ctx, channel, p.adapterCancel)
}

// Resolve maps the not-found-outcome sentinel onto PredictionInvalid so the
// command can tell the mod to check the spelling.
func (p predictionController) Resolve(ctx context.Context, channel, winningOutcome string) (commands.PredictionInfo, commands.PredictionOutcome) {
	if p.adapter == nil {
		return commands.PredictionInfo{}, commands.PredictionUnavailable
	}
	view, err := p.adapter.ResolvePrediction(ctx, channel, winningOutcome)
	switch {
	case err == nil:
		return toPredictionInfo(view), commands.PredictionOK
	case errors.Is(err, twitch.ErrNoActivePrediction):
		return commands.PredictionInfo{}, commands.PredictionNone
	case errors.Is(err, twitch.ErrOutcomeNotFound):
		return commands.PredictionInfo{}, commands.PredictionInvalid
	default:
		p.logger.WarnContext(ctx, "prediction: resolve failed", "channel", channel, "err", err)
		return commands.PredictionInfo{}, commands.PredictionUnavailable
	}
}

func (p predictionController) adapterLock(ctx context.Context, channel string) (twitch.PredictionView, error) {
	return p.adapter.LockPrediction(ctx, channel)
}

func (p predictionController) adapterCancel(ctx context.Context, channel string) (twitch.PredictionView, error) {
	return p.adapter.CancelPrediction(ctx, channel)
}

// endVerb is the shared Lock/Cancel path: both find the active prediction and
// PATCH it, so they share identical sentinel translation.
func (p predictionController) endVerb(ctx context.Context, channel string, fn func(context.Context, string) (twitch.PredictionView, error)) (commands.PredictionInfo, commands.PredictionOutcome) {
	if p.adapter == nil {
		return commands.PredictionInfo{}, commands.PredictionUnavailable
	}
	view, err := fn(ctx, channel)
	switch {
	case err == nil:
		return toPredictionInfo(view), commands.PredictionOK
	case errors.Is(err, twitch.ErrNoActivePrediction):
		return commands.PredictionInfo{}, commands.PredictionNone
	default:
		p.logger.WarnContext(ctx, "prediction: end failed", "channel", channel, "err", err)
		return commands.PredictionInfo{}, commands.PredictionUnavailable
	}
}

// toPredictionInfo flattens the adapter's PredictionView (outcome IDs + titles)
// into the title-only commands.PredictionInfo the chat replies render.
func toPredictionInfo(v twitch.PredictionView) commands.PredictionInfo {
	titles := make([]string, 0, len(v.Outcomes))
	for _, o := range v.Outcomes {
		titles = append(titles, o.Title)
	}
	return commands.PredictionInfo{Title: v.Title, Outcomes: titles, Status: v.Status}
}

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
		ID:          prof.ID,
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
// adapter handle (nil when Twitch is not started - used so the OAuth
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
func startPlatforms(ctx context.Context, logger *slog.Logger, store auth.Store, tenantID string) ([]adapters.Platform, *twitch.Adapter, *discord.Adapter, func()) {
	var (
		started       []adapters.Platform
		closers       []func()
		twitchHandle  *twitch.Adapter
		discordHandle *discord.Adapter
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
		} else if errors.Is(err, auth.ErrOAuthIdentityNotFound) {
			if usr, uerr := store.GetUserIdentityByProvider(ctx, tenantID, auth.ProviderTwitch); uerr == nil {
				oauthToken = usr.AccessToken
				if usr.ProviderLogin != "" {
					username = usr.ProviderLogin
				}
				logger.Info("twitch user token loaded from store (no bot identity; broadcaster is the bot)", "login", usr.ProviderLogin)
			} else if !errors.Is(uerr, auth.ErrOAuthIdentityNotFound) && !errors.Is(uerr, auth.ErrCryptoRequired) {
				logger.Warn("twitch user identity lookup failed", "err", uerr)
			}
		} else if !errors.Is(err, auth.ErrCryptoRequired) {
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
			discordHandle = dc
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

	videoID := strings.TrimSpace(os.Getenv("ENGELOS_YOUTUBE_VIDEO_ID"))
	liveChatID := strings.TrimSpace(os.Getenv("ENGELOS_YOUTUBE_LIVE_CHAT_ID"))
	if videoID != "" || liveChatID != "" {
		botToken := ""
		if bot, err := store.GetBotIdentity(ctx, tenantID, auth.ProviderYouTube); err == nil {
			botToken = bot.AccessToken
			logger.Info("youtube bot token loaded from store", "login", bot.ProviderLogin)
		} else if !errors.Is(err, auth.ErrOAuthIdentityNotFound) && !errors.Is(err, auth.ErrCryptoRequired) {
			logger.Warn("youtube bot identity lookup failed", "err", err)
		}
		if botToken == "" {
			logger.Info("youtube adapter disabled (no bot identity)")
		} else {
			yt := ytadapter.New(ytadapter.Config{
				AccessToken: botToken,
				VideoID:     videoID,
				LiveChatID:  liveChatID,
				Logger:      logger.With("platform", "youtube"),
			})
			if err := yt.Connect(ctx); err != nil {
				logger.Error("youtube adapter connect failed", "err", err)
			} else {
				started = append(started, yt)
				closers = append(closers, func() {
					disconnectCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
					defer c()
					if err := yt.Disconnect(disconnectCtx); err != nil {
						logger.Warn("youtube disconnect", "err", err)
					}
				})
				logger.Info("youtube adapter connected",
					"video_id", videoID, "live_chat_id", liveChatID)
			}
		}
	}

	if bid := strings.TrimSpace(os.Getenv("ENGELOS_KICK_BROADCASTER_USER_ID")); bid != "" {
		broadcasterID, err := strconv.Atoi(bid)
		if err != nil {
			logger.Warn("kick adapter disabled: invalid broadcaster id", "value", bid, "err", err)
		} else {
			userToken := os.Getenv("ENGELOS_KICK_USER_TOKEN")
			if bot, berr := store.GetBotIdentity(ctx, tenantID, auth.ProviderKick); berr == nil {
				userToken = bot.AccessToken
				logger.Info("kick user token loaded from store", "login", bot.ProviderLogin)
			} else if !errors.Is(berr, auth.ErrOAuthIdentityNotFound) && !errors.Is(berr, auth.ErrCryptoRequired) {
				logger.Warn("kick bot identity lookup failed", "err", berr)
			}
			kc, kerr := kick.New(kick.Config{
				ClientID:          os.Getenv("ENGELOS_KICK_CLIENT_ID"),
				ClientSecret:      os.Getenv("ENGELOS_KICK_CLIENT_SECRET"),
				AppAccessToken:    os.Getenv("ENGELOS_KICK_APP_TOKEN"),
				UserAccessToken:   userToken,
				BroadcasterUserID: broadcasterID,
				Channel:           os.Getenv("ENGELOS_KICK_CHANNEL"),
				WebhookURL:        os.Getenv("ENGELOS_KICK_WEBHOOK_URL"),
				Logger:            logger.With("platform", "kick"),
			})
			if kerr != nil {
				logger.Error("kick adapter init failed", "err", kerr)
			} else if err := kc.Connect(ctx); err != nil {
				logger.Error("kick adapter connect failed", "err", err)
			} else {
				started = append(started, kc)
				closers = append(closers, func() {
					disconnectCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
					defer c()
					if err := kc.Disconnect(disconnectCtx); err != nil {
						logger.Warn("kick disconnect", "err", err)
					}
				})
				logger.Info("kick adapter connected", "broadcaster", broadcasterID)
			}
		}
	}

	return started, twitchHandle, discordHandle, cleanup
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

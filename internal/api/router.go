package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/api/ws"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/clipper"
	"github.com/Luca-Pelzer/engelos/internal/cohost"
	"github.com/Luca-Pelzer/engelos/internal/counters"
	"github.com/Luca-Pelzer/engelos/internal/customcommands"
	"github.com/Luca-Pelzer/engelos/internal/featureflags"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/liveops"
	"github.com/Luca-Pelzer/engelos/internal/loyalty"
	"github.com/Luca-Pelzer/engelos/internal/moderation"
	"github.com/Luca-Pelzer/engelos/internal/quotes"
	"github.com/Luca-Pelzer/engelos/internal/redemptions"
	"github.com/Luca-Pelzer/engelos/internal/rewards"
	"github.com/Luca-Pelzer/engelos/internal/songrequests"
	"github.com/Luca-Pelzer/engelos/internal/songrequests/queue"
	"github.com/Luca-Pelzer/engelos/internal/timers"
	"github.com/Luca-Pelzer/engelos/internal/translate"
	"github.com/Luca-Pelzer/engelos/internal/tts"
	"github.com/Luca-Pelzer/engelos/internal/workspaces"
	"github.com/Luca-Pelzer/engelos/internal/wrapped"
)

// Login rate-limit budget: ~1 attempt/sec sustained per client IP with a small
// burst, throttling credential-stuffing without impeding a human retyping a
// password.
const (
	loginRPS   = 1
	loginBurst = 5
)

// WSHandler is the narrow interface the router needs from the WebSocket hub.
// internal/api/ws.Hub satisfies it without the router importing the concrete
// type at the routing layer.
type WSHandler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

// WebhookProvider is a platform adapter that ingests inbound webhooks. The
// router type-asserts adapters against this interface and mounts the returned
// handler at /webhooks/{Name}. Kept narrow so the api package never imports
// the concrete adapter types.
type WebhookProvider interface {
	Name() string
	WebhookHandler() http.HandlerFunc
}

// Deps bundles the narrow set of dependencies the router needs. Concrete
// implementations (auth, eventsourcing, etc.) live in their own packages and
// satisfy these interfaces - the api package never imports them directly.
type Deps struct {
	Logger *slog.Logger

	// Version is reported by GET /version.
	Version handlers.Version

	// AllowedOrigins is consumed by the CORS middleware.
	AllowedOrigins []string

	// AllowCredentials enables Access-Control-Allow-Credentials on CORS.
	AllowCredentials bool

	// EventsHeartbeat is the SSE keepalive cadence. Zero = 5s.
	EventsHeartbeat time.Duration

	// WS is the WebSocket upgrade handler. If nil, a fresh ws.Hub is
	// constructed and used (its Run loop is not started by the router; the
	// caller must start it).
	WS WSHandler

	// Web, if non-nil, serves the embedded SvelteKit dashboard on any path
	// not matched by an API or health route. When nil the router falls back
	// to the plain handlers.Index landing page at "/".
	Web http.Handler

	// Overlay, if non-nil, serves the OBS browser-source overlay pages at
	// /overlay/*. It is mounted OUTSIDE /api so the JSON content-type
	// middleware does not clobber its text/html responses.
	Overlay http.Handler

	// AuthStore backs the auth handlers and the SessionAuth middleware.
	// When nil, the auth routes degrade to 501 "not_implemented" and no
	// session middleware is mounted; this is the bootstrap-time state.
	AuthStore auth.Store

	// TenantID is the single-tenant identifier this daemon serves. It is
	// required when AuthStore is set; otherwise the empty string is fine.
	TenantID string

	// CookieSecure sets the Secure attribute on the session cookie. Must be
	// false for plain-HTTP loopback deployments (otherwise browsers and Go's
	// cookiejar refuse to send the cookie back); true once the daemon is
	// fronted by TLS.
	CookieSecure bool

	// Pity, when non-nil, exposes the pity-system endpoints under
	// /api/v1/pity/*. Nil disables the feature (handlers return 501).
	Pity *pity.System

	// Streak, when non-nil, exposes the streak-system endpoints under
	// /api/v1/streak/*. Nil disables the feature (handlers return 501).
	Streak *streak.System

	// StatsProvider, when non-nil, surfaces dispatcher counters at
	// GET /api/v1/stats. Nil hides the dispatcher block - the version
	// payload is still served.
	StatsProvider handlers.StatsProvider

	// OAuthSpotify, when non-nil, mounts the "Connect Spotify" routes under
	// /api/v1/auth/spotify/*. The callback requires an authenticated session
	// (it links Spotify to the logged-in user). Nil leaves it unmounted.
	OAuthSpotify *handlers.SpotifyOAuth

	// OAuthYouTube, when non-nil, mounts the "Connect YouTube" routes under
	// /api/v1/auth/youtube/*. The callback requires an authenticated session
	// (it links YouTube to the logged-in user). Nil leaves it unmounted.
	OAuthYouTube *handlers.YouTubeOAuth

	// OAuthKick, when non-nil, mounts the "Connect Kick" routes under
	// /api/v1/auth/kick/*. The callback requires an authenticated session
	// (it links Kick to the logged-in user). Nil leaves it unmounted.
	OAuthKick *handlers.KickOAuth

	// OAuthDiscord, when non-nil, mounts the "Login with Discord" routes
	// under /api/v1/auth/discord. Nil leaves them unmounted.
	OAuthDiscord *handlers.DiscordOAuth

	// OAuthTwitch, when non-nil, mounts the "Login with Twitch" routes
	// at GET /api/v1/auth/twitch/login and GET /api/v1/auth/twitch/callback.
	// Nil leaves the OAuth feature disabled (the routes are not mounted).
	OAuthTwitch *handlers.OAuth

	// RedemptionStore, when non-nil, exposes the Channel-Points reward→action
	// binding CRUD under /api/v1/redemptions/*. Nil makes those endpoints
	// return 501 (feature off).
	RedemptionStore redemptions.Store

	// CommandStore, when non-nil, exposes the custom-command CRUD under
	// /api/v1/commands/*. Nil makes those endpoints return 501 (feature off).
	CommandStore customcommands.Store

	// TimerStore, when non-nil, lets the migration import create timers from a
	// Nightbot/StreamElements/Moobot export under /api/v1/migrate. Nil imports
	// commands only.
	TimerStore timers.Store

	// CounterStore, when non-nil, exposes the named-counter CRUD under
	// /api/v1/counters/*. Nil makes those endpoints return 501 (feature off).
	CounterStore counters.Store

	// Moderation, when non-nil, exposes the AutoMod config + audit endpoints
	// under /api/v1/automod/*. Nil makes those endpoints return 501.
	Moderation *moderation.Service

	// FeatureStore, when non-nil, exposes the per-channel feature-flag
	// overrides (e.g. the points "economy" toggle) under /api/v1/features/*.
	// Nil makes those endpoints return 501 (feature off).
	FeatureStore featureflags.Store

	// SongRequestStore, when non-nil, exposes per-channel song-request config
	// (provider, playlist, max-duration) under /api/v1/songrequests. Nil makes
	// those endpoints return 501 (feature off).
	SongRequestStore songrequests.Store

	// TranslateStore, when non-nil, exposes per-channel chat-translation config
	// under /api/v1/translate. Nil makes those endpoints return 501.
	TranslateStore translate.Store

	// TTSStore and TTSSecrets, when both non-nil, expose per-channel
	// text-to-speech config under /api/v1/tts. TTSSecrets encrypts the stored
	// ElevenLabs key. Either nil makes those endpoints return 501.
	TTSStore   tts.Store
	TTSSecrets handlers.TTSSecrets

	// ClipperStore, when non-nil, exposes per-channel auto-clipper tuning
	// under /api/v1/clipper. Nil makes those endpoints return 501.
	ClipperStore clipper.Store

	// CoHostStore, when non-nil, exposes per-channel AI co-host config
	// under /api/v1/cohost. Nil makes those endpoints return 501.
	CoHostStore cohost.Store

	// SongQueueStore, when non-nil, exposes the bot-managed YouTube song queue
	// to the player overlay at /api/v1/songqueue/next (intentionally NOT
	// session-protected; an OBS browser source cannot log in). Nil returns 501.
	SongQueueStore queue.Store

	// WrappedStore, when non-nil, serves Stream-Wrapped recap cards at
	// /api/v1/wrapped (public: cards are meant to be shared/screenshotted).
	// Nil returns 501.
	WrappedStore wrapped.Store

	// WrappedRanker, when non-nil, supplies the loyalty/streak extras on a
	// viewer Wrapped card. Optional; nil omits those fields.
	WrappedRanker handlers.WrappedRanker

	// Chat, when non-nil, exposes outbound chat control (send a message,
	// moderate a user) under /api/v1/chat/*. Nil leaves those routes
	// unmounted so the dashboard composer/mod actions degrade to 404.
	Chat *handlers.ChatController

	// WebhookProviders are platform adapters that ingest inbound webhooks
	// (Kick). Each is mounted at /webhooks/{name} OUTSIDE the session-gated
	// API group: the handler itself verifies authenticity (signatures), so
	// no session middleware applies. Empty mounts nothing.
	WebhookProviders []WebhookProvider

	// QuoteStore, when non-nil, exposes the per-channel quotes CRUD under
	// /api/v1/quotes/*. Nil makes those endpoints return 501 (feature off).
	QuoteStore quotes.Store

	// RewardStore, when non-nil, exposes the per-channel rewards catalog CRUD
	// under /api/v1/rewards/*. Nil makes those endpoints return 501.
	RewardStore rewards.Store

	// TimersStore, when non-nil, exposes the per-channel auto-announcement
	// timers CRUD under /api/v1/timers/*. Nil makes those endpoints
	// return 501. (Distinct from TimerStore, used only for migration import.)
	TimersStore timers.Store

	// ActionsStore and ActionsRegistry, when non-nil, expose the
	// Action-Engine rule CRUD and plugin catalog under /api/v1/actions/*.
	// Nil makes those endpoints return 501.
	ActionsStore    actions.Store
	ActionsRegistry *actions.Registry

	// ActionsRunner backs POST /api/v1/channels/{channelSlug}/actions/{name}/fire
	// by running one rule on demand. Nil makes the fire endpoint return 501.
	ActionsRunner handlers.RuleRunner

	// ActionsScheduler, when non-nil, is re-armed after every rule mutation so
	// timer rules take effect live. Nil skips live re-arming.
	ActionsScheduler handlers.SchedulerReloader

	// WorkspacesStore, when non-nil, exposes workspace/membership endpoints
	// and backs the WorkspaceMiddleware that authorizes channel-scoped
	// routes under /api/v1/channels/{channelSlug}. Nil disables both.
	WorkspacesStore workspaces.Store

	// LiveOpsStore, when non-nil, exposes the per-channel event-plan CRUD
	// under /api/v1/liveops/*. Nil makes those endpoints return 501.
	LiveOpsStore liveops.Store

	// LoyaltyStore, when non-nil, exposes the points leaderboard and manual
	// grant/deduct controls under /api/v1/loyalty/*. Nil makes those
	// endpoints return 501.
	LoyaltyStore loyalty.Store

	// LoyaltyResolver maps a dashboard-typed username to its numeric user id
	// so manual grants share the in-chat economy's keyspace. Nil makes the
	// Adjust endpoint fail closed instead of writing a phantom login-keyed row.
	LoyaltyResolver handlers.LoyaltyResolver
}

// NewRouter builds the full chi router with middleware and routes mounted.
func NewRouter(deps Deps) chi.Router {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	wsh := deps.WS
	if wsh == nil {
		wsh = ws.NewHub(logger)
	}

	health := handlers.NewHealth(deps.Version)
	authH := handlers.NewAuth(deps.AuthStore, deps.TenantID, logger).
		WithCookieSecure(deps.CookieSecure)
	events := handlers.NewEvents(logger, deps.EventsHeartbeat)
	pityH := handlers.NewPity(deps.Pity, deps.TenantID, logger)
	streakH := handlers.NewStreak(deps.Streak, deps.TenantID, logger)
	statsH := handlers.NewStats(deps.Version, deps.StatsProvider, logger)
	redemptionsH := handlers.NewRedemptions(deps.RedemptionStore, deps.TenantID, logger)
	commandsH := handlers.NewCommands(deps.CommandStore, deps.TenantID, logger)
	migrateH := handlers.NewMigrate(deps.CommandStore, deps.TimerStore, deps.TenantID, logger)
	countersH := handlers.NewCounters(deps.CounterStore, deps.TenantID, logger)
	quotesH := handlers.NewQuotes(deps.QuoteStore, deps.TenantID, logger)
	rewardsH := handlers.NewRewards(deps.RewardStore, deps.TenantID, logger)
	timersH := handlers.NewTimers(deps.TimersStore, deps.TenantID, logger)
	actionsH := handlers.NewActions(deps.ActionsStore, deps.ActionsRegistry, deps.ActionsRunner, deps.ActionsScheduler, deps.TenantID, logger)
	workspacesH := handlers.NewWorkspaces(deps.WorkspacesStore, deps.TenantID, logger)
	liveopsH := handlers.NewLiveOps(deps.LiveOpsStore, deps.TenantID, logger)
	loyaltyH := handlers.NewLoyalty(deps.LoyaltyStore, deps.TenantID, deps.LoyaltyResolver, logger)
	automodH := handlers.NewAutoMod(deps.Moderation, logger)
	featuresH := handlers.NewFeatures(deps.FeatureStore, deps.TenantID, logger)
	songRequestsH := handlers.NewSongRequests(deps.SongRequestStore, deps.TenantID, logger)
	translateH := handlers.NewTranslate(deps.TranslateStore, deps.TenantID, logger)
	ttsH := handlers.NewTTS(deps.TTSStore, deps.TTSSecrets, deps.TenantID, logger)
	clipperH := handlers.NewClipper(deps.ClipperStore, deps.TenantID, logger)
	cohostH := handlers.NewCoHost(deps.CoHostStore, deps.TenantID, logger)
	connectionsH := handlers.NewConnections(deps.AuthStore, deps.TenantID, logger)
	songQueueH := handlers.NewSongQueue(deps.SongQueueStore, deps.TenantID, logger)
	wrappedH := handlers.NewWrapped(deps.WrappedStore, deps.WrappedRanker, deps.TenantID, logger)

	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(apimw.Logger(logger))
	r.Use(chimw.Recoverer)
	r.Use(apimw.SecurityHeaders)
	r.Use(apimw.CORS(apimw.CORSOptions{
		AllowedOrigins:   deps.AllowedOrigins,
		AllowCredentials: deps.AllowCredentials,
	}))
	r.Use(apimw.JSONContentType)

	r.Get("/healthz", health.Healthz)
	r.Get("/readyz", health.Readyz)
	r.Get("/version", health.VersionHandler)

	for _, wp := range deps.WebhookProviders {
		if wp == nil {
			continue
		}
		r.Post("/webhooks/"+wp.Name(), wp.WebhookHandler())
		logger.Info("webhook route mounted", "platform", wp.Name())
	}

	r.Route("/api/v1", func(r chi.Router) {
		if deps.AuthStore != nil {
			r.Use(apimw.SessionAuth(deps.AuthStore, "", logger))
		}
		r.Route("/auth", func(r chi.Router) {
			r.With(apimw.RateLimit(loginRPS, loginBurst)).Post("/login", authH.Login)
			r.Post("/logout", authH.Logout)
			// Public capability discovery so the login page only renders the
			// OAuth buttons whose routes are actually mounted; an unconfigured
			// provider would otherwise 404 on click and surface as an error.
			r.Get("/providers", handlers.AuthProviders(handlers.AuthProviderFlags{
				Twitch:  deps.OAuthTwitch != nil,
				Discord: deps.OAuthDiscord != nil,
			}))
			if deps.OAuthTwitch != nil {
				r.Get("/twitch/login", deps.OAuthTwitch.Login)
				r.Get("/twitch/callback", deps.OAuthTwitch.Callback)
			}
			if deps.OAuthDiscord != nil {
				r.Get("/discord/login", deps.OAuthDiscord.Login)
				r.Get("/discord/callback", deps.OAuthDiscord.Callback)
			}
			if deps.OAuthSpotify != nil {
				r.With(apimw.RequireGlobalOwner).Get("/spotify/login", deps.OAuthSpotify.Login)
				r.With(apimw.RequireGlobalOwner).Get("/spotify/callback", deps.OAuthSpotify.Callback)
			}
			if deps.OAuthKick != nil {
				r.With(apimw.RequireGlobalOwner).Get("/kick/login", deps.OAuthKick.Login)
				r.With(apimw.RequireGlobalOwner).Get("/kick/callback", deps.OAuthKick.Callback)
			}
			if deps.OAuthYouTube != nil {
				r.With(apimw.RequireGlobalOwner).Get("/youtube/login", deps.OAuthYouTube.Login)
				r.With(apimw.RequireGlobalOwner).Get("/youtube/callback", deps.OAuthYouTube.Callback)
			}
		})
		r.Route("/users", func(r chi.Router) {
			r.Get("/me", authH.Me)
		})
		r.Route("/connections", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireGlobalOwner)
			}
			r.Get("/", connectionsH.List)
			r.Delete("/{id}", connectionsH.Delete)
		})
		// Live operational data (event stream, dashboard stats, the
		// WebSocket feed) is owner-only: it exposes real chat activity and
		// metrics, so it must sit behind a session like every other
		// dashboard surface. Gated only when an auth store is configured,
		// matching the rest of the API's bootstrap-time behaviour.
		r.Group(func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireGlobalOwner)
			}
			r.Get("/events", events.Stream)
			r.Get("/stats", statsH.Get)
			r.HandleFunc("/ws", wsh.ServeHTTP)
			if deps.Chat != nil {
				r.Post("/chat/send", deps.Chat.Send)
				r.Post("/chat/moderate", deps.Chat.Moderate)
			}
		})

		r.Route("/pity", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireGlobalOwner)
			}
			r.Post("/grant", pityH.Grant)
			r.Post("/roll", pityH.Roll)
			r.Get("/status", pityH.Status)
			r.Get("/leaderboard", pityH.Leaderboard)
			r.Post("/reset", pityH.Reset)
		})

		r.Route("/streak", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireGlobalOwner)
			}
			r.Post("/tick", streakH.Tick)
			r.Post("/freeze", streakH.UseFreeze)
			r.Get("/status", streakH.Status)
			r.Get("/leaderboard", streakH.Leaderboard)
			r.Post("/reset", streakH.Reset)
		})

		r.Route("/migrate", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireGlobalOwner)
			}
			r.Post("/", migrateH.Import)
		})

		r.Route("/me/workspaces", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Get("/", workspacesH.ListMine)
		})

		r.Route("/workspaces", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Post("/", workspacesH.Create)
		})

		r.Route("/channels/{channelSlug}", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			if deps.WorkspacesStore != nil {
				r.Use(apimw.WorkspaceMiddleware(deps.WorkspacesStore, deps.TenantID, logger))
			}
			r.Get("/", workspacesH.Get)
			r.Get("/members", workspacesH.ListMembers)
			r.Group(func(r chi.Router) {
				r.Use(apimw.RequireRole(workspaces.RoleOwner))
				r.Delete("/members/{userID}", workspacesH.RemoveMember)
				r.Get("/invitations", workspacesH.ListInvitations)
				r.Post("/invitations", workspacesH.Invite)
				r.Delete("/invitations/{id}", workspacesH.DeleteInvitation)
				r.Put("/auto-verify", workspacesH.SetAutoVerify)
			})

			r.Route("/commands", func(r chi.Router) {
				r.Get("/", commandsH.List)
				r.Post("/", commandsH.Create)
				r.Put("/{name}", commandsH.Update)
				r.Delete("/{name}", commandsH.Delete)
			})
			r.Route("/counters", func(r chi.Router) {
				r.Get("/", countersH.List)
				r.Put("/{name}", countersH.Set)
				r.Post("/{name}/add", countersH.Add)
				r.Delete("/{name}", countersH.Delete)
			})
			r.Route("/quotes", func(r chi.Router) {
				r.Get("/", quotesH.List)
				r.Post("/", quotesH.Create)
				r.Delete("/{number}", quotesH.Delete)
			})
			r.Route("/rewards", func(r chi.Router) {
				r.Get("/", rewardsH.List)
				r.Post("/", rewardsH.Create)
				r.Put("/{name}", rewardsH.Update)
				r.Delete("/{name}", rewardsH.Delete)
			})
			r.Route("/redemptions", func(r chi.Router) {
				r.Get("/", redemptionsH.List)
				r.Post("/", redemptionsH.Create)
				r.Put("/{rewardID}", redemptionsH.Update)
				r.Post("/{rewardID}/enabled", redemptionsH.SetEnabled)
				r.Delete("/{rewardID}", redemptionsH.Delete)
			})
			r.Route("/timers", func(r chi.Router) {
				r.Get("/", timersH.List)
				r.Post("/", timersH.Create)
				r.Put("/{name}", timersH.Update)
				r.Delete("/{name}", timersH.Delete)
			})
			r.Route("/actions", func(r chi.Router) {
				r.Get("/catalog", actionsH.Catalog)
				r.Get("/", actionsH.List)
				r.Post("/", actionsH.Create)
				r.Put("/{name}", actionsH.Update)
				r.Delete("/{name}", actionsH.Delete)
				r.Post("/{name}/fire", actionsH.Fire)
			})
			r.Route("/liveops", func(r chi.Router) {
				r.Get("/", liveopsH.List)
				r.Post("/", liveopsH.Create)
				r.Delete("/{number}", liveopsH.Delete)
			})
			r.Route("/loyalty", func(r chi.Router) {
				r.Get("/leaderboard", loyaltyH.Leaderboard)
				r.Post("/adjust", loyaltyH.Adjust)
			})
			r.Route("/features", func(r chi.Router) {
				r.Get("/", featuresH.List)
				r.Put("/{feature}", featuresH.Set)
			})
			r.Route("/songrequests", func(r chi.Router) {
				r.Get("/", songRequestsH.Get)
				r.Put("/", songRequestsH.Set)
			})
			r.Route("/translate", func(r chi.Router) {
				r.Get("/", translateH.Get)
				r.Put("/", translateH.Set)
			})
			r.Route("/tts", func(r chi.Router) {
				r.Get("/", ttsH.Get)
				r.Put("/", ttsH.Set)
				r.Get("/voices", ttsH.Voices)
				r.Post("/clone", ttsH.Clone)
				r.Delete("/voices/{voiceID}", ttsH.DeleteVoice)
			})
			r.Route("/clipper", func(r chi.Router) {
				r.Get("/", clipperH.Get)
				r.Put("/", clipperH.Set)
			})
			r.Route("/cohost", func(r chi.Router) {
				r.Get("/", cohostH.Get)
				r.Put("/", cohostH.Set)
			})
		})

		r.Route("/automod", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireGlobalOwner)
			}
			r.Get("/config", automodH.GetConfig)
			r.Put("/config", automodH.PutConfig)
			r.Get("/audit", automodH.Audit)
		})

		// /songqueue is intentionally NOT session-protected: the OBS browser
		// source player has no login and only advances the public song queue.
		r.Get("/songqueue/next", songQueueH.Next)

		// /wrapped is public: recap cards are meant to be shared and rendered
		// in an unauthenticated overlay/browser source.
		r.Get("/wrapped", wrappedH.Get)
	})

	if deps.Overlay != nil {
		r.Handle("/overlay/*", deps.Overlay)
	}

	if deps.Web != nil {
		r.Handle("/*", deps.Web)
	} else {
		r.Get("/", handlers.Index)
	}

	return r
}

package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	apimw "github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/api/ws"
	"github.com/Luca-Pelzer/engelos/internal/auth"
	"github.com/Luca-Pelzer/engelos/internal/counters"
	"github.com/Luca-Pelzer/engelos/internal/customcommands"
	"github.com/Luca-Pelzer/engelos/internal/featureflags"
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
	"github.com/Luca-Pelzer/engelos/internal/features/streak"
	"github.com/Luca-Pelzer/engelos/internal/moderation"
	"github.com/Luca-Pelzer/engelos/internal/redemptions"
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

// Deps bundles the narrow set of dependencies the router needs. Concrete
// implementations (auth, eventsourcing, etc.) live in their own packages and
// satisfy these interfaces — the api package never imports them directly.
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
	// GET /api/v1/stats. Nil hides the dispatcher block — the version
	// payload is still served.
	StatsProvider handlers.StatsProvider

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
	countersH := handlers.NewCounters(deps.CounterStore, deps.TenantID, logger)
	automodH := handlers.NewAutoMod(deps.Moderation, logger)
	featuresH := handlers.NewFeatures(deps.FeatureStore, deps.TenantID, logger)

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

	r.Route("/api/v1", func(r chi.Router) {
		if deps.AuthStore != nil {
			r.Use(apimw.SessionAuth(deps.AuthStore, "", logger))
		}
		r.Route("/auth", func(r chi.Router) {
			r.With(apimw.RateLimit(loginRPS, loginBurst)).Post("/login", authH.Login)
			r.Post("/logout", authH.Logout)
			if deps.OAuthTwitch != nil {
				r.Get("/twitch/login", deps.OAuthTwitch.Login)
				r.Get("/twitch/callback", deps.OAuthTwitch.Callback)
			}
		})
		r.Route("/users", func(r chi.Router) {
			r.Get("/me", authH.Me)
		})
		r.Get("/events", events.Stream)
		r.Get("/stats", statsH.Get)
		r.HandleFunc("/ws", wsh.ServeHTTP)

		r.Route("/pity", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Post("/grant", pityH.Grant)
			r.Post("/roll", pityH.Roll)
			r.Get("/status", pityH.Status)
			r.Get("/leaderboard", pityH.Leaderboard)
			r.Post("/reset", pityH.Reset)
		})

		r.Route("/streak", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Post("/tick", streakH.Tick)
			r.Post("/freeze", streakH.UseFreeze)
			r.Get("/status", streakH.Status)
			r.Get("/leaderboard", streakH.Leaderboard)
			r.Post("/reset", streakH.Reset)
		})

		r.Route("/redemptions", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Get("/", redemptionsH.List)
			r.Post("/", redemptionsH.Create)
			r.Put("/{rewardID}", redemptionsH.Update)
			r.Post("/{rewardID}/enabled", redemptionsH.SetEnabled)
			r.Delete("/{rewardID}", redemptionsH.Delete)
		})

		r.Route("/commands", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Get("/", commandsH.List)
			r.Post("/", commandsH.Create)
			r.Put("/{name}", commandsH.Update)
			r.Delete("/{name}", commandsH.Delete)
		})

		r.Route("/counters", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Get("/", countersH.List)
			r.Put("/{name}", countersH.Set)
			r.Post("/{name}/add", countersH.Add)
			r.Delete("/{name}", countersH.Delete)
		})

		r.Route("/automod", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Get("/config", automodH.GetConfig)
			r.Put("/config", automodH.PutConfig)
			r.Get("/audit", automodH.Audit)
		})

		r.Route("/features", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Get("/", featuresH.List)
			r.Put("/{feature}", featuresH.Set)
		})
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

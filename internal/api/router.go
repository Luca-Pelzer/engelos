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
	"github.com/Luca-Pelzer/engelos/internal/features/pity"
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

	// AuthStore backs the auth handlers and the SessionAuth middleware.
	// When nil, the auth routes degrade to 501 "not_implemented" and no
	// session middleware is mounted; this is the bootstrap-time state.
	AuthStore auth.Store

	// TenantID is the single-tenant identifier this daemon serves. It is
	// required when AuthStore is set; otherwise the empty string is fine.
	TenantID string

	// Pity, when non-nil, exposes the pity-system endpoints under
	// /api/v1/pity/*. Nil disables the feature (handlers return 501).
	Pity *pity.System
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
	authH := handlers.NewAuth(deps.AuthStore, deps.TenantID, logger)
	events := handlers.NewEvents(logger, deps.EventsHeartbeat)
	pityH := handlers.NewPity(deps.Pity, deps.TenantID, logger)

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
			r.Post("/login", authH.Login)
			r.Post("/logout", authH.Logout)
		})
		r.Route("/users", func(r chi.Router) {
			r.Get("/me", authH.Me)
		})
		r.Get("/events", events.Stream)
		r.HandleFunc("/ws", wsh.ServeHTTP)

		r.Route("/pity", func(r chi.Router) {
			if deps.AuthStore != nil {
				r.Use(apimw.RequireSession)
			}
			r.Post("/grant", pityH.Grant)
			r.Post("/roll", pityH.Roll)
			r.Get("/status", pityH.Status)
			r.Post("/reset", pityH.Reset)
		})
	})

	if deps.Web != nil {
		r.Handle("/*", deps.Web)
	} else {
		r.Get("/", handlers.Index)
	}

	return r
}

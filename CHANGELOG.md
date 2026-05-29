# Changelog

All notable changes to engelOS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Until the first stable release (1.0.0), expect breaking changes between minor
versions. CHANGELOG entries during the alpha phase focus on milestones, not
individual commits.

## [Unreleased]

## [0.0.7-alpha.1] — 2026-05-29

### Added
- **Live streak feature-events over WebSocket**: the runtime dispatcher now
  threads the streak tick outcome through and broadcasts
  `feature.streak.milestone` (when a viewer crosses 7/30/100/365 days) and
  `feature.streak.broken` (when a streak ends) to all connected WS clients, so
  dashboards can render live milestone moments. Decoupled via a new
  `runtime.StreakOutcome` type — the runtime package retains zero dependency on
  `internal/features/*` (verified with `go list -deps`).

### Fixed
- **WebSocket upgrade was silently broken** (`501 Not Implemented`): the
  `JSONContentType` middleware wrapped the `ResponseWriter` for all `/api/*`
  paths without implementing `http.Hijacker`, so `/api/v1/ws` could never be
  upgraded. Added a `Hijack` passthrough; the upgrade now returns
  `101 Switching Protocols` (verified live). This latent bug would have made the
  new feature-event broadcasts — and any browser/TUI live feed — undeliverable.

## [0.0.6-alpha.1] — 2026-05-29

### Added
- **Pity-Leaderboard** at `GET /api/v1/pity/leaderboard?channel=&limit=`:
  ranks viewers by accumulated pity points (Points desc, ViewerID asc tie-break).
  Empty `channel` aggregates across all channels in the tenant; `limit` defaults
  to 10 and is validated to 1..100. Mirrors the streak-leaderboard vertical slice
  end-to-end: `pity.ReadModel.Leaderboard` + `pity.System.Leaderboard` →
  `handlers.Pity.Leaderboard` → router route → TUI `Client.PityLeaderboard`
  (previously a stub). Verified live against a running daemon.

## [0.0.3-alpha.1] — 2026-05-29

### Added
- **Runtime dispatcher** (`internal/runtime`): fan-in goroutine that consumes
  every connected adapter's Events channel concurrently, routes
  `EventMessageCreated` → `pity.GrantPoints` (auto-credit on chat), and
  forwards every event to the WebSocket broadcast hub. Counts messages, subs,
  raids, pity-grant errors.
- **WS broadcast bridge**: wraps `ws.Hub` byte-sink with typed envelopes
  `{"type": "...", "data": <event>}` so dashboard clients get live activity
  without the runtime depending on the ws package directly.
- **Twitch adapter wired into `cmd/engelos`**: enabled via env vars,
  anonymous IRC by default. Verified live against `twitch.tv/engelswtf`.

### Environment
- New env vars: `ENGELOS_TWITCH_CHANNELS`, `ENGELOS_TWITCH_USERNAME`,
  `ENGELOS_TWITCH_OAUTH`, `ENGELOS_TWITCH_CLIENT_ID`. All optional; unset =
  Twitch adapter disabled.

## [0.0.2-alpha.2] — 2026-05-29

### Added
- **Pity-System HTTP API** at `/api/v1/pity/*`:
  - `POST /grant` — credit points to a viewer
  - `POST /roll` — evaluate the dice, lose or win (natural or guaranteed)
  - `GET /status` — current points / soft-pity flag / effective chance
  - `POST /reset` — admin clears a viewer's bucket
- Daemon opens a second SQLite file (`$ENGELOS_DATA_DIR/events.db`) for the
  event store, constructs the Pity system on boot, and calls `Recover()` to
  rebuild the read model from persisted events.
- Pity routes are session-protected (`RequireSession` middleware); requests
  without a valid `engelos_session` cookie return 401.
- Verified end-to-end via curl: grant returns running total, status reports
  points/soft-pity/effective-chance, roll respects `MaxPointsPerWindow` rate
  limit (saw a 100-point grant capped at 57 due to the 60/h cap).

## [0.0.2-alpha.1] — 2026-05-29

### Added
- **Twitch adapter** (`internal/adapters/twitch`): IRC via
  `gempir/go-twitch-irc/v4` + Helix via `nicklaw5/helix/v2`. Anonymous
  read-only mode (justinfan + random digits) is the default; authenticated
  mode (with OAUTH + ClientID) flips to send/moderate actions. Translates
  PRIVMSG / CLEARMSG / CLEARCHAT / USERNOTICE into platform-neutral
  `adapters.Event` values. 84.5% test coverage, 66 tests, race-clean.
- **Discord adapter** (`internal/adapters/discord`): via `bwmarrin/discordgo`.
  Translates MessageCreate / MessageDelete / Ready / Disconnect into
  `adapters.Event`. Channel→GuildID mapping cached on Ready so moderation
  actions resolve without extra REST calls. 35 tests.
- **Pity-System** (`internal/features/pity`): event-sourced gacha mechanic.
  Viewers earn points; rolls draw against a probability that ramps from
  `BaseWinChance` up to 1.0 between `SoftPityFraction` and
  `HardPityThreshold`; past the threshold the next roll is guaranteed. Crypto
  RNG in production, PCG-seeded in tests. 89.3% coverage, 32 tests.
- **Web embed** (`internal/web`): `go:embed all:build` wrapper for the
  prerendered SvelteKit dashboard. Two-flavour build: with embed
  (`make web-build`) or without (dev iteration). Hashed `_app/*` assets get
  `Cache-Control: immutable`; HTML routes get `no-store`. Falls back to a
  JSON landing page when no UI is embedded.
- **Auth handlers** (`internal/api/handlers/auth.go`): real Login / Logout /
  Me wired to `internal/auth.Store`. Sessions are opaque tokens persisted as
  hashes; cookies are HttpOnly + Secure + SameSite=Strict with a 30-day
  default TTL.
- **Session middleware** (`internal/api/middleware/session.go`): reads
  `engelos_session`, looks up the user, injects via context. `RequireSession`
  returns 401 when absent.
- **Timing-equalised login**: pre-computed dummy Argon2id hash on the auth
  bundle; the unknown-email path still runs `VerifyPassword` against it so
  response time leaks nothing about which credential is wrong.

### Changed
- **Module path** renamed from `github.com/engelswtf/engelos` to
  `github.com/Luca-Pelzer/engelos` to match the actual publishing location.

### Verified
- Full auth flow end-to-end via curl: login → 200 + Set-Cookie, me → 200
  sanitized JSON (no PasswordHash, no TOTPSecret), logout → 204 + cookie
  cleared, me-after-logout → 401.

## [0.0.1-alpha.1] — 2026-05-28

### Added
- Initial repository skeleton with Go 1.25 daemon and `go build` producing a
  ~7 MB static binary that serves `/healthz` on `127.0.0.1:8080`.
- Master Vision Plan (`docs/MASTER-VISION.md`, 1366 lines) covering 5-7 year
  roadmap, stack decisions (Go + Wails + Bubble Tea + Svelte), dual-license
  strategy (AGPL-3.0 core + Apache-2.0 SDK), and 8 architecture principles.
- **Event-sourcing engine** (`internal/eventsourcing`): SQLite append-only
  store, ULID-based event IDs, multi-tenant isolation, `iter.Seq2` reads,
  embedded migrations, WAL mode, STRICT tables. 11 tests.
- **Platform adapter interface** (`internal/adapters`): `Platform` contract,
  platform-agnostic `Event` and `Action` types, in-memory `Mock` impl. 11
  tests.
- **Auth system** (`internal/auth`): users, sessions, RBAC (Owner / Admin /
  Mod / Viewer), API keys with scopes + IP whitelist, Argon2id password
  hashing. 24 tests.
- **HTTP API server skeleton** (`internal/server` + `internal/api`): chi
  router, CORS / security / logging middleware, WebSocket hub via
  `coder/websocket`, SSE event stream.
- **SvelteKit dashboard skeleton** (`web/`): Svelte 5 + SvelteKit 2 +
  Tailwind 4. Login, setup wizard, dashboard, chat viewer, commands,
  integrations, settings, upgrade-to-Cloud pages — all prerendered.
- OSS hygiene: `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`,
  `.gitignore`, GoReleaser config for Linux/macOS/Windows, GitHub Actions CI
  + release workflows, distroless Dockerfile.

### Infrastructure
- `EngelGuard` (legacy Python bot, predecessor) restored to service after 11
  days of downtime — systemd hardenings incompatible with unprivileged LXC
  were removed; bot resumed on `#engelswtf`.

---

## Phase milestones (forward-looking)

These are not releases yet — they are the roadmap from
`docs/MASTER-VISION.md`:

### Phase 1 (June 2026 – December 2026)
Core daemon with 6+ killer features, OSS public launch.

### Phase 2 (January 2027 – June 2027)
Cloud version live, Native GUI apps (Wails) on Windows / macOS, 100-1.000
streamers.

### Phase 3 (July 2027 – June 2028)
Monetisation flip, 5.000 streamers, profitable.

### Phase 4 (July 2028 – June 2030)
Network effects, 50.000 streamers, "industry standard" perception.

### Phase 5 (2030+)
Strategic inflection point — lifestyle business, VC raise, or strategic
acquisition.

[Unreleased]: https://github.com/Luca-Pelzer/engelos/compare/v0.0.3-alpha.1...HEAD
[0.0.3-alpha.1]: https://github.com/Luca-Pelzer/engelos/compare/v0.0.2-alpha.2...v0.0.3-alpha.1
[0.0.2-alpha.2]: https://github.com/Luca-Pelzer/engelos/compare/v0.0.2-alpha.1...v0.0.2-alpha.2
[0.0.2-alpha.1]: https://github.com/Luca-Pelzer/engelos/compare/v0.0.1-alpha.1...v0.0.2-alpha.1
[0.0.1-alpha.1]: https://github.com/Luca-Pelzer/engelos/releases/tag/v0.0.1-alpha.1

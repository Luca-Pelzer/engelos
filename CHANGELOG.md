# Changelog

All notable changes to engelOS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Until the first stable release (1.0.0), expect breaking changes between minor
versions. CHANGELOG entries during the alpha phase focus on milestones, not
individual commits.

## [Unreleased]

### Changed
- **AI context-moderation is now category- and severity-aware.** The escalator
  no longer maps every AI hit to a blanket delete-or-10-minute-timeout. Claude
  now returns a structured verdict (category, severity 0-3, confidence) and a
  policy decides the action to fit the situation:
  - Spam and advertising (foreign links, follow-for-follow, bot sites) is
    deleted immediately and NEVER feeds the repeat-offender escalation ladder,
    because hit-and-run spammers do not care about a later ban; only deleting
    the message in the moment matters.
  - Jokes and low-severity or low-confidence messages are not timed out (deleted
    at most, or just audited when the model is unsure), so a harmless joke does
    not get someone punished.
  - Medium and high severity (harassment, threats, hate, doxxing) is timed out
    AND fed into the same escalation ladder the rule-based AutoMod uses, so
    repeat offenders escalate toward a ban.
  - A cheap local pre-filter keeps clearly-harmless chatter (very short
    messages, pure emote spam) off the paid backend while always escalating
    risk signals (links, spam markers); a short-lived dedup cache classifies
    identical copy-paste spam only once. Both cut cost and latency without
    weakening moderation. New `Service.EscalateExternal` feeds AI hits into the
    ladder while honouring the dry-run and audit discipline. Fail-open and the
    per-channel opt-in are unchanged.

### Added
- **Timer and Manual triggers for the Action-Engine.** Rules can now fire on a
  fixed interval, not just on chat events and commands. A new scheduler loads
  every enabled timer rule on startup and reloads automatically when rules
  change, so timing edits take effect without a restart; the minimum cadence is
  5 seconds and the scheduler shuts down cleanly without leaking goroutines.
  Operators can also test-fire any rule on demand via a new "run now" button
  (POST `/actions/{name}/fire`), which runs the rule's conditions and actions
  once. The rule builder UI gains a Timer interval field, a Manual option, and
  a per-row run button, and the action catalog now enumerates the available
  trigger kinds. Backend covered by race-tested scheduler and handler tests.

### Fixed
- **Follow-up hardening (low-severity audit findings):**
  - Event ids are now minted from a mutex-guarded monotonic ULID source in both
    `adapters.NewEventID` and `eventsourcing.NewEvent`. Previously they used
    non-monotonic entropy, so two events created in the same millisecond could
    sort in random order, which the event store's id-ordered replay and
    `AfterID` cursor rely on. This matters more now that the dispatcher mints
    ids from several per-channel workers concurrently; the mutex keeps it
    race-free. Added monotonicity and concurrent-uniqueness regression tests.
  - The WebSocket handshake now logs a warning when the daemon is exposed
    (`ENGELOS_ALLOW_LAN`) without an `ENGELOS_ALLOWED_ORIGINS` allowlist, since
    that combination leaves the socket accepting any Origin. The deployed
    instance now sets the allowlist to the dashboard host. The `/api/v1/ws`
    endpoint remains gated behind owner auth regardless; the origin allowlist is
    defense in depth.
  - Audited and dismissed several reported issues as false alarms: invitation
    expiry is enforced, invitation tokens use 192 bits of crypto/rand, the heist
    game renders display names (not ids), and language detection handles
    Japanese kana correctly.

- **Hardening pass: 15 bugs found by a full-codebase audit, fixed and verified.**
  Each fix ships with regression tests and the whole tree passes
  `go test -race ./...` (48 packages).
  - Action-Engine: a shutdown race that could `panic` on send to a closed
    channel, a serial-queue goroutine leak/deadlock, a context-cancellation bug
    that skipped every action after the first (so "send chat, wait, switch
    scene" silently dropped the wait and everything after it), and a
    condition-gate that fired none-mode rules when a condition type was unknown.
  - Commands: `$(random a b)` with extreme operands could overflow and `panic`
    out of the dispatcher (any viewer could crash the bot); the expansion path
    is now overflow-guarded and wrapped in panic-recovery. Counter values that
    overflowed int64 permanently corrupted the row; they now saturate and stay
    readable.
  - Economy: `!give` credited the recipient under their login instead of their
    numeric id, so gifted points landed in a phantom account and effectively
    vanished; dashboard point adjustments had the same divergent-keyspace bug.
    Both now resolve to the stable numeric id and fail closed when it cannot be
    resolved.
  - Moderation: dry-run/shadow mode still mutated the persistent escalation
    ladder and burned link permits (a user could be one real strike from a ban
    after shadow testing); AI verdicts ignored dry-run and were never audited;
    and a banned-word regex matching the empty string punished every message.
  - Streak: a grace-window scale mismatch broke streaks for late-night
    streamers whose ticks fell inside the grace window.
  - Song requests: the queue could leave multiple tracks marked "playing" at
    once (now atomic and self-correcting), and played rows are pruned so the
    table cannot grow without bound.
  - Runtime: the single per-platform consumer blocked on Helix calls while the
    adapter's bounded buffer silently dropped chat events under load. Events are
    now processed by per-channel workers, so a slow channel no longer starves
    the others; per-channel ordering is preserved and a dropped-event counter
    is exposed in stats.

### Added
- **OBS action config forms in the Action-Engine rule builder**: the dashboard
  rule builder now renders config inputs for the two OBS actions that the
  backend already supported but the UI could not configure. `obs:switch-scene`
  gets a scene-name field, and `obs:set-source-visibility` gets scene + source
  fields plus a "visible" toggle. Both map directly to the backend config keys
  (`scene`, `source`, `visible`), so OBS actions are now fully usable end-to-end
  from the UI instead of being wired-but-unreachable.

### Security
- **Pinned the build toolchain to Go 1.25.11** via a `toolchain` directive in
  `go.mod`. The previous Go 1.25.0 standard library was reachable for three
  fixed CVEs: GO-2025-4008 (`crypto/tls` ALPN error leaks attacker-controlled
  data, used by the HTTP server, Twitch IRC TLS and the Claude client),
  GO-2025-4007 (`crypto/x509` quadratic name-constraint checking) and
  GO-2025-4009 (`encoding/pem` quadratic parsing). `govulncheck ./...` now
  reports zero vulnerabilities. Verified live after a rebuild and redeploy.

## [0.0.7-alpha.1] - 2026-05-29

### Added
- **Live streak feature-events over WebSocket**: the runtime dispatcher now
  threads the streak tick outcome through and broadcasts
  `feature.streak.milestone` (when a viewer crosses 7/30/100/365 days) and
  `feature.streak.broken` (when a streak ends) to all connected WS clients, so
  dashboards can render live milestone moments. Decoupled via a new
  `runtime.StreakOutcome` type - the runtime package retains zero dependency on
  `internal/features/*` (verified with `go list -deps`).

### Fixed
- **WebSocket upgrade was silently broken** (`501 Not Implemented`): the
  `JSONContentType` middleware wrapped the `ResponseWriter` for all `/api/*`
  paths without implementing `http.Hijacker`, so `/api/v1/ws` could never be
  upgraded. Added a `Hijack` passthrough; the upgrade now returns
  `101 Switching Protocols` (verified live). This latent bug would have made the
  new feature-event broadcasts - and any browser/TUI live feed - undeliverable.

## [0.0.6-alpha.1] - 2026-05-29

### Added
- **Pity-Leaderboard** at `GET /api/v1/pity/leaderboard?channel=&limit=`:
  ranks viewers by accumulated pity points (Points desc, ViewerID asc tie-break).
  Empty `channel` aggregates across all channels in the tenant; `limit` defaults
  to 10 and is validated to 1..100. Mirrors the streak-leaderboard vertical slice
  end-to-end: `pity.ReadModel.Leaderboard` + `pity.System.Leaderboard` →
  `handlers.Pity.Leaderboard` → router route → TUI `Client.PityLeaderboard`
  (previously a stub). Verified live against a running daemon.

## [0.0.3-alpha.1] - 2026-05-29

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

## [0.0.2-alpha.2] - 2026-05-29

### Added
- **Pity-System HTTP API** at `/api/v1/pity/*`:
  - `POST /grant` - credit points to a viewer
  - `POST /roll` - evaluate the dice, lose or win (natural or guaranteed)
  - `GET /status` - current points / soft-pity flag / effective chance
  - `POST /reset` - admin clears a viewer's bucket
- Daemon opens a second SQLite file (`$ENGELOS_DATA_DIR/events.db`) for the
  event store, constructs the Pity system on boot, and calls `Recover()` to
  rebuild the read model from persisted events.
- Pity routes are session-protected (`RequireSession` middleware); requests
  without a valid `engelos_session` cookie return 401.
- Verified end-to-end via curl: grant returns running total, status reports
  points/soft-pity/effective-chance, roll respects `MaxPointsPerWindow` rate
  limit (saw a 100-point grant capped at 57 due to the 60/h cap).

## [0.0.2-alpha.1] - 2026-05-29

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

## [0.0.1-alpha.1] - 2026-05-28

### Added
- Initial repository skeleton with Go 1.25 daemon and `go build` producing a
  ~7 MB static binary that serves `/healthz` on `127.0.0.1:8080`.

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
  integrations, settings, upgrade-to-Cloud pages - all prerendered.
- OSS hygiene: `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`,
  `.gitignore`, GoReleaser config for Linux/macOS/Windows, GitHub Actions CI
  + release workflows, distroless Dockerfile.

### Infrastructure
- `EngelGuard` (legacy Python bot, predecessor) restored to service after 11
  days of downtime - systemd hardenings incompatible with unprivileged LXC
  were removed; bot resumed on `#engelswtf`.

---

## Phase milestones (forward-looking)

These are not releases yet - they are the forward-looking roadmap:

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
Strategic inflection point - lifestyle business, VC raise, or strategic
acquisition.

[Unreleased]: https://github.com/Luca-Pelzer/engelos/compare/v0.0.3-alpha.1...HEAD
[0.0.3-alpha.1]: https://github.com/Luca-Pelzer/engelos/compare/v0.0.2-alpha.2...v0.0.3-alpha.1
[0.0.2-alpha.2]: https://github.com/Luca-Pelzer/engelos/compare/v0.0.2-alpha.1...v0.0.2-alpha.2
[0.0.2-alpha.1]: https://github.com/Luca-Pelzer/engelos/compare/v0.0.1-alpha.1...v0.0.2-alpha.1
[0.0.1-alpha.1]: https://github.com/Luca-Pelzer/engelos/releases/tag/v0.0.1-alpha.1

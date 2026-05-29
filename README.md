# engelOS

> **The streaming bot that remembers you. Open source. Run it anywhere.**

engelOS is an open-source streaming bot for Twitch, Discord, YouTube Live, and Kick.
Self-host it on Linux, macOS, or Windows — eventually with a managed Cloud version
for streamers who don't want to run a server themselves.

## Status

**Phase 1B — Alpha.** Core daemon works end-to-end. Phase 0 (skeleton) and Phase 1A
(foundations) are complete. The first real engagement feature (Pity-System) ships
working over HTTP and is auto-credited from Twitch chat. Public OSS launch is still
targeted for **December 2026** — APIs and database schema may change without notice
until then.

Currently working:

- ✅ **Twitch IRC adapter** (anonymous + authenticated modes via `gempir/go-twitch-irc`)
- ✅ **Discord adapter** (skeleton via `bwmarrin/discordgo`, needs bot token)
- ✅ **Event-sourcing engine** (SQLite append-only, ULID IDs, multi-tenant)
- ✅ **Argon2id auth** with HttpOnly+Secure+SameSite=Strict session cookies
- ✅ **Pity-System** (gacha mechanic): viewers earn points by chatting, soft-pity
  ramps win chance, hard-pity guarantees a win — fully event-sourced and replayable
- ✅ **Runtime dispatcher**: fans Twitch events into pity-grants automatically
- ✅ **HTTP API** with chi router, security headers, SSE event stream, WebSocket hub
- ✅ **SvelteKit dashboard** embedded into the binary via `go:embed`

Not done yet (Phase 1B-4+):

- Streak-System (Duolingo-style)
- AI Auto-Clipper, Real-Time Translator, Stream-Wrapped (Tier-A features)
- AI Co-Host, Context-Aware AI-Moderator (Tier-B, Cloud-only)
- TUI via Bubble Tea
- Native GUI app via Wails v2
- Cloud-Premium variant

See [`docs/MASTER-VISION.md`](docs/MASTER-VISION.md) for the full 5-7 year roadmap.

## Vision

| | |
|---|---|
| **License** | AGPL-3.0 (Core) · Apache-2.0 (SDK) · Proprietary (Cloud) |
| **Stack** | Go 1.25 · Wails v2 · Bubble Tea · Svelte 5 · Tailwind 4 |
| **Platforms** | Linux · macOS · Windows · Docker · Raspberry Pi |
| **Roadmap** | [`docs/MASTER-VISION.md`](docs/MASTER-VISION.md) |

## Run it now (anonymous Twitch read-only)

```bash
git clone https://github.com/Luca-Pelzer/engelos.git
cd engelos
go build -o engelos ./cmd/engelos

# Anonymous Twitch — no credentials needed
ENGELOS_TWITCH_CHANNELS=engelswtf ./engelos
```

The daemon listens on `http://127.0.0.1:8080` (loopback only by default).
Optionally `make web-build` first to embed the SvelteKit dashboard into the
binary; without it `/` serves a JSON status page.

### Live HTTP API (Phase 1B)

```
GET  /version
GET  /healthz
GET  /readyz
POST /api/v1/auth/login      → {email, password}
POST /api/v1/auth/logout
GET  /api/v1/users/me
POST /api/v1/pity/grant      → {channel, viewer_id, amount?, reason?}
POST /api/v1/pity/roll       → {channel, viewer_id}
GET  /api/v1/pity/status?channel=...&viewer_id=...
POST /api/v1/pity/reset
GET  /api/v1/events          → Server-Sent Events stream
     /api/v1/ws              → WebSocket live event feed
```

All `/api/v1/*` routes require a valid session cookie (set by `/auth/login`).

### Environment variables

| Var | Meaning | Default |
|---|---|---|
| `ENGELOS_DATA_DIR` | Where SQLite DBs live | `$XDG_DATA_HOME/engelos` |
| `ENGELOS_TWITCH_CHANNELS` | Comma-separated channels to JOIN | unset (Twitch disabled) |
| `ENGELOS_TWITCH_USERNAME` | Bot login (authenticated mode) | empty (anonymous) |
| `ENGELOS_TWITCH_OAUTH` | OAuth token (`oauth:` prefix optional) | empty (anonymous) |
| `ENGELOS_TWITCH_CLIENT_ID` | Helix Client-ID (required with OAUTH) | empty |

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  engelOS Core Daemon (Go, single static binary)          │
│                                                          │
│  ┌────────────────────┐    ┌────────────────────────┐    │
│  │ Platform Adapters  │ →  │  Runtime Dispatcher    │ →  │
│  │  Twitch · Discord  │    │  (fan-in goroutine)    │    │
│  │  YouTube · Kick    │    └────────────────────────┘    │
│  └────────────────────┘             │                    │
│                                     ↓                    │
│  ┌────────────────────┐    ┌────────────────────────┐    │
│  │  Auth (Argon2id +  │    │  Features              │    │
│  │  RBAC + API keys)  │    │   Pity · Streak · ...  │    │
│  └────────────────────┘    └────────────────────────┘    │
│            │                        │                    │
│            └────────┬───────────────┘                    │
│                     ↓                                    │
│  ┌────────────────────────────────────────────────────┐  │
│  │ Event-Sourcing Engine (SQLite WAL, append-only)    │  │
│  └────────────────────────────────────────────────────┘  │
│                                                          │
│  HTTP/WebSocket/SSE API on 127.0.0.1:8080                │
└──────────────────────────────────────────────────────────┘
        ▲              ▲                  ▲
   ┌────┴───┐    ┌─────┴──────┐    ┌──────┴──────┐
   │ TUI    │    │ Web UI     │    │ Native GUI  │
   │ (BTea) │    │ (Svelte 5) │    │ (Wails v2)  │
   └────────┘    └────────────┘    └─────────────┘
```

## Repository layout

```
cmd/engelos/             Daemon entry point
internal/
  adapters/              Platform interfaces + twitch/, discord/, mock/
  api/                   chi router, handlers, middleware, WebSocket hub
  auth/                  Users, sessions, RBAC, API keys, Argon2id
  eventsourcing/         SQLite append-only event log
  features/
    pity/                Gacha mechanic (Tier-A #1 — shipped)
    streak/              Duolingo-style streaks (Tier-A #2 — in progress)
  runtime/               Dispatcher: adapter events → features + broadcast
  server/                HTTP server lifecycle
  web/                   go:embed wrapper for SvelteKit build/
pkg/sdk/                 Public SDK (Apache-2.0) for third-party plugins
web/                     Svelte 5 frontend (local + cloud variants)
docs/                    MASTER-VISION.md and other long-form docs
```

## Contributing

Not accepting external contributions yet — the codebase is moving too fast and
the public APIs aren't stable. Once Phase 1 ships and the OSS launch happens
(December 2026), see `CONTRIBUTING.md`.

## License

- **Core daemon** (this repository, default): **AGPL-3.0** — see [`LICENSE`](LICENSE)
- **SDK** (`pkg/sdk/`): **Apache-2.0** — see [`pkg/sdk/LICENSE`](pkg/sdk/LICENSE)
- **Cloud features** (Phase 2+): proprietary, not in this repository.

The dual-license follows the [Grafana model](https://grafana.com/licensing/):
core is protected from cloud reselling (AGPL), the SDK is open so any company
or contributor can build integrations against it without AGPL burden (Apache).

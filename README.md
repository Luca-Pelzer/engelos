<div align="center">

# engelOS

### The open-source streaming bot that does what the paid ones do, and the things they won't.

[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![SDK: Apache-2.0](https://img.shields.io/badge/SDK-Apache--2.0-green.svg)](pkg/sdk/LICENSE)
[![Platforms](https://img.shields.io/badge/Linux%20·%20macOS%20·%20Windows-self--hosted-success.svg)](#-quick-start)
[![Status: Alpha](https://img.shields.io/badge/status-Phase%201%20alpha-orange.svg)](#-project-status)
[![Successor to EngelGuard](https://img.shields.io/badge/successor%20to-EngelGuard-9146FF.svg)](https://github.com/Luca-Pelzer/engelguard)

**One fast, self-hosted binary for Twitch and Discord.**
Moderation, custom commands, a loyalty economy, mini-games, and provably-fair giveaways.
Free forever, no premium tier, no vendor lock-in, your data on your machine.

[Why engelOS?](#-why-engelos) • [Features](#-features) • [Quick Start](#-quick-start) • [Configuration](#-configuration) • [Architecture](#-architecture) • [Status](#-project-status)

</div>

---

engelOS is a **suite**, not a single bot. It grew out of
**[EngelGuard](https://github.com/Luca-Pelzer/engelguard)**, an earlier Python Twitch bot. Building
EngelGuard made one thing clear: what I actually wanted was far bigger than any single bot, so I
rebuilt everything from scratch in Go as one coherent suite. EngelGuard didn't disappear; it's the
moderation bot **inside** engelOS. The Twitch and Discord bots are still EngelGuard, and engelOS is
the bigger home they live in, tying moderation, commands, a points economy, games, giveaways, and
engagement systems together in a single static binary with an embedded web dashboard.

> [!IMPORTANT]
> **Phase 1 alpha.** The core is live and tested under Go's race detector, but the public OSS launch
> is targeted for **December 2026**, so APIs and the database schema may still change without notice.
> Not accepting external contributions yet (the codebase moves fast).

---

## 🎯 Why engelOS?

Tired of Nightbot Premium, StreamElements' limits, or cloud bots that own your data?

- 🟢 **Free forever.** No premium upsells, no feature paywalls.
- 🏠 **Self-hosted.** Runs on your Linux, macOS, or Windows machine, loopback-only by default.
- 🔓 **Open source.** AGPL-3.0 core plus Apache-2.0 SDK; modify and extend anything.
- 🔌 **No vendor lock-in.** It's a SQLite-backed binary; your data never leaves your server.
- ⚡ **One binary, one suite.** Everything in a single Go build with the dashboard embedded, no runtime to install.

Plus a few things the big bots simply don't offer:

- 🎲 **Provably-fair giveaways.** The draw seed is published when the giveaway opens, so anyone can
  verify the winner wasn't rigged.
- 🛡️ **AutoMod with an audit log and dry-run mode.** Test moderation rules in shadow mode before they
  ever time anyone out, and review every action after the fact.
- 🪙 **A real points economy with mini-games.** Earn, gift, gamble, duel, and heist, with anti-farming
  and a store that can never go negative.
- 🧮 **A `$(...)` variable system** for custom commands, including a real `$(math …)` evaluator.

### Quick comparison

| | engelOS | Nightbot | StreamElements | EngelGuard (Python) |
|---|:---:|:---:|:---:|:---:|
| Cost | **Free forever** | Free + Premium | Free + Premium | Free |
| Self-hosted | ✅ | ❌ cloud only | ❌ cloud only | ✅ |
| Open source | ✅ AGPL-3.0 | ❌ | ❌ | ✅ MIT |
| Custom commands + `$(...)` vars | ✅ incl. `$(math)` | ✅ | ✅ | ✅ |
| AutoMod audit log + dry-run | ✅ | ❌ | ❌ | partial |
| Loyalty economy + mini-games | ✅ | ❌ | ✅ | ✅ |
| Provably-fair giveaways | ✅ | ❌ | ❌ | ❌ |
| Single binary | ✅ Go | n/a | n/a | ❌ Python |
| Multi-platform | Twitch + Discord, YouTube/Kick 🚧 | Twitch/YT | Twitch/YT | Twitch |

---

## ✨ Features

### 🛡️ Moderation (AutoMod)
Seven configurable filters, each with **per-filter role exemptions**:

- **Caps** (ratio-based, not a naive count), **symbols and zalgo**, **links** (allow-list plus `!permit`),
  **emote limits**, **message length**, **repetition**, and **banned words** (5 match modes incl. regex).
- **Escalation ladder**: warn, 60s, 10m, 24h, ban, with a decay window.
- **Audit log** of every enforcement action, viewable in the dashboard.
- **Dry-run / shadow mode**: see exactly what would happen without punishing anyone.

### 💬 Commands and Variables
- **Custom commands**: `!addcom`, `!editcom`, `!delcom` with a Nightbot-style variable system:
  `$(user)` `$(touser)` `$(channel)` `$(args)` `$(1)…$(9)` `$(random a b)` `$(random.pick …)`
  `$(time)` and a real **`$(math 1+2*3)`** recursive-descent evaluator (not arbitrary code-eval).
- **Quotes**: `!quote`, `!addquote`, `!delquote`. **Counters**: `!counter`, `+`, `−`, `set`, `reset`.
  **Timers** and auto-announcements.
- **Stream info**: `!uptime` `!game` `!title` `!accountage` `!so` (shoutout with last category).
- **Fun**: `!8ball` `!lurk` `!unlurk` `!dice` `!roll` `!love` `!ship` `!hug` `!slap`.

### 🪙 Loyalty and Mini-Games
- **Economy**: earn points by chatting with a per-viewer cooldown (**anti-farming**, so idle and bot
  accounts can't grind), `!points`, `!give`, `!pointslb`. The store is atomic and **can never overdraw**.
- **Games**: `!gamble` (double-or-nothing with a documented house edge), `!slots` (weighted reels),
  `!duel` (PvP wager, both players must afford the stake before any points move), and `!heist`
  (async multiplayer group game). No player can ever go negative.

### 🎁 Giveaways
- `!giveaway`, `!enter`, `!draw`, `!reroll` with a **provably-fair draw**:
  `winner = SHA256(seed │ drawNumber │ sorted-entrant-ids) mod N`, where the seed is announced at open
  time, so the draw is publicly verifiable and un-riggable.

### 🎮 Engagement systems
- **Pity-System** (gacha): soft-pity ramps win chance, hard-pity guarantees, fully event-sourced and replayable.
- **Streak-System** (Duolingo-style daily streaks).
- **Live-Ops calendar**: `!nextevent`, `!schedule`.
- **Channel-Points trigger engine**: bind a Twitch reward to a bot action.

### 🖥️ Platform and Dashboard
- **Embedded SvelteKit web dashboard** (via `go:embed`) with live pages: Home (real daemon stats),
  Channel Points, Commands, Counters, AutoMod (filter config plus audit-log viewer), and Login.
- **Event-sourcing engine** (SQLite WAL, append-only, ULID, multi-tenant).
- **Auth**: Argon2id, RBAC, sessions (HttpOnly/Secure/SameSite cookies), and API keys.
- **HTTP API** (chi router, security headers), **Server-Sent Events** stream, and a **WebSocket** hub.
- **Adapters**: Twitch (IRC plus Helix plus EventSub WebSocket, anonymous or authenticated) and Discord
  (needs a bot token). Around 32 Go packages, all tested under the race detector.

---

## 🚀 Quick Start

```bash
git clone https://github.com/Luca-Pelzer/engelos.git
cd engelos

make web-build          # build + embed the dashboard (optional; without it, / serves JSON)
go build -o engelos ./cmd/engelos

# Anonymous, read-only Twitch, no credentials needed
ENGELOS_TWITCH_CHANNELS=yourchannel ./engelos
```

The daemon listens on **`http://127.0.0.1:8080`** (loopback only by default). Open it in a browser to
reach the dashboard, or a JSON status page if you skipped `make web-build`.

---

## ⚙️ Configuration

All configuration is via environment variables:

| Variable | Meaning | Default |
|---|---|---|
| `ENGELOS_DATA_DIR` | Where the SQLite databases live | `$XDG_DATA_HOME/engelos` |
| `ENGELOS_ADDR` | HTTP listen address | `127.0.0.1:8080` |
| `ENGELOS_TWITCH_CHANNELS` | Comma-separated channels to join | unset (Twitch disabled) |
| `ENGELOS_TWITCH_USERNAME` | Bot login (authenticated mode) | empty (anonymous) |
| `ENGELOS_TWITCH_OAUTH` | OAuth token (`oauth:` prefix optional) | empty (anonymous) |
| `ENGELOS_TWITCH_CLIENT_ID` | Helix Client-ID (required with OAuth) | empty |
| `ENGELOS_SECRETS_KEY` | Enables encrypted OAuth/login storage (`openssl rand -base64 32`) | unset |

> [!NOTE]
> Anonymous mode is read-only (chat-reading and counters). Moderation actions, the dashboard login,
> and OAuth-gated features need an authenticated bot account and `ENGELOS_SECRETS_KEY`.

---

## 🏗️ Architecture

```
┌──────────────────────────────────────────────────────────┐
│  engelOS Core Daemon (Go, single static binary)          │
│                                                          │
│  ┌────────────────────┐    ┌────────────────────────┐    │
│  │ Platform Adapters  │ →  │  Runtime Dispatcher    │    │
│  │  Twitch · Discord  │    │  (fan-in goroutine)    │    │
│  │  YouTube/Kick 🚧   │    └───────────┬────────────┘    │
│  └────────────────────┘                │                 │
│                                        ↓                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐    │
│  │  AutoMod     │  │  Commands +  │  │  Economy +   │    │
│  │  (7 filters, │  │  $(...) vars │  │  Games +     │    │
│  │  audit log)  │  │  · Quotes    │  │  Giveaways   │    │
│  └──────────────┘  └──────────────┘  └──────────────┘    │
│  ┌──────────────┐  ┌──────────────────────────────┐      │
│  │  Auth        │  │  Pity · Streak · Live-Ops ·   │      │
│  │  (Argon2id,  │  │  Channel-Points · Counters    │      │
│  │  RBAC, keys) │  └──────────────────────────────┘      │
│  └──────────────┘                │                       │
│            └────────┬────────────┘                       │
│                     ↓                                    │
│  ┌────────────────────────────────────────────────────┐  │
│  │ Event-Sourcing Engine (SQLite WAL, append-only)    │  │
│  └────────────────────────────────────────────────────┘  │
│                                                          │
│  HTTP / WebSocket / SSE API on 127.0.0.1:8080            │
└──────────────────────────────────────────────────────────┘
        ▲                  ▲                    ▲
   ┌────┴────┐    ┌────────┴───────┐    ┌───────┴────────┐
   │ TUI 🚧  │    │  Web Dashboard │    │ Native GUI 🚧  │
   │ (BTea)  │    │  (Svelte 5) ✅ │    │ (Wails v2)     │
   └─────────┘    └────────────────┘    └────────────────┘
```

---

## 📦 Project status

**✅ Built and live (Phase 1 alpha):**

- Twitch (IRC, Helix, EventSub) and Discord adapters
- AutoMod (7 filters, escalation, audit log, dry-run) plus dashboard config
- Custom commands and `$(...)` variable system, quotes, counters, timers
- Loyalty economy and mini-games (`!gamble` `!slots` `!duel` `!heist`)
- Provably-fair giveaways
- Pity, Streak, Live-Ops, Channel-Points trigger engine
- Fun and info commands (`!8ball` `!so` `!accountage`, and more)
- Event-sourcing, Argon2id auth, REST/SSE/WebSocket API, embedded SvelteKit dashboard

**🚧 Planned (roadmap):**

- YouTube and Kick adapters
- AI features: Auto-Clipper, real-time Translator, context-aware AI-Mod, AI Co-Host, AI-Voice/TTS
- Stream-Wrapped, Addon/Script marketplace
- TUI (Bubble Tea), native GUI (Wails v2)
- Managed Cloud-Premium tier (Phase 2+, not in this repo)

See [`docs/MASTER-VISION.md`](docs/MASTER-VISION.md) for the full multi-year roadmap.

---

## 🗂️ Repository layout

```
cmd/engelos/             Daemon entry point + wiring
internal/
  adapters/              Platform interfaces + twitch/, discord/, mock/
  api/                   chi router, handlers, middleware, WebSocket hub
  auth/                  Users, sessions, RBAC, API keys, Argon2id
  automod/               Stateless filter engine
  automodstate/          Escalation ladder + audit-log store
  moderation/            Glue: filters + escalation + audit into one Service
  commands/              ~30 builtins, custom commands, $(...) vars, games, giveaways
  counters/ quotes/ timers/ customcommands/ redemptions/ loyalty/ rewards/
  eventsourcing/         SQLite append-only event log
  features/pity/ streak/ Engagement systems
  runtime/               Dispatcher: adapter events into features + broadcast
  server/  web/          HTTP lifecycle + go:embed of the SvelteKit build
pkg/sdk/                 Public SDK (Apache-2.0) for third-party plugins
web/                     Svelte 5 frontend (local + cloud variants)
docs/                    MASTER-VISION.md and long-form docs
```

---

## 🤝 Contributing

Not accepting external contributions **yet**: the codebase moves too fast and the public APIs aren't
stable. Once Phase 1 ships and the OSS launch happens (December 2026), contribution guidelines will
land in `CONTRIBUTING.md`. Stars and issues are very welcome in the meantime.

---

## 📄 License

- **Core daemon** (this repository): **AGPL-3.0**, see [`LICENSE`](LICENSE)
- **SDK** (`pkg/sdk/`): **Apache-2.0**, see [`pkg/sdk/LICENSE`](pkg/sdk/LICENSE)
- **Cloud features** (Phase 2+): proprietary, not in this repository.

The dual license follows the [Grafana model](https://grafana.com/licensing/): the core is protected
from cloud reselling (AGPL), while the SDK stays permissive (Apache) so anyone can build integrations
against it without AGPL obligations.

<div align="center">

**engelOS**, built in the open, the successor to [EngelGuard](https://github.com/Luca-Pelzer/engelguard).

</div>

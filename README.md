<div align="center">

# EngelOS

### The open-source streaming bot that does what the paid ones do, and the things they won't.

[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![SDK: Apache-2.0](https://img.shields.io/badge/SDK-Apache--2.0-green.svg)](pkg/sdk/LICENSE)
[![Platforms](https://img.shields.io/badge/Linux%20·%20macOS%20·%20Windows-self--hosted-success.svg)](#-quick-start)
[![Status: Alpha](https://img.shields.io/badge/status-Phase%201%20alpha-orange.svg)](#-project-status)
[![Successor to EngelGuard](https://img.shields.io/badge/successor%20to-EngelGuard-9146FF.svg)](https://github.com/Luca-Pelzer/engelguard)
[![GitHub stars](https://img.shields.io/github/stars/Luca-Pelzer/engelos?style=social)](https://github.com/Luca-Pelzer/engelos/stargazers)

**One fast, self-hosted binary for Twitch and Discord.**
Moderation, custom commands, Channel-Points triggers and native Twitch Predictions, a points economy
with mini-games, provably-fair giveaways, multi-provider song requests (Spotify and YouTube), Stream
Wrapped recap cards, and OBS overlays. Self-host it free forever, your data on your machine.

[Why EngelOS?](#-why-engelos) • [Features](#-features) • [Quick Start](#-quick-start) • [Configuration](#-configuration) • [Architecture](#-architecture) • [Status](#-project-status)

⭐ **If this looks useful, [star the repo](https://github.com/Luca-Pelzer/engelos)** to follow along and help it grow.

</div>

---

EngelOS is a **platform**, not a single bot. It grew out of
**[EngelGuard](https://github.com/Luca-Pelzer/engelguard)**, an earlier Python Twitch bot. Building
EngelGuard made one thing clear: what I actually wanted was far bigger than any single bot, so I
rebuilt everything from scratch in Go as one coherent suite. EngelGuard didn't disappear; it's the
moderation bot **inside** EngelOS. The Twitch and Discord bots are still EngelGuard, and EngelOS is
the bigger home they live in, tying moderation, commands, a points economy, games, giveaways, and
engagement systems together in a single static binary with an embedded web dashboard.

The goal is for everything to live under one roof: the chat bots, the Discord integration, the web
dashboard, and over time a plugin/addon ecosystem and a downloadable companion client. The companion
client is the ambitious part of the vision: a small app a streamer installs so the bot can trigger
real actions on their machine in response to events (a channel-point redemption firing an on-screen
effect, for example). Most of that is still on the roadmap below, but it's why EngelOS is built as a
platform from day one rather than a one-off bot.

> [!IMPORTANT]
> **Phase 1 alpha.** The core is live and tested under Go's race detector, but the public OSS launch
> is targeted for **December 2026**, so APIs and the database schema may still change without notice.
> Not accepting external contributions yet (the codebase moves fast).

---

## 🎯 Why EngelOS?

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
- 🎟️ **Channel Points and a points economy, both optional, both toggleable.** Affiliates can bind
  real Twitch Channel-Point redemptions to bot actions. Everyone (affiliate or not) can also switch on
  a built-in points economy with mini-games. They're independent: run either, both, or neither.
- 🧮 **A `$(...)` variable system** for custom commands, including a real `$(math …)` evaluator.

### Quick comparison

| | EngelOS | Nightbot | StreamElements | EngelGuard (Python) |
|---|:---:|:---:|:---:|:---:|
| Cost | **Free self-hosted** (optional paid Cloud later) | Free + Premium | Free + Premium | Free |
| Self-hosted | ✅ | ❌ cloud only | ❌ cloud only | ✅ |
| Open source | ✅ AGPL-3.0 | ❌ | ❌ | ✅ MIT |
| Custom commands + `$(...)` vars | ✅ incl. `$(math)` | ✅ | ✅ | ✅ |
| AutoMod audit log + dry-run | ✅ | ❌ | ❌ | partial |
| Channel-Points triggers + points fallback | ✅ | ❌ | partial | partial |
| Native Twitch Predictions | ✅ | ❌ | ❌ | ❌ |
| Mini-games (gamble/duel/heist) | ✅ | ❌ | ✅ | ✅ |
| Provably-fair giveaways | ✅ | ❌ | ❌ | ❌ |
| Song requests (Spotify + YouTube) | ✅ both | ✅ YT | ✅ both | ❌ |
| Stream Wrapped recap cards | ✅ | ❌ | ❌ | ❌ |
| OBS overlays built in | ✅ | ❌ | ✅ | ❌ |
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
- **Stream info**: `!uptime` `!game` `!title` `!accountage` `!followage` `!so` (shoutout with last category).
- **Fun**: `!8ball` `!lurk` `!unlurk` `!dice` `!roll` `!love` `!ship` `!hug` `!slap`.

### 🎟️ Channel Points and the points economy
Two independent systems you can switch on or off, in any combination:

**Channel-Points trigger engine** (for affiliates and partners): bind a real Twitch Channel-Point
reward to a bot action. If your channel has Channel Points and you want to use them, turn it on. This
also powers **native Twitch Predictions**: `!prediction <title> | <option> | <option>` opens a real
Channel-Points prediction, `!lockprediction` stops betting, and `!endprediction <winner>` settles it,
Twitch handles the proportional points payout itself.

**Built-in points economy** (works on any channel, affiliate or not): a self-managed currency that
powers engagement and mini-games. Channels without Channel Points use it as their main system, but an
affiliate who still wants gambling can run it alongside Channel Points, or skip it entirely.

- **Economy**: earn points by chatting with a per-viewer cooldown (**anti-farming**, so idle and bot
  accounts can't grind), `!points`, `!give`, `!pointslb`. The store is atomic and **can never overdraw**.
- **Games**: `!gamble` (double-or-nothing with a documented house edge), `!slots` (weighted reels),
  `!duel` (PvP wager, both players must afford the stake before any points move), and `!heist`
  (async multiplayer group game). No player can ever go negative.
- **Rewards**: `!reward`/`!rewards`/`!redeem`, a points-backed reward store, a Channel-Points-style
  redemption system for channels that don't have Channel Points.

### 🎁 Giveaways
- `!giveaway`, `!enter`, `!draw`, `!reroll` with a **provably-fair draw**:
  `winner = SHA256(seed │ drawNumber │ sorted-entrant-ids) mod N`, where the seed is announced at open
  time, so the draw is publicly verifiable and un-riggable.

### 🎵 Song requests (multi-provider)
Per-channel choice of music backend, so you pick how requests are played:

- **Spotify** (playlist-as-queue): `!sr <song or link>` adds the track to a dedicated playlist,
  `!song` shows what's playing, `!skipsong` skips. Connect your account from the dashboard
  (`/api/v1/auth/spotify/login`); needs Spotify Premium on the bot account.
- **YouTube** (bot-managed queue): the bot keeps the queue itself and the `/overlay/song-player`
  browser source auto-plays it via the YouTube IFrame player.
- A **now-playing overlay** (`/overlay/now-playing`) renders the current track for either provider.

### 📊 Stream Wrapped
Spotify-Wrapped-style recap cards, served as shareable pages and a `/overlay/wrapped` browser source:

- **Per-viewer card**: messages sent, chatter rank and top-percentile, points, longest streak.
- **Per-channel card**: total messages, subs, raids, distinct chatters, and the top chatters.
- Stats accumulate live (all-time plus per-month buckets) from chat, sub and raid activity.

### 🎮 Engagement systems
- **Pity-System** (gacha): soft-pity ramps win chance, hard-pity guarantees, fully event-sourced and replayable.
- **Streak-System** (Duolingo-style daily streaks).
- **Live-Ops schedule**: a lightweight event calendar the bot is aware of, `!addevent <when> <name>`,
  `!nextevent`, and `!schedule` with live countdowns, so viewers always know what's coming up.
- **Channel-Points trigger engine + Predictions**: bind a Twitch reward to a bot action, or run native
  Channel-Points predictions.
- **Moment Alerts** (experimental): a mod opens a short `!here` window on a big play and it earns a
  common/rare/legendary tier by turnout. Small engagement extra, off unless a mod starts one.

### 🖥️ Platform and Dashboard
- **Embedded SvelteKit web dashboard** (via `go:embed`) with live pages: Home (real daemon stats),
  Channel Points, Commands, Counters, AutoMod (filter config plus audit-log viewer), and Login.
- **OBS browser-source overlays** served straight from the daemon: `/overlay/events`,
  `/overlay/alerts`, `/overlay/leaderboard`, `/overlay/now-playing`, `/overlay/song-player`,
  `/overlay/wrapped`, and `/overlay/moment`. Drop the URL into an OBS Browser Source and go.
- **Event-sourcing engine** (SQLite WAL, append-only, ULID, multi-tenant).
- **Auth**: Argon2id, RBAC, sessions (HttpOnly/Secure/SameSite cookies), and API keys.
- **HTTP API** (chi router, security headers), **Server-Sent Events** stream, and a **WebSocket** hub.
- **Adapters**: Twitch (IRC plus Helix plus EventSub WebSocket, anonymous or authenticated) and Discord
  (needs a bot token). Around 40 internal Go packages, all tested under the race detector.

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
| `ENGELOS_SPOTIFY_CLIENT_ID` / `_SECRET` / `_REDIRECT_URL` | Enable Spotify song requests (needs `ENGELOS_SECRETS_KEY`) | unset |
| `ENGELOS_YOUTUBE_API_KEY` | Enable YouTube song requests (YouTube Data API v3 key) | unset |

> [!NOTE]
> Anonymous mode is read-only (chat-reading and counters). Moderation actions, the dashboard login,
> and OAuth-gated features need an authenticated bot account and `ENGELOS_SECRETS_KEY`.

---

## 🏗️ Architecture

```
┌──────────────────────────────────────────────────────────┐
│  EngelOS Core Daemon (Go, single static binary)          │
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
│  │  Auth        │  │ Pity·Streak·Live-Ops·Channel- │      │
│  │  (Argon2id,  │  │ Points·Predictions·SongReqs·  │      │
│  │  RBAC, keys) │  │ Wrapped·Moments·Counters      │      │
│  └──────────────┘  └──────────────────────────────┘      │
│            └────────┬────────────┘                       │
│                     ↓                                    │
│  ┌────────────────────────────────────────────────────┐  │
│  │ Event-Sourcing Engine (SQLite WAL, append-only)    │  │
│  └────────────────────────────────────────────────────┘  │
│                                                          │
│  HTTP / WebSocket / SSE API on 127.0.0.1:8080            │
└──────────────────────────────────────────────────────────┘
        ▲                  ▲                    ▲
   ┌────┴────┐    ┌────────┴───────┐    ┌─────────┴─────────┐
   │ TUI 🚧  │    │  Web Dashboard │    │ Companion app 🚧  │
   │ (BTea)  │    │  (Svelte 5) ✅ │    │ OBS overlays +    │
   │         │    │                │    │ on-machine actions│
   └─────────┘    └────────────────┘    └───────────────────┘
```

---

## ☁️ Self-host vs Cloud

EngelOS follows an open-core model, the same spirit as [Netdata](https://www.netdata.cloud/): a free
community edition you self-host, and a paid cloud edition for people who want it managed.

| | **Community** (self-hosted) | **Cloud** (planned, Phase 2+) |
|---|---|---|
| Price | **Free forever** | Flat monthly price |
| Hosting | Your machine, your data | Managed for you |
| Setup | Build and run the binary | One-click, no server to run |
| Features | The full bot, dashboard, moderation, commands, economy and games | Everything in Community, plus cloud-only conveniences and AI-backed features |
| Best for | Anyone who wants full control | People who are serious about it and would rather not run their own server |

The Community edition is the real product, not a teaser: nothing in this repository is paywalled, and
nothing here will ever be moved behind the Cloud tier. The Cloud edition exists to fund the
open-source work, not to cripple the free one.

---

## 📦 Project status

**✅ Built and live (Phase 1 alpha):**

- Twitch (IRC, Helix, EventSub) and Discord adapters
- AutoMod (7 filters, escalation, audit log, dry-run) plus dashboard config
- Custom commands and `$(...)` variable system, quotes, counters, timers
- Loyalty economy and mini-games (`!gamble` `!slots` `!duel` `!heist`)
- Provably-fair giveaways
- Pity, Streak, Live-Ops schedule, Channel-Points trigger engine
- Native Twitch Predictions (`!prediction` / `!lockprediction` / `!endprediction`)
- Multi-provider song requests: Spotify (Connect-OAuth, playlist queue) and YouTube (bot queue +
  player overlay), with a now-playing overlay and per-channel config API
- Stream Wrapped recap cards (per-viewer and per-channel) with a shareable overlay
- Moment Alerts (experimental engagement extra)
- Fun and info commands (`!8ball` `!so` `!accountage` `!followage`, and more)
- Event-sourcing, Argon2id auth, REST/SSE/WebSocket API, embedded SvelteKit dashboard
- Seven OBS browser-source overlays

**🚧 Planned (roadmap):**

- Full YouTube and Kick chat adapters (YouTube song requests already work)
- AI features: Auto-Clipper, real-time Translator, context-aware AI-Mod, AI Co-Host, AI-Voice/TTS
- Plugin/addon ecosystem and marketplace
- Downloadable companion client: a desktop app that manages the OBS browser-source overlays (set
  them up and tweak them without editing config) and lets the bot trigger on-machine actions from
  events, for example a channel-point redemption firing an on-screen effect
- TUI (Bubble Tea), native GUI (Wails v2)
- Managed Cloud tier: a paid, hosted option for non-self-hosters (flat monthly price, easier setup,
  some cloud-only AI features), Netdata-style. The self-hosted build stays free forever. (Phase 2+, not in this repo.)

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
  commands/              builtins, custom commands, $(...) vars, games, giveaways, predictions,
                         song requests, moments
  counters/ quotes/ timers/ customcommands/ redemptions/ loyalty/ rewards/ liveops/
  songrequests/          provider config + spotify/ + youtube/ + queue/ (bot-managed queue)
  wrapped/ moments/      Stream Wrapped stats + Moment Alerts
  eventsourcing/         SQLite append-only event log
  features/pity/ streak/ Engagement systems
  runtime/               Dispatcher: adapter events into features + broadcast
  server/  web/          HTTP lifecycle + go:embed of the SvelteKit build
pkg/sdk/                 Public SDK (Apache-2.0) for third-party plugins
web/                     Svelte 5 frontend (local + cloud variants)
docs/                    MASTER-VISION.md and long-form docs
```

---

## ⭐ Star the project

EngelOS is built in the open by one person, and a star is the simplest way to help. It isn't vanity:

- It tells me which features people actually want, so I build the right things next.
- It puts EngelOS in front of more streamers who are tired of paying for the closed bots.
- It's the encouragement that keeps a long, ambitious project (5+ years of roadmap) moving.

If you self-host it, plan to, or just like where it's heading,
**[drop a ⭐ on the repo](https://github.com/Luca-Pelzer/engelos)**. Watch the repo to follow releases,
and open an issue with ideas or bugs. That's the whole ask, and it genuinely matters at this stage.

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

**EngelOS**, built in the open, the successor to [EngelGuard](https://github.com/Luca-Pelzer/engelguard).

If you made it this far, a ⭐ would mean a lot. Thanks for reading.

</div>

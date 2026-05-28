# engelOS

> **The streaming bot that remembers you. Open source. Run it anywhere.**

engelOS is an open-source streaming bot for Twitch, Discord, YouTube Live, and Kick.com.
Self-host it on Linux, macOS, or Windows — or use the managed Cloud version at
[engelos.com](https://engelos.com).

## Status

⚠️ **Phase 0 — Pre-Alpha.** Skeleton exists. No working features yet. Public OSS launch
targeted for **December 2026**. Follow [@engelos](https://x.com/engelos) for updates.

## Vision

| | |
|---|---|
| **License** | AGPL-3.0 (Core) · Apache-2.0 (SDK) · Proprietary (Cloud) |
| **Stack** | Go 1.24+ · Wails v2 · Bubble Tea · Svelte 5 |
| **Platforms** | Linux · macOS · Windows · Docker · Raspberry Pi |
| **Roadmap** | See [`docs/MASTER-VISION.md`](docs/MASTER-VISION.md) |

## Quickstart (when Phase 1 ships)

```bash
# Linux/macOS
curl -L https://engelos.org/install.sh | bash

# Docker
docker run -d -p 8080:8080 -v engelos-data:/data engelos/engelos:latest

# Homebrew (macOS, planned)
brew install engelos

# Windows (planned)
winget install engelos
```

Then open `http://localhost:8080` and follow the setup wizard.

## Architecture

```
┌──────────────────────────────────────────────────┐
│  engelOS Core Daemon (Go)                        │
│  - Twitch/Discord/YouTube/Kick adapters          │
│  - Event-sourcing engine                         │
│  - Multi-user auth + RBAC + API keys             │
│  - HTTP/WebSocket API on :8080                   │
└──────────────────────────────────────────────────┘
        ▲              ▲              ▲
   ┌────┴───┐    ┌─────┴────┐    ┌────┴────┐
   │ TUI    │    │ Web UI   │    │ Native  │
   │ (BTea) │    │ (Svelte) │    │ (Wails) │
   └────────┘    └──────────┘    └─────────┘
```

## Repository Layout

```
cmd/engelos/            # Main daemon entry point
internal/
  adapters/             # Platform adapters (Twitch, Discord, ...)
  auth/                 # Auth + RBAC + API keys
  eventsourcing/        # Append-only event log
  api/                  # REST + WebSocket API
  server/               # HTTP server, static file embed
pkg/sdk/                # Public SDK (Apache-2.0) for plugins
web/                    # Svelte frontend (local + cloud variants)
tui/                    # Bubble Tea TUI
docs/                   # MASTER-VISION.md, ARCHITECTURE.md, ...
scripts/                # Dev + release scripts
.github/workflows/      # CI/CD
```

## Contributing

Not accepting external contributions yet (Phase 0). Once Phase 1 ships and the public
OSS launch happens (December 2026), see `CONTRIBUTING.md`.

## License

- **Core daemon** (this repository, default): **AGPL-3.0** — see [`LICENSE`](LICENSE)
- **SDK** (`pkg/sdk/`): **Apache-2.0** — see [`pkg/sdk/LICENSE`](pkg/sdk/LICENSE)
- **Cloud features**: proprietary, hosted at [engelos.com](https://engelos.com), not in
  this repository.

This dual-licensing follows the [Grafana model](https://grafana.com/licensing/): the
core is protected against cloud reselling (AGPL), while the SDK is open for any company
or contributor to build integrations against (Apache).

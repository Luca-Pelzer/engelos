# Changelog

All notable changes to engelOS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Until the first stable release (1.0.0), expect breaking changes between minor
versions. The CHANGELOG entries during the alpha/beta phase will focus on
high-level milestones rather than every commit.

## [Unreleased]

### Added
- Initial repository skeleton with Go 1.24 daemon and `go build` producing a
  5.8 MB static binary that serves `/healthz` on `127.0.0.1:8080`
- Master Vision Plan (`docs/MASTER-VISION.md`) covering 5-7 year roadmap, stack
  decisions (Go + Wails + Bubble Tea + Svelte), dual-license strategy
  (AGPL-3.0 core + Apache-2.0 SDK), and 8 architecture principles
- Event-sourcing engine (`internal/eventsourcing`) with SQLite append-only
  store, ULID-based event IDs, multi-tenant isolation, `iter.Seq2` reads,
  embedded migrations, WAL mode, STRICT tables
- Platform adapter interface (`internal/adapters`) with `Platform` interface,
  platform-agnostic `Event` and `Action` types, and in-memory `Mock`
  implementation for tests
- Auth system (`internal/auth`) with users, sessions, RBAC (Owner/Admin/Mod/
  Viewer roles), API keys with scopes, Argon2id password hashing
- HTTP API server skeleton (`internal/server` + `internal/api`) with chi
  router, CORS/security/logging middleware, WebSocket hub via coder/websocket,
  SSE event stream
- Svelte 5 + SvelteKit 2 + Tailwind 4 web UI skeleton (`web/`) with login,
  setup wizard, dashboard, chat viewer, commands, integrations, settings, and
  upgrade-to-Cloud pages
- OSS hygiene: `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`,
  `.gitignore`, GoReleaser config for Linux/macOS/Windows, GitHub Actions CI
  + release workflows

### Documentation
- `README.md` with project status, architecture diagram, and dual-license
  rationale
- License placeholders (`LICENSE` for AGPL-3.0 core, `pkg/sdk/LICENSE` for
  Apache-2.0 SDK) — full text to be added at OSS public launch

### Infrastructure
- `EngelGuard` (legacy Python bot, predecessor to engelOS) restored to
  service after 11 days of downtime — systemd hardenings incompatible with
  unprivileged LXC were removed; bot resumed on `#engelswtf`

---

## Phase milestones (forward-looking)

These are not releases yet — they're the roadmap from `docs/MASTER-VISION.md`:

### Phase 1 (June 2026 – December 2026)
Core daemon with 6+ killer features, OSS public launch.

### Phase 2 (January 2027 – June 2027)
Cloud version live, Native GUI apps (Wails) on Win/Mac, 100-1.000 streamers.

### Phase 3 (July 2027 – June 2028)
Monetization flip, 5.000 streamers, profitable.

### Phase 4 (July 2028 – June 2030)
Network effects, 50.000 streamers, "industry standard" perception.

### Phase 5 (2030+)
Strategic inflection point — lifestyle business, VC raise, or strategic
acquisition.

[Unreleased]: https://github.com/engelswtf/engelos/commits/main

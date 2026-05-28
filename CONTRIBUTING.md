# Contributing to engelOS

Thanks for your interest in engelOS. This document explains how to contribute.

> **Status note (2026-05):** engelOS is in Phase 0 / early Phase 1. We are not
> yet accepting community PRs because the architecture is still solidifying.
> Public OSS launch is targeted for **December 2026**. Star the repo and
> follow [@engelos](https://x.com/engelos) for the launch announcement.

## Before You Contribute

Read these first:

1. [`docs/MASTER-VISION.md`](docs/MASTER-VISION.md) — the 5-7 year plan and
   architecture principles. Most contributions that conflict with the plan
   will be politely declined; we'd rather miss good ideas than dilute the
   focus.
2. [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) — Contributor Covenant 2.1.
3. This file — the rest.

## License

By contributing, you agree that your contributions will be licensed under the
license of the package you're contributing to:

| Path | License |
|---|---|
| Default (core daemon) | **AGPL-3.0** |
| `pkg/sdk/**` | **Apache-2.0** |
| `web/**` | **AGPL-3.0** (frontend embedded in AGPL daemon) |
| `docs/**` | **CC-BY-4.0** (proposed; check individual files) |

We do **not** require a CLA (Contributor License Agreement). Your contribution
inherits the license of the path you touch.

If you cannot contribute under AGPL-3.0 (for example, your employer forbids
AGPL contributions), please limit your contributions to `pkg/sdk/**` which is
Apache-2.0.

## Development Setup

### Prerequisites

- **Go 1.24+** ([install](https://go.dev/dl/))
- **Node.js 22+** and **pnpm 9+** (for the web UI)
- **Docker** (optional, for running PostgreSQL / Redis in dev)
- A POSIX shell (Linux, macOS, or WSL on Windows)

### Quick start

```bash
git clone https://github.com/engelswtf/engelos
cd engelos

go mod download
go build ./...
go test -race ./...

cd web && pnpm install && pnpm --filter local dev
```

The daemon listens on `127.0.0.1:8080`. The web dev server runs on a separate
port (Vite picks one automatically, usually `5173`).

## How We Work

### Branch model

- `main` is always shippable. Every commit on `main` must pass CI.
- Feature branches: `feat/<short-name>`, `fix/<short-name>`, `docs/<...>`.
- Open PRs against `main`. We squash-merge.

### Commit style

We follow a relaxed [Conventional Commits](https://www.conventionalcommits.org/)
style:

```
feat(eventsourcing): add snapshot support for read-models
fix(auth): prevent timing attack in session token compare
docs(vision): clarify Phase 4 cross-streamer caveats
chore(deps): bump go-twitch-irc to v3.2.0
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`,
`security`, `breaking`.

### Code style

#### Go

- Run `gofmt -s -w` and `goimports -w` on every file.
- We use `golangci-lint`; see [`.golangci.yml`](.golangci.yml) (added Phase 1A).
- Prefer `log/slog` over `log` or third-party loggers.
- Prefer `context.Context` over channels for cancellation.
- Wrap errors with package context: `fmt.Errorf("eventsourcing: %w", err)`.
- No `interface{}` / `any` unless you have a real reason. Prefer generics.
- No global state. Pass dependencies through constructors.

#### TypeScript / Svelte

- Strict mode on. No `any` without comment.
- Svelte 5 runes (`$state`, `$derived`, `$effect`). No Svelte 4 reactivity.
- Tailwind 4 with design tokens via `@theme`. No raw color hex codes outside
  the token file.

### Tests

- Every new package has tests. Coverage target: meaningful, not numeric.
- Run `go test -race ./...` locally before pushing.
- We don't gatekeep on coverage percent, but a PR that adds a new feature
  without any tests will be sent back.

### Performance

We care about resource usage because self-hosters run engelOS on Raspberry
Pis. Baseline targets for the daemon at idle (no active channels):

- RSS memory: **< 80 MB**
- CPU usage: **< 1%**
- Binary size: **< 30 MB stripped**

If your PR materially regresses any of these, mention it in the description
and propose a tradeoff.

## What Makes a Good PR

- One thing per PR. "Refactor X **and** add feature Y" should be two PRs.
- Description explains **why**, not just **what** (the diff already shows what).
- Tests where they belong. New behavior → new tests.
- No drive-by formatting in unrelated files.
- Link the issue you're solving (or open one first for non-trivial work).

## What Makes a Bad PR

- Adding a dependency to save five lines of code.
- Reformatting existing code that wasn't broken.
- Implementing a Tier-C / Tier-D feature from the vision plan before Tier A is
  complete. Order matters.
- Anything that requires us to maintain code for a use case nobody has asked
  for.

## Security

If you find a security issue, please follow [`SECURITY.md`](SECURITY.md).
**Do not open a public issue for security problems.**

## Asking Questions

Use [GitHub Discussions](https://github.com/engelswtf/engelos/discussions)
once we open them (Phase 1D, with OSS launch). Until then, issues are open
for bug reports only.

## License of Your Contribution

By submitting a pull request, you certify that:

1. The contribution is your own work, or you have the right to submit it.
2. You agree that the contribution will be distributed under the licenses
   noted in the "License" section above.

This is the same intent as the [Developer Certificate of Origin](https://developercertificate.org/)
but we do not require a signed-off-by line.

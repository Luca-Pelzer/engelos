# engelos-tui

Terminal dashboard for the running engelOS daemon. Connects over HTTP/WebSocket,
shares the daemon's session cookie, and renders live stats, leaderboards, and
chat events with [bubbletea](https://github.com/charmbracelet/bubbletea).

## Build

```bash
CGO_ENABLED=0 go build -o engelos-tui ./cmd/engelos-tui
```

## Run

```bash
./engelos-tui                              # default http://127.0.0.1:8080
./engelos-tui -addr https://daemon.local   # custom daemon
./engelos-tui -email me@x.test             # email prefilled, will prompt for password
./engelos-tui -insecure                    # skip TLS verification (self-signed)
```

Flags:

| Flag | Default | Description |
|---|---|---|
| `-addr` | `http://127.0.0.1:8080` | Daemon base URL |
| `-email` | _(empty)_ | Login email (prompted if missing) |
| `-password` | _(empty)_ | Login password (prompted, hidden) |
| `-insecure` | `false` | Skip TLS certificate verification |

The TUI prompts inside the bubbletea event loop for any missing credentials —
nothing is read from raw stdin, so terminal modes stay sane.

## Keys

| Key | Action |
|---|---|
| `?` | Toggle help overlay |
| `q` / `ctrl+c` | Logout + quit |
| `r` | Refresh current view |
| `d` | Dashboard |
| `l` | Leaderboards (pity + streak) |
| `c` | Chat (live WebSocket) |
| `b` / `esc` | Back / close overlay |
| `tab` | Cycle focus (leaderboards) |
| `↑/↓ pgup/pgdn` | Scroll (chat) |

## Environment

`ENGELOS_TWITCH_CHANNELS` — comma-separated list; the first entry seeds the
leaderboard channel input. Falls back to `engelswtf` when unset.

## Architecture

```
cmd/engelos-tui/
  main.go         flag parsing + tea.NewProgram
  model.go        Bubble Tea state machine + per-view models
  api.go          HTTP/WS client (cookiejar-backed)
  styles.go       lipgloss palette (matches Svelte UI)
  keys.go         keybind constants + help text
  *_test.go       table-driven unit tests
```

The TUI talks exclusively to the existing daemon HTTP API; it adds no new
server-side surface. The pity leaderboard endpoint does not exist yet, so
`Client.PityLeaderboard` returns `(nil, nil)` as a stub until the daemon
exposes `/api/v1/pity/leaderboard`.

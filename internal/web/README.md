# internal/web - embedded SvelteKit dashboard

This package wraps the prerendered SvelteKit static build (from
`web/packages/local/build/`) in a `//go:embed` directive and exposes it as a
standard `http.Handler`.

## How it works

```
web/packages/local/build/    <-- `pnpm build` output (gitignored)
        |
        | make web-build  (copy)
        v
internal/web/build/          <-- mirror, also gitignored
        |
        | //go:embed all:build
        v
embedded in the daemon binary
```

The `build/` directory always contains a sentinel `.gitkeep`, so:

- A fresh checkout still compiles (`go build ./...`) without a Node toolchain.
- CI / release builds run `make web-build` first to populate the tree, then
  `go build` so the final binary ships the dashboard.

`web.Available()` lets callers detect at runtime which mode the binary was
built in. `web.Handler(fallback)` returns a handler that serves the embedded
files with sane defaults (Content-Type from extension, `no-store` on HTML,
`max-age=31536000, immutable` on hashed `_app/` assets) and delegates
anything not found in the embed to `fallback`.

## Usage

```go
fallback := http.HandlerFunc(handlers.Index)
if h := web.Handler(fallback); h != nil {
    r.Handle("/*", h)        // dashboard + SPA-style fallthrough
} else {
    r.Get("/", fallback)     // dev binary, no UI embedded
}
```

## Refreshing the embedded assets

```bash
make web-build       # rebuilds web/ and copies into internal/web/build/
go build ./...       # bakes new assets into the binary
```

To return to the dev path:

```bash
git clean -xdf internal/web/build/
touch internal/web/build/.gitkeep
```

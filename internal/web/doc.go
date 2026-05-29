// Package web embeds the prerendered SvelteKit dashboard into the engelOS
// daemon binary and exposes it as a standard http.Handler.
//
// The Svelte build output lives in web/packages/local/build/ and is copied
// into internal/web/build/ by the `make web-build` target before `go build`
// runs. The Go //go:embed directive in embed.go then bakes the entire tree
// into the daemon binary, so a single distributable executable can serve
// the dashboard with zero external assets at runtime.
//
// # Dev path
//
// When developers iterate without running `make web-build`, the only file
// inside internal/web/build/ is a sentinel .gitkeep. In that case
// Available() returns false and callers should keep using their existing
// JSON / plain-HTML index handler. This keeps `go build ./...` green on a
// fresh checkout without requiring a Node toolchain.
//
// # Production path
//
// After `make web-build` populates the tree, Handler returns a handler
// that serves files out of the embedded FS with correct Content-Type
// detection and HTTP cache headers tuned for SvelteKit's hashed
// _app/immutable assets. Unknown paths fall through to the caller-supplied
// fallback handler (typically a JSON 404 or the SPA shell).
package web

// Package api wires the engelOS HTTP/WebSocket API surface.
//
// It owns the chi router, middleware stack, and request handlers. The
// transport layer (listener, timeouts, graceful shutdown) lives in
// internal/server. Other domains (auth, eventsourcing) interact with this
// package only via the narrow Deps interface declared in router.go.
package api

// Package server provides the HTTP server lifecycle for the engelOS daemon.
//
// It wraps net/http with sensible defaults (timeouts, graceful shutdown,
// loopback-only binding by default) and integrates with log/slog for
// structured request logging. The Server is transport-only — routing and
// handlers live in internal/api.
package server

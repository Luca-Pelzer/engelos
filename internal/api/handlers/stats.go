package handlers

import (
	"log/slog"
	"net/http"
)

// StatsProvider is the narrow contract handlers.Stats needs to expose the
// dispatcher counters as a JSON-serialisable value. The concrete return type
// is the caller's choice; the handler just marshals whatever comes back.
// Callers typically pass a func wrapper around a runtime.Dispatcher.
type StatsProvider interface {
	Snapshot() any
}

// Stats serves GET /api/v1/stats. It returns the daemon's version + phase
// plus the latest dispatcher counters so the TUI / web dashboard can render
// a live overview without subscribing to the WebSocket stream.
type Stats struct {
	version  Version
	provider StatsProvider
	logger   *slog.Logger
}

// NewStats constructs a Stats handler. When provider is nil the dispatcher
// section is omitted (handler still returns 200 with version info).
func NewStats(version Version, provider StatsProvider, logger *slog.Logger) *Stats {
	if logger == nil {
		logger = slog.Default()
	}
	return &Stats{version: version, provider: provider, logger: logger}
}

// Get handles GET /api/v1/stats.
func (s *Stats) Get(w http.ResponseWriter, _ *http.Request) {
	payload := map[string]any{
		"version": s.version.Version,
		"phase":   s.version.Phase,
	}
	if s.provider != nil {
		payload["dispatcher"] = s.provider.Snapshot()
	}
	writeJSON(w, http.StatusOK, payload)
}

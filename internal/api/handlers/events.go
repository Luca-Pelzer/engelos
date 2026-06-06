package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Events serves the Server-Sent Events stream. Phase 0 emits only heartbeats.
type Events struct {
	Logger    *slog.Logger
	Heartbeat time.Duration
}

// NewEvents constructs an SSE handler with the given heartbeat cadence.
// A zero or negative Heartbeat defaults to 5s.
func NewEvents(logger *slog.Logger, heartbeat time.Duration) *Events {
	if logger == nil {
		logger = slog.Default()
	}
	if heartbeat <= 0 {
		heartbeat = 5 * time.Second
	}
	return &Events{Logger: logger, Heartbeat: heartbeat}
}

// Stream handles GET /api/v1/events.
func (e *Events) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	_, _ = fmt.Fprintf(w, ": engelos sse stream\n\n")
	flusher.Flush()

	ticker := time.NewTicker(e.Heartbeat)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			if _, err := fmt.Fprintf(w, "event: heartbeat\ndata: %d\n\n", t.UnixMilli()); err != nil {
				e.Logger.Debug("sse write failed", "err", err)
				return
			}
			flusher.Flush()
		}
	}
}

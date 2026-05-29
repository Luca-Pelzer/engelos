package overlay

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed assets/*
var assetsFS embed.FS

var pages = map[string]string{
	"":            "assets/index.html",
	"events":      "assets/events.html",
	"alerts":      "assets/alerts.html",
	"leaderboard": "assets/leaderboard.html",
}

// Handler serves OBS browser-source overlay pages from an embedded asset
// set at /overlay/{name}. Each overlay is a self-contained HTML page that
// connects to the bot's WebSocket and renders live events over a
// transparent background.
//
// The handler expects to be mounted at /overlay/ and receive the full
// request path; it derives the overlay name by trimming that prefix.
// Empty / "/" serves a small index page listing available overlays.
// Unknown names produce a 404 HTML body (not JSON), since the audience
// for these URLs is a streamer pasting them into OBS, not an API client.
//
// Every response is sent as text/html; charset=utf-8 with
// Cache-Control: no-store so OBS picks up edits without a cache flush.
func Handler(logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &overlayHandler{logger: logger}
}

type overlayHandler struct {
	logger *slog.Logger
}

func (h *overlayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.notFound(w, r)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/overlay/")
	name = strings.TrimPrefix(name, "/")
	name = strings.Trim(name, "/")

	asset, ok := pages[name]
	if !ok {
		h.notFound(w, r)
		return
	}

	body, err := fs.ReadFile(assetsFS, asset)
	if err != nil {
		h.logger.Debug("overlay asset read failed", "name", name, "err", err)
		h.notFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

const notFoundHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>overlay not found</title>
<style>body{font:14px/1.5 system-ui,sans-serif;background:#111;color:#eee;padding:2rem;max-width:40rem;margin:auto}a{color:#a970ff}</style>
</head><body>
<h1>Overlay not found</h1>
<p>No overlay is registered at this path. Available overlays:</p>
<ul>
<li><a href="/overlay/events">/overlay/events</a></li>
<li><a href="/overlay/alerts">/overlay/alerts</a></li>
<li><a href="/overlay/leaderboard">/overlay/leaderboard</a></li>
</ul>
</body></html>`

func (h *overlayHandler) notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(notFoundHTML))
}

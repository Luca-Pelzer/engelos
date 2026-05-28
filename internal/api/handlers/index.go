package handlers

import "net/http"

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>engelOS — Phase 0 skeleton</title>
<style>
body{font-family:system-ui,sans-serif;max-width:42rem;margin:4rem auto;padding:0 1rem;line-height:1.6;color:#111}
code{background:#f4f4f5;padding:.1rem .3rem;border-radius:.25rem;font-size:.9em}
.muted{color:#71717a}
</style>
</head>
<body>
<h1>engelOS</h1>
<p class="muted">Phase 0 — skeleton. No features yet.</p>
<p>The daemon is running. Health check: <code>GET /healthz</code></p>
<p>Roadmap: <a href="https://github.com/engelos-bot/engelos">github.com/engelos-bot/engelos</a></p>
</body>
</html>`

// Index serves the placeholder landing page at /.
func Index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

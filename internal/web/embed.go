package web

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:build
var buildFS embed.FS

// FS is the read-only filesystem rooted at the embedded build/ directory.
// Callers that need raw access to the assets (e.g. for tests or alternative
// serving strategies) can range over this. The handler returned by Handler
// should be preferred for HTTP serving.
var FS = buildFS

// Available reports whether a real web UI was embedded into this binary.
// It returns false when the only entry under build/ is the sentinel
// .gitkeep used to keep the embed directive valid in dev checkouts.
func Available() bool {
	entries, err := fs.ReadDir(buildFS, "build")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() == ".gitkeep" {
			continue
		}
		return true
	}
	return false
}

// Handler returns an http.Handler that serves the embedded SvelteKit
// dashboard. Unknown paths are delegated to fallback. If no UI was embedded
// (see Available), Handler returns nil so the caller can wire a plain
// fallback handler instead.
//
// Behaviour:
//   - "/" serves build/index.html.
//   - Extensionless paths like "/chat" first try "/chat.html".
//   - Paths matching files in the embed are served with Content-Type
//     derived from the extension via mime.TypeByExtension.
//   - HTML responses send "Cache-Control: no-store" so users always get the
//     freshest app shell.
//   - Hashed asset paths under "/_app/" send a one-year immutable cache
//     header; SvelteKit fingerprints these filenames so it is safe.
//   - Anything not found in the embed is delegated to fallback.
func Handler(fallback http.Handler) http.Handler {
	if !Available() {
		return nil
	}
	if fallback == nil {
		fallback = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
	}

	sub, err := fs.Sub(buildFS, "build")
	if err != nil {
		return nil
	}

	return &embedHandler{root: sub, fallback: fallback}
}

type embedHandler struct {
	root     fs.FS
	fallback http.Handler
}

func (h *embedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.fallback.ServeHTTP(w, r)
		return
	}

	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" || rel == "." {
		rel = "index.html"
	}

	candidates := []string{rel}
	if path.Ext(rel) == "" {
		candidates = append(candidates, rel+".html", path.Join(rel, "index.html"))
	}

	for _, name := range candidates {
		if h.serveFile(w, r, name) {
			return
		}
	}

	h.fallback.ServeHTTP(w, r)
}

func (h *embedHandler) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	f, err := h.root.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return false
	}

	ext := strings.ToLower(path.Ext(name))
	if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	switch {
	case ext == ".html":
		w.Header().Set("Cache-Control", "no-store")
	case strings.HasPrefix(name, "_app/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}

	if seeker, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, stat.ModTime(), seeker)
		return true
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return false
	}
	http.ServeContent(w, r, name, modTimeOrNow(stat.ModTime()), bytes.NewReader(data))
	return true
}

func modTimeOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

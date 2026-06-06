// Package handlers contains the engelOS HTTP handler implementations.
package handlers

import (
	"encoding/json"
	"net/http"
)

// Version reports the running engelOS build identity. Set at construction
// time from main via ldflags.
type Version struct {
	Version string
	Phase   string
}

// Health responds with liveness, readiness, and version information.
type Health struct {
	V Version
}

// NewHealth constructs a Health handler bundle.
func NewHealth(v Version) *Health {
	if v.Phase == "" {
		v.Phase = "0"
	}
	return &Health{V: v}
}

// Healthz is the liveness probe.
func (h *Health) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz is the readiness probe. In Phase 0 it always reports ready.
func (h *Health) Readyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// VersionHandler reports build identity.
func (h *Health) VersionHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": h.V.Version,
		"phase":   h.V.Phase,
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// writeNoStore marks a response as never-cacheable. It is used on OAuth
// login redirects, whose Location embeds a one-time state and a current
// redirect_uri: a cached 302 from an earlier deploy would replay a stale
// redirect_uri and be rejected by the provider with redirect_mismatch.
func writeNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

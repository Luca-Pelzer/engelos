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

package handlers

import "net/http"

// CapabilityFlags reports which optional, flag-gated product surfaces have
// their backend mounted. It is built from the same nil-checks the router uses
// to gate route registration, so the map it produces can never drift from what
// is actually reachable.
//
// This is the dashboard analogue of [AuthProviderFlags]: instead of telling the
// login page which OAuth buttons to draw, it tells the authenticated dashboard
// which optional feature surfaces to show. A card whose capability is false
// points at routes that would 404, so hiding it removes a dead link.
type CapabilityFlags struct {
	// SongRequests is true when the songrequests subsystem is enabled
	// (ENGELOS_FEATURE_SONGREQUESTS). When false the /api/v1/songrequests and
	// /api/v1/songqueue routes are unmounted and the "Music Plugin" card is
	// hidden. See RES-19 (Option A quarantine).
	SongRequests bool
}

// Capabilities returns a handler for GET /api/v1/capabilities. It serves the
// capability map the dashboard reads to decide which optional feature surfaces
// to render. The response leaks nothing beyond which optional features exist,
// mirroring the public /auth/providers discovery endpoint.
func Capabilities(flags CapabilityFlags) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{
			"songrequests": flags.SongRequests,
		})
	}
}

package handlers

import "net/http"

// AuthProviderFlags reports which OAuth login providers are configured and
// therefore have their routes mounted. The router builds it from the same
// nil-checks that gate route registration so the answer can never drift from
// what is actually reachable.
type AuthProviderFlags struct {
	Twitch  bool
	Discord bool
}

// AuthProviders returns a handler for GET /api/v1/auth/providers. It serves
// the public capability map the login page reads to decide which OAuth
// buttons to render: a button for an unmounted provider would 404 on click
// and surface to the user as a generic error, which is exactly the failure
// this endpoint removes. The response is intentionally unauthenticated and
// leaks nothing beyond which login methods exist.
func AuthProviders(flags AuthProviderFlags) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{
			"twitch":  flags.Twitch,
			"discord": flags.Discord,
		})
	}
}

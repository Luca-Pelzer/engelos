package handlers

import "net/http"

// Auth holds the stub authentication handlers. Real implementation lives in
// internal/auth and is wired by another agent.
type Auth struct{}

// NewAuth constructs the stub Auth handler bundle.
func NewAuth() *Auth { return &Auth{} }

// Login is a 501 stub until internal/auth is wired in.
func (a *Auth) Login(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// Logout is a 501 stub until internal/auth is wired in.
func (a *Auth) Logout(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

// Me is a 501 stub until internal/auth is wired in.
func (a *Auth) Me(w http.ResponseWriter, _ *http.Request) { notImplemented(w) }

func notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "not_implemented",
	})
}

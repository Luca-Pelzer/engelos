package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nicklaw5/helix/v2"
	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/auth"
)

// OAuthStateCookieName is the cookie that carries the CSRF state value
// between the Login redirect and the Callback handler. It is short-lived
// (10 minutes) and SameSite=Lax — Lax (not Strict) is required because
// the user returns to the callback URL from id.twitch.tv via a top-level
// navigation, which Strict would treat as cross-site and drop the cookie.
// Lax still protects against the relevant CSRF surfaces (forged
// sub-requests cannot read/replay the state value).
const OAuthStateCookieName = "engelos_oauth_state"

// oauthStateTTL bounds how long a freshly-issued state value remains
// acceptable in the callback. Twitch's authorize page is interactive,
// so 10 minutes is a comfortable upper bound on user latency without
// giving attackers a long replay window.
const oauthStateTTL = 10 * time.Minute

// oauthStateBytes is the byte length of the random state value before
// base64url encoding (32 bytes ≈ 256 bits of entropy).
const oauthStateBytes = 32

// helixUserGetter is the narrow surface of *helix.Client that the
// Callback handler needs to fetch the authenticated user's profile.
// It is an interface so tests can substitute a fake without making
// real Twitch API calls.
type helixUserGetter interface {
	GetUsers(*helix.UsersParams) (*helix.UsersResponse, error)
}

// OAuth bundles the "Login with Twitch" HTTP handlers (Login, Callback).
// It is wired in two pieces of state: an auth.Store (must have been
// opened WithCrypto so OAuth tokens can be encrypted at rest) and an
// *oauth2.Config that pins the client id/secret/redirect/scopes and the
// Twitch endpoint. When either is nil the handlers degrade to 501
// "not_implemented" so the router can still be built in OAuth-disabled
// deployments.
//
// OAuth is safe for concurrent use; all mutable configuration is fixed
// at construction time.
type OAuth struct {
	store        auth.Store
	tenantID     string
	logger       *slog.Logger
	sessionTTL   time.Duration
	cookieName   string
	cookieSecure bool

	cfg      *oauth2.Config
	clientID string

	// newHelix is a test seam. Production callers leave it nil and the
	// Callback handler builds a real *helix.Client. Tests inject a fake
	// that returns a canned helix.UsersResponse.
	newHelix func(clientID, userAccessToken string) (helixUserGetter, error)

	// exchange is a test seam around o.cfg.Exchange. Production callers
	// leave it nil and the Callback handler calls o.cfg.Exchange directly.
	// Tests inject a fake that bypasses the real Twitch token endpoint.
	exchange func(ctx context.Context, code string) (*oauth2.Token, error)
}

// NewOAuth constructs the OAuth handler bundle.
//
// store may be nil; in that case every handler returns 501 (this lets
// the router be built before OAuth is configured). cfg may also be nil
// with the same effect — together they let the OAuth feature be turned
// off entirely without compile-time changes.
//
// tenantID is the single-tenant identifier this daemon serves. A nil
// logger falls back to slog.Default.
//
// Defaults: sessionTTL = DefaultSessionTTL (30d), cookieName =
// DefaultCookieName ("engelos_session"), cookieSecure = true. Use
// WithCookieSecure / WithSessionTTL to override.
func NewOAuth(store auth.Store, tenantID string, logger *slog.Logger, cfg *oauth2.Config) *OAuth {
	if logger == nil {
		logger = slog.Default()
	}
	clientID := ""
	if cfg != nil {
		clientID = cfg.ClientID
	}
	return &OAuth{
		store:        store,
		tenantID:     strings.TrimSpace(tenantID),
		logger:       logger.With("component", "api.handlers.oauth"),
		sessionTTL:   DefaultSessionTTL,
		cookieName:   DefaultCookieName,
		cookieSecure: true,
		cfg:          cfg,
		clientID:     clientID,
	}
}

// WithSessionTTL overrides the default session lifetime. Non-positive
// values are ignored.
func (o *OAuth) WithSessionTTL(d time.Duration) *OAuth {
	if d > 0 {
		o.sessionTTL = d
	}
	return o
}

// WithCookieSecure controls the Secure attribute on the session cookie.
// Tests typically pass false because httptest serves plain HTTP.
func (o *OAuth) WithCookieSecure(secure bool) *OAuth {
	o.cookieSecure = secure
	return o
}

// disabled reports whether the OAuth feature is turned off — either
// because no Store was supplied or because no oauth2.Config was wired.
// In both cases handlers respond 501.
func (o *OAuth) disabled() bool {
	return o.store == nil || o.cfg == nil
}

// generateState returns a base64url-encoded cryptographically random
// state value suitable for use as the OAuth2 state parameter.
func generateState() (string, error) {
	buf := make([]byte, oauthStateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Login handles GET /api/v1/auth/twitch/login.
//
// It mints a fresh CSRF state value, stores it in the short-lived
// engelos_oauth_state cookie (HttpOnly, SameSite=Lax, MaxAge=600), and
// 302-redirects the user to Twitch's authorize endpoint with the state
// parameter pinned. The browser will return to /api/v1/auth/twitch/callback
// after the user grants (or denies) consent.
func (o *OAuth) Login(w http.ResponseWriter, r *http.Request) {
	if o.disabled() {
		notImplemented(w)
		return
	}
	state, err := generateState()
	if err != nil {
		o.logger.ErrorContext(r.Context(), "oauth: generate state failed",
			slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error",
		})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   int(oauthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		// Lax (not Strict) — see OAuthStateCookieName doc comment.
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, o.cfg.AuthCodeURL(state), http.StatusFound)
}

// clearStateCookie writes a Set-Cookie that immediately expires the
// state cookie. Always emitted after a callback (success or failure)
// so a leaked state value can't be replayed.
func (o *OAuth) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Callback handles GET /api/v1/auth/twitch/callback.
//
// Flow:
//  1. Reject the request when Twitch reported an error (?error=...).
//  2. Validate the state query parameter against the state cookie in
//     constant time; reject on mismatch and clear the cookie either way.
//  3. Exchange the authorization code for an *oauth2.Token.
//  4. Use the access token to fetch the authenticated user's identity
//     from Helix (GetUsers with empty params returns the bearer's user).
//  5. Find an existing OAuthIdentity by (provider, provider_user_id) or
//     create a new dashboard auth.User and link it.
//  6. Store the encrypted access/refresh tokens via CreateOAuthIdentity
//     (which upserts on conflict).
//  7. Mint a session exactly like the password Login handler and set
//     the engelos_session cookie. Redirect 303 to /?login=success.
//
// Error responses are deliberately generic ({"error":"..."}). The
// handler never logs access or refresh tokens.
func (o *OAuth) Callback(w http.ResponseWriter, r *http.Request) {
	if o.disabled() {
		notImplemented(w)
		return
	}
	ctx := r.Context()

	// Generic redirect on provider-side errors avoids leaking upstream
	// diagnostic strings into the URL the user lands on.
	if e := r.URL.Query().Get("error"); e != "" {
		o.logger.WarnContext(ctx, "oauth: provider returned error",
			slog.String("provider", auth.ProviderTwitch),
			slog.String("error", e))
		o.clearStateCookie(w)
		http.Redirect(w, r, "/?login=error", http.StatusSeeOther)
		return
	}

	state := r.URL.Query().Get("state")
	cookie, cookieErr := r.Cookie(OAuthStateCookieName)
	o.clearStateCookie(w)
	if cookieErr != nil || cookie == nil || cookie.Value == "" || state == "" ||
		subtle.ConstantTimeCompare([]byte(state), []byte(cookie.Value)) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid_state",
		})
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing_code",
		})
		return
	}

	exchange := o.exchange
	if exchange == nil {
		exchange = func(ctx context.Context, code string) (*oauth2.Token, error) {
			return o.cfg.Exchange(ctx, code)
		}
	}
	tok, err := exchange(ctx, code)
	if err != nil {
		o.logger.ErrorContext(ctx, "oauth: token exchange failed",
			slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "oauth_exchange_failed",
		})
		return
	}

	hxFactory := o.newHelix
	if hxFactory == nil {
		hxFactory = defaultHelixUserGetterFactory
	}
	hx, err := hxFactory(o.clientID, tok.AccessToken)
	if err != nil {
		o.logger.ErrorContext(ctx, "oauth: helix client build failed",
			slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "identity_fetch_failed",
		})
		return
	}
	usersResp, err := hx.GetUsers(&helix.UsersParams{})
	if err != nil || usersResp == nil || len(usersResp.Data.Users) == 0 {
		o.logger.ErrorContext(ctx, "oauth: identity fetch failed",
			slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "identity_fetch_failed",
		})
		return
	}
	tu := usersResp.Data.Users[0]
	providerUserID := strings.TrimSpace(tu.ID)
	login := strings.TrimSpace(tu.Login)
	if providerUserID == "" || login == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "identity_fetch_failed",
		})
		return
	}

	user, err := o.findOrCreateUser(ctx, providerUserID, login, tu.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error",
		})
		return
	}

	scopes := parseGrantedScopes(tok)
	if _, err := o.store.CreateOAuthIdentity(ctx, auth.OAuthIdentity{
		TenantID:       o.tenantID,
		UserID:         user.ID,
		Provider:       auth.ProviderTwitch,
		ProviderUserID: providerUserID,
		ProviderLogin:  login,
		Purpose:        auth.OAuthPurposeUser,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		Scopes:         scopes,
		ExpiresAt:      tok.Expiry,
	}); err != nil {
		o.logger.ErrorContext(ctx, "oauth: persist identity failed",
			slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error",
		})
		return
	}

	token, sess, err := auth.NewSession(
		user.TenantID, user.ID,
		r.UserAgent(), r.RemoteAddr,
		o.sessionTTL,
	)
	if err != nil {
		o.logger.ErrorContext(ctx, "oauth: session mint failed",
			slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error",
		})
		return
	}
	if err := o.store.CreateSession(ctx, sess); err != nil {
		o.logger.ErrorContext(ctx, "oauth: session persist failed",
			slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal_error",
		})
		return
	}
	user.LastLoginAt = time.Now().UTC()
	if err := o.store.UpdateUser(ctx, user); err != nil {
		o.logger.WarnContext(ctx, "oauth: update last_login_at failed",
			slog.Any("err", err))
	}
	http.SetCookie(w, &http.Cookie{
		Name:     o.cookieName,
		Value:    token,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		MaxAge:   int(o.sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/?login=success", http.StatusSeeOther)
}

// findOrCreateUser returns the dashboard auth.User linked to the given
// Twitch identity, creating both the User and (implicitly, via the
// caller) the OAuthIdentity link on first sight.
//
// When an OAuthIdentity already exists, the linked User is loaded by
// (tenant, id). When it does not exist, a new viewer-role User is
// created with email = providerEmail (or "<login>@twitch.local" when
// the user has not exposed their email to the OAuth app — the schema
// requires a non-empty unique email). PasswordHash is filled with a
// random 32-byte un-loginable blob because these users will never
// authenticate via password.
func (o *OAuth) findOrCreateUser(ctx context.Context, providerUserID, login, providerEmail string) (auth.User, error) {
	identity, err := o.store.GetOAuthIdentityByProviderUserID(ctx, auth.ProviderTwitch, providerUserID)
	switch {
	case err == nil:
		u, getErr := o.store.GetUserByID(ctx, identity.TenantID, identity.UserID)
		if getErr != nil {
			o.logger.ErrorContext(ctx, "oauth: load linked user failed",
				slog.Any("err", getErr))
			return auth.User{}, getErr
		}
		return u, nil
	case errors.Is(err, auth.ErrOAuthIdentityNotFound):
	default:
		o.logger.ErrorContext(ctx, "oauth: identity lookup failed",
			slog.Any("err", err))
		return auth.User{}, err
	}

	email := strings.TrimSpace(providerEmail)
	if email == "" {
		// Twitch users may decline to share their email with the OAuth
		// app. The users table requires a unique non-empty email per
		// tenant, so we synthesise a stable placeholder rooted in the
		// Twitch login. It is never used for delivery and is replaceable
		// by the user later via a settings endpoint.
		email = login + "@twitch.local"
	}
	// Generate an unloginable password hash. PasswordHash is required by
	// User.Validate; we just need 32 random bytes that won't compare
	// equal to anything produced by HashPassword.
	noPass := make([]byte, 32)
	if _, err := rand.Read(noPass); err != nil {
		return auth.User{}, err
	}
	newUser := auth.User{
		ID:           auth.NewUserID(),
		TenantID:     o.tenantID,
		Email:        email,
		Username:     login,
		PasswordHash: noPass,
		Role:         auth.RoleViewer,
	}
	created, err := o.store.CreateUser(ctx, newUser)
	if err == nil {
		return created, nil
	}
	if errors.Is(err, auth.ErrUserAlreadyExists) {
		// An existing dashboard user (created via password or a previous
		// OAuth flow that since lost its identity row) already owns the
		// synthesised email/username. Fall back to the existing record
		// so the OAuth link is attached to that user instead of failing.
		existing, getErr := o.store.GetUserByEmail(ctx, o.tenantID, email)
		if getErr == nil {
			return existing, nil
		}
		o.logger.ErrorContext(ctx, "oauth: user-already-exists fallback failed",
			slog.Any("err", getErr))
		return auth.User{}, getErr
	}
	o.logger.ErrorContext(ctx, "oauth: create user failed",
		slog.Any("err", err))
	return auth.User{}, err
}

// parseGrantedScopes pulls the space-separated "scope" extra field out
// of the token (Twitch returns the granted scopes there). Falls back to
// an empty slice when the token has no scope claim.
func parseGrantedScopes(tok *oauth2.Token) []string {
	if tok == nil {
		return nil
	}
	raw, _ := tok.Extra("scope").(string)
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}

// defaultHelixUserGetterFactory builds the production *helix.Client used
// to fetch the authenticated user's identity. The same client is used in
// the twitch adapter (internal/adapters/twitch), but here we only need
// the GetUsers surface so we expose a narrow helixUserGetter interface.
func defaultHelixUserGetterFactory(clientID, userAccessToken string) (helixUserGetter, error) {
	c, err := helix.NewClient(&helix.Options{
		ClientID:        clientID,
		UserAccessToken: userAccessToken,
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

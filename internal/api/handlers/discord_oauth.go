package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/auth"
)

// errDiscordStatus is returned when GET /users/@me responds with a
// non-200 status, carrying the code for the caller's log line.
func errDiscordStatus(code int) error {
	return fmt.Errorf("discord: users/@me returned status %d", code)
}

// discordUserEndpoint is the Discord REST endpoint that returns the
// access token's owning user (id, username, email). v10 is the current
// stable API version.
const discordUserEndpoint = "https://discord.com/api/v10/users/@me"

// DiscordOAuth implements "Login with Discord" for the dashboard. It
// mirrors the Twitch OAuth handler's owner-gated session model but talks
// to Discord's OAuth2 + /users/@me instead of Twitch + Helix. It reuses
// the shared *OAuth core for the owner allowlist, user upsert, and
// session-cookie settings so the two providers stay consistent.
//
// When the config is nil the handlers degrade to 501 so the router still
// boots in a Discord-disabled deployment.
type DiscordOAuth struct {
	core *OAuth
	cfg  *oauth2.Config

	// fetchUser is a test seam. Production leaves it nil and the Callback
	// fetches the real Discord profile; tests inject a canned user.
	fetchUser func(ctx context.Context, accessToken string) (discordUser, error)

	// exchange is a test seam around cfg.Exchange.
	exchange func(ctx context.Context, code string) (*oauth2.Token, error)
}

// discordUser is the subset of GET /users/@me the handler consumes.
type discordUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// NewDiscordOAuth constructs the handler. It shares the owner allowlist,
// session settings, and store from the provided Twitch OAuth core so both
// login providers grant identical access.
func NewDiscordOAuth(core *OAuth, cfg *oauth2.Config) *DiscordOAuth {
	return &DiscordOAuth{core: core, cfg: cfg}
}

// disabled reports whether the handler is missing the wiring it needs.
func (d *DiscordOAuth) disabled() bool {
	return d == nil || d.core == nil || d.core.store == nil || d.cfg == nil
}

// Login handles GET /api/v1/auth/discord/login. It mints a CSRF state,
// stores it in the short-lived state cookie, and redirects to Discord's
// authorize endpoint. Only purpose=user is meaningful for Discord today
// (dashboard SSO); the parameter is still threaded through for symmetry.
func (d *DiscordOAuth) Login(w http.ResponseWriter, r *http.Request) {
	if d.disabled() {
		notImplemented(w)
		return
	}
	raw := make([]byte, oauthStateBytes)
	if _, err := rand.Read(raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	purpose := normalizePurpose(r.URL.Query().Get("purpose"))
	state := buildStateValue(base64.RawURLEncoding.EncodeToString(raw), purpose)

	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   int(oauthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   d.core.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, d.cfg.AuthCodeURL(state), http.StatusSeeOther)
}

// Callback handles GET /api/v1/auth/discord/callback. It validates the
// CSRF state, exchanges the code, fetches the Discord identity, and -
// only for an owner login - mints a dashboard session. Strangers are
// refused with 403 and no account is created.
func (d *DiscordOAuth) Callback(w http.ResponseWriter, r *http.Request) {
	if d.disabled() {
		notImplemented(w)
		return
	}
	ctx := r.Context()

	stateParam := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie(OAuthStateCookieName)
	if err != nil || stateCookie.Value == "" || stateParam == "" ||
		subtle.ConstantTimeCompare([]byte(stateParam), []byte(stateCookie.Value)) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_state"})
		return
	}
	// Clear the one-time state cookie.
	http.SetCookie(w, &http.Cookie{
		Name: OAuthStateCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: d.core.cookieSecure, SameSite: http.SameSiteLaxMode,
	})
	_, purpose := parseStateValue(stateCookie.Value)

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_code"})
		return
	}

	exchange := d.exchange
	if exchange == nil {
		exchange = func(ctx context.Context, code string) (*oauth2.Token, error) {
			return d.cfg.Exchange(ctx, code)
		}
	}
	tok, err := exchange(ctx, code)
	if err != nil {
		d.core.logger.ErrorContext(ctx, "discord oauth: token exchange failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "oauth_exchange_failed"})
		return
	}

	fetch := d.fetchUser
	if fetch == nil {
		fetch = fetchDiscordUser
	}
	du, err := fetch(ctx, tok.AccessToken)
	if err != nil || strings.TrimSpace(du.ID) == "" || strings.TrimSpace(du.Username) == "" {
		d.core.logger.ErrorContext(ctx, "discord oauth: identity fetch failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "identity_fetch_failed"})
		return
	}
	providerUserID := strings.TrimSpace(du.ID)
	login := strings.ToLower(strings.TrimSpace(du.Username))

	// Discord dashboard login is owner-only; there is no bot-token grant
	// flow here, so a non-owner is always refused with no account created.
	if !d.core.isOwnerLogin(ctx, auth.ProviderDiscord, providerUserID, login) {
		d.core.logger.WarnContext(ctx, "discord oauth: login refused, not an owner",
			slog.String("login", login))
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not_authorized"})
		return
	}

	user, err := d.core.findOrCreateUser(ctx, auth.ProviderDiscord, providerUserID, login, du.Email, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	if _, err := d.core.store.CreateOAuthIdentity(ctx, auth.OAuthIdentity{
		TenantID:       d.core.tenantID,
		UserID:         user.ID,
		Provider:       auth.ProviderDiscord,
		ProviderUserID: providerUserID,
		ProviderLogin:  login,
		Purpose:        purpose,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		Scopes:         parseGrantedScopes(tok),
		ExpiresAt:      tok.Expiry,
	}); err != nil {
		d.core.logger.ErrorContext(ctx, "discord oauth: persist identity failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	if err := d.core.mintSessionCookie(ctx, w, r, user); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	http.Redirect(w, r, "/?login=success", http.StatusSeeOther)
}

// fetchDiscordUser calls GET /users/@me with the bearer token.
func fetchDiscordUser(ctx context.Context, accessToken string) (discordUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discordUserEndpoint, nil)
	if err != nil {
		return discordUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return discordUser{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return discordUser{}, errDiscordStatus(resp.StatusCode)
	}
	var du discordUser
	if err := json.NewDecoder(resp.Body).Decode(&du); err != nil {
		return discordUser{}, err
	}
	return du, nil
}

package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

// KickStateCookieName carries the CSRF state between the Kick connect redirect
// and its callback. KickVerifierCookieName carries the PKCE code_verifier the
// callback must replay at token exchange. Both are distinct from the other
// connect/login cookies so the flows can never be confused.
const (
	KickStateCookieName    = "engelos_kick_state"
	KickVerifierCookieName = "engelos_kick_verifier"
)

// kickProfileURL is the Kick public API endpoint that returns the
// authenticated user, used to record which Kick account was linked as the
// outbound bot identity.
const kickProfileURL = "https://api.kick.com/public/v1/users"

// KickOAuth bundles the "Connect Kick" HTTP handlers. Like the YouTube connect
// flow it links a Kick account to the already-logged-in dashboard user as the
// tenant's outbound bot identity (Purpose=bot). Kick requires OAuth 2.1
// Authorization Code with PKCE, so this flow additionally generates a
// code_verifier at Login and replays it at exchange. It degrades to 501 when
// no store or oauth2 config is wired.
//
// KickOAuth is safe for concurrent use; all configuration is fixed at
// construction.
type KickOAuth struct {
	store        auth.Store
	tenantID     string
	logger       *slog.Logger
	cookieSecure bool
	cfg          *oauth2.Config

	httpClient *http.Client
	profileURL string
	exchange   func(ctx context.Context, code, verifier string) (*oauth2.Token, error)
}

// NewKickOAuth constructs the Kick connect handler. store and cfg may be nil;
// in that case every handler returns 501 so the router still builds with the
// feature off.
func NewKickOAuth(store auth.Store, tenantID string, logger *slog.Logger, cfg *oauth2.Config) *KickOAuth {
	if logger == nil {
		logger = slog.Default()
	}
	return &KickOAuth{
		store:        store,
		tenantID:     strings.TrimSpace(tenantID),
		logger:       logger.With("component", "api.handlers.kick_oauth"),
		cookieSecure: true,
		cfg:          cfg,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		profileURL:   kickProfileURL,
	}
}

// WithCookieSecure controls the Secure attribute on the state and verifier
// cookies. Tests pass false because httptest serves plain HTTP.
func (o *KickOAuth) WithCookieSecure(secure bool) *KickOAuth {
	o.cookieSecure = secure
	return o
}

func (o *KickOAuth) disabled() bool { return o.store == nil || o.cfg == nil }

// Login handles GET /api/v1/auth/kick/login. It requires an authenticated
// dashboard session (enforced by RequireGlobalOwner at the router), mints a
// CSRF state cookie plus a PKCE code_verifier cookie, and redirects to Kick's
// authorize endpoint with the S256 challenge.
func (o *KickOAuth) Login(w http.ResponseWriter, r *http.Request) {
	if o.disabled() {
		notImplemented(w)
		return
	}
	random, err := generateState()
	if err != nil {
		o.logger.ErrorContext(r.Context(), "kick oauth: generate state failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	verifier := oauth2.GenerateVerifier()

	o.setCookie(w, KickStateCookieName, random)
	o.setCookie(w, KickVerifierCookieName, verifier)

	authURL := o.cfg.AuthCodeURL(random, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (o *KickOAuth) setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(oauthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (o *KickOAuth) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Callback handles GET /api/v1/auth/kick/callback. It validates state,
// exchanges the code with the replayed PKCE verifier, fetches the Kick profile,
// and stores the encrypted tokens as the tenant's bot identity.
func (o *KickOAuth) Callback(w http.ResponseWriter, r *http.Request) {
	if o.disabled() {
		notImplemented(w)
		return
	}
	ctx := r.Context()

	user, ok := middleware.UserFromContext(ctx)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	state := r.URL.Query().Get("state")
	stateCookie, stateErr := r.Cookie(KickStateCookieName)
	verifierCookie, verifierErr := r.Cookie(KickVerifierCookieName)
	o.clearCookie(w, KickStateCookieName)
	o.clearCookie(w, KickVerifierCookieName)

	if e := r.URL.Query().Get("error"); e != "" {
		o.logger.WarnContext(ctx, "kick oauth: provider returned error", slog.String("error", e))
		http.Redirect(w, r, "/?kick=error", http.StatusSeeOther)
		return
	}

	if stateErr != nil || stateCookie == nil || stateCookie.Value == "" || state == "" ||
		subtle.ConstantTimeCompare([]byte(state), []byte(stateCookie.Value)) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_state"})
		return
	}
	if verifierErr != nil || verifierCookie == nil || verifierCookie.Value == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_verifier"})
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_code"})
		return
	}

	exchange := o.exchange
	if exchange == nil {
		exchange = func(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
			return o.cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
		}
	}
	tok, err := exchange(ctx, code, verifierCookie.Value)
	if err != nil {
		o.logger.ErrorContext(ctx, "kick oauth: token exchange failed", slog.Any("err", err))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "oauth_exchange_failed"})
		return
	}

	profileID, profileName := o.fetchProfile(ctx, tok.AccessToken)
	if profileID == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "identity_fetch_failed"})
		return
	}

	if _, err := o.store.CreateOAuthIdentity(ctx, auth.OAuthIdentity{
		TenantID:       o.tenantID,
		UserID:         user.ID,
		Provider:       auth.ProviderKick,
		ProviderUserID: profileID,
		ProviderLogin:  profileName,
		Purpose:        auth.OAuthPurposeBot,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		Scopes:         o.cfg.Scopes,
		ExpiresAt:      tok.Expiry,
	}); err != nil {
		o.logger.ErrorContext(ctx, "kick oauth: persist identity failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	http.Redirect(w, r, "/?kick=connected", http.StatusSeeOther)
}

// fetchProfile calls the Kick public users endpoint with the freshly issued
// access token and returns (id, name). An empty id signals failure. Kick
// returns the authenticated user in a data array; the name falls back to the
// id when blank.
func (o *KickOAuth) fetchProfile(ctx context.Context, accessToken string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.profileURL, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := o.httpClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "kick oauth: profile fetch failed", slog.Any("err", err))
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		o.logger.ErrorContext(ctx, "kick oauth: profile fetch status", slog.Int("status", resp.StatusCode))
		return "", ""
	}
	var body struct {
		Data []struct {
			UserID int    `json:"user_id"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if json.Unmarshal(raw, &body) != nil || len(body.Data) == 0 {
		return "", ""
	}
	id := strconv.Itoa(body.Data[0].UserID)
	name := strings.TrimSpace(body.Data[0].Name)
	if name == "" {
		name = id
	}
	return id, name
}

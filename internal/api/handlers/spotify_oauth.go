package handlers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/Luca-Pelzer/engelos/internal/api/middleware"
	"github.com/Luca-Pelzer/engelos/internal/auth"
)

// SpotifyStateCookieName carries the CSRF state between the Spotify connect
// redirect and its callback. It is distinct from the Twitch login state cookie
// so the two flows can never be confused for one another.
const SpotifyStateCookieName = "engelos_spotify_state"

// spotifyProfileURL is the Spotify Web API endpoint that returns the
// authenticated user's profile, used to record which Spotify account was
// linked.
const spotifyProfileURL = "https://api.spotify.com/v1/me"

// SpotifyOAuth bundles the "Connect Spotify" HTTP handlers. Unlike the Twitch
// login flow (which mints a dashboard session), this flow links a Spotify
// account to the ALREADY-logged-in dashboard user as the tenant's outbound
// bot identity (Purpose=bot), so song requests can drive that account's
// playback. It degrades to 501 when no store or oauth2 config is wired.
//
// SpotifyOAuth is safe for concurrent use; all configuration is fixed at
// construction.
type SpotifyOAuth struct {
	store        auth.Store
	tenantID     string
	logger       *slog.Logger
	cookieSecure bool
	cfg          *oauth2.Config

	// httpClient fetches the Spotify profile. A test seam: nil falls back to a
	// 10s-timeout client.
	httpClient *http.Client
	// profileURL is the /v1/me endpoint; a test seam overriding the default
	// production URL so tests can point at an httptest server.
	profileURL string
	// exchange is a test seam around cfg.Exchange.
	exchange func(ctx context.Context, code string) (*oauth2.Token, error)
}

// NewSpotifyOAuth constructs the Spotify connect handler. store and cfg may be
// nil; in that case every handler returns 501 so the router still builds with
// the feature off.
func NewSpotifyOAuth(store auth.Store, tenantID string, logger *slog.Logger, cfg *oauth2.Config) *SpotifyOAuth {
	if logger == nil {
		logger = slog.Default()
	}
	return &SpotifyOAuth{
		store:        store,
		tenantID:     strings.TrimSpace(tenantID),
		logger:       logger.With("component", "api.handlers.spotify_oauth"),
		cookieSecure: true,
		cfg:          cfg,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		profileURL:   spotifyProfileURL,
	}
}

// WithCookieSecure controls the Secure attribute on the state cookie. Tests
// pass false because httptest serves plain HTTP.
func (o *SpotifyOAuth) WithCookieSecure(secure bool) *SpotifyOAuth {
	o.cookieSecure = secure
	return o
}

func (o *SpotifyOAuth) disabled() bool { return o.store == nil || o.cfg == nil }

// Login handles GET /api/v1/auth/spotify/login. It requires an authenticated
// dashboard session (enforced by RequireSession at the router), mints a CSRF
// state cookie, and redirects to Spotify's authorize endpoint.
func (o *SpotifyOAuth) Login(w http.ResponseWriter, r *http.Request) {
	if o.disabled() {
		notImplemented(w)
		return
	}
	random, err := generateState()
	if err != nil {
		o.logger.ErrorContext(r.Context(), "spotify oauth: generate state failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SpotifyStateCookieName,
		Value:    random,
		Path:     "/",
		MaxAge:   int(oauthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, o.cfg.AuthCodeURL(random), http.StatusFound)
}

func (o *SpotifyOAuth) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SpotifyStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Callback handles GET /api/v1/auth/spotify/callback. It validates state,
// exchanges the code, fetches the Spotify profile, and stores the encrypted
// tokens as the tenant's bot identity linked to the logged-in user.
func (o *SpotifyOAuth) Callback(w http.ResponseWriter, r *http.Request) {
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

	if e := r.URL.Query().Get("error"); e != "" {
		o.logger.WarnContext(ctx, "spotify oauth: provider returned error", slog.String("error", e))
		o.clearStateCookie(w)
		http.Redirect(w, r, "/?spotify=error", http.StatusSeeOther)
		return
	}

	state := r.URL.Query().Get("state")
	cookie, cookieErr := r.Cookie(SpotifyStateCookieName)
	o.clearStateCookie(w)
	if cookieErr != nil || cookie == nil || cookie.Value == "" || state == "" ||
		subtle.ConstantTimeCompare([]byte(state), []byte(cookie.Value)) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_state"})
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_code"})
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
		o.logger.ErrorContext(ctx, "spotify oauth: token exchange failed", slog.Any("err", err))
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
		Provider:       auth.ProviderSpotify,
		ProviderUserID: profileID,
		ProviderLogin:  profileName,
		Purpose:        auth.OAuthPurposeBot,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		Scopes:         o.cfg.Scopes,
		ExpiresAt:      tok.Expiry,
	}); err != nil {
		o.logger.ErrorContext(ctx, "spotify oauth: persist identity failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	http.Redirect(w, r, "/?spotify=connected", http.StatusSeeOther)
}

// fetchProfile calls GET /v1/me with the freshly issued access token and
// returns (id, display-name). An empty id signals failure.
func (o *SpotifyOAuth) fetchProfile(ctx context.Context, accessToken string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.profileURL, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := o.httpClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "spotify oauth: profile fetch failed", slog.Any("err", err))
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		o.logger.ErrorContext(ctx, "spotify oauth: profile fetch status", slog.Int("status", resp.StatusCode))
		return "", ""
	}
	var body struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if json.Unmarshal(raw, &body) != nil {
		return "", ""
	}
	name := body.DisplayName
	if strings.TrimSpace(name) == "" {
		name = body.ID
	}
	return strings.TrimSpace(body.ID), strings.TrimSpace(name)
}

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

// YouTubeStateCookieName carries the CSRF state between the YouTube connect
// redirect and its callback. It is distinct from the other connect/login state
// cookies so the flows can never be confused for one another.
const YouTubeStateCookieName = "engelos_youtube_state"

// youtubeProfileURL is the Google userinfo endpoint that returns the
// authenticated user's profile, used to record which Google account was
// linked. The actual channel/liveChatId is resolved later by the adapter from
// its own config, not here.
const youtubeProfileURL = "https://www.googleapis.com/oauth2/v2/userinfo"

// YouTubeOAuth bundles the "Connect YouTube" HTTP handlers. Like the Spotify
// connect flow (and unlike the Twitch/Discord login flows that mint dashboard
// sessions), this flow links a Google/YouTube account to the ALREADY-logged-in
// dashboard user as the tenant's outbound bot identity (Purpose=bot), so the
// live-chat adapter can read and write chat as that account. It degrades to
// 501 when no store or oauth2 config is wired.
//
// YouTubeOAuth is safe for concurrent use; all configuration is fixed at
// construction.
type YouTubeOAuth struct {
	store        auth.Store
	tenantID     string
	logger       *slog.Logger
	cookieSecure bool
	cfg          *oauth2.Config

	// httpClient fetches the Google userinfo profile. A test seam: nil falls
	// back to a 10s-timeout client.
	httpClient *http.Client
	// profileURL is the userinfo endpoint; a test seam overriding the default
	// production URL so tests can point at an httptest server.
	profileURL string
	// exchange is a test seam around cfg.Exchange.
	exchange func(ctx context.Context, code string) (*oauth2.Token, error)
}

// NewYouTubeOAuth constructs the YouTube connect handler. store and cfg may be
// nil; in that case every handler returns 501 so the router still builds with
// the feature off.
func NewYouTubeOAuth(store auth.Store, tenantID string, logger *slog.Logger, cfg *oauth2.Config) *YouTubeOAuth {
	if logger == nil {
		logger = slog.Default()
	}
	return &YouTubeOAuth{
		store:        store,
		tenantID:     strings.TrimSpace(tenantID),
		logger:       logger.With("component", "api.handlers.youtube_oauth"),
		cookieSecure: true,
		cfg:          cfg,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		profileURL:   youtubeProfileURL,
	}
}

// WithCookieSecure controls the Secure attribute on the state cookie. Tests
// pass false because httptest serves plain HTTP.
func (o *YouTubeOAuth) WithCookieSecure(secure bool) *YouTubeOAuth {
	o.cookieSecure = secure
	return o
}

func (o *YouTubeOAuth) disabled() bool { return o.store == nil || o.cfg == nil }

// Login handles GET /api/v1/auth/youtube/login. It requires an authenticated
// dashboard session (enforced by RequireGlobalOwner at the router), mints a
// CSRF state cookie, and redirects to Google's authorize endpoint requesting
// offline access with a forced consent prompt so a refresh token is always
// issued.
func (o *YouTubeOAuth) Login(w http.ResponseWriter, r *http.Request) {
	if o.disabled() {
		notImplemented(w)
		return
	}
	random, err := generateState()
	if err != nil {
		o.logger.ErrorContext(r.Context(), "youtube oauth: generate state failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     YouTubeStateCookieName,
		Value:    random,
		Path:     "/",
		MaxAge:   int(oauthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	authURL := o.cfg.AuthCodeURL(random, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (o *YouTubeOAuth) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     YouTubeStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Callback handles GET /api/v1/auth/youtube/callback. It validates state,
// exchanges the code, fetches the Google profile, and stores the encrypted
// tokens as the tenant's bot identity linked to the logged-in user.
func (o *YouTubeOAuth) Callback(w http.ResponseWriter, r *http.Request) {
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
		o.logger.WarnContext(ctx, "youtube oauth: provider returned error", slog.String("error", e))
		o.clearStateCookie(w)
		http.Redirect(w, r, "/?youtube=error", http.StatusSeeOther)
		return
	}

	state := r.URL.Query().Get("state")
	cookie, cookieErr := r.Cookie(YouTubeStateCookieName)
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
		o.logger.ErrorContext(ctx, "youtube oauth: token exchange failed", slog.Any("err", err))
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
		Provider:       auth.ProviderYouTube,
		ProviderUserID: profileID,
		ProviderLogin:  profileName,
		Purpose:        auth.OAuthPurposeBot,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		Scopes:         o.cfg.Scopes,
		ExpiresAt:      tok.Expiry,
	}); err != nil {
		o.logger.ErrorContext(ctx, "youtube oauth: persist identity failed", slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	http.Redirect(w, r, "/?youtube=connected", http.StatusSeeOther)
}

// fetchProfile calls the Google userinfo endpoint with the freshly issued
// access token and returns (id, name). An empty id signals failure. The name
// falls back to the email and then the id when blank.
func (o *YouTubeOAuth) fetchProfile(ctx context.Context, accessToken string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.profileURL, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := o.httpClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "youtube oauth: profile fetch failed", slog.Any("err", err))
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		o.logger.ErrorContext(ctx, "youtube oauth: profile fetch status", slog.Int("status", resp.StatusCode))
		return "", ""
	}
	var body struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if json.Unmarshal(raw, &body) != nil {
		return "", ""
	}
	name := body.Name
	if strings.TrimSpace(name) == "" {
		name = body.Email
	}
	if strings.TrimSpace(name) == "" {
		name = body.ID
	}
	return strings.TrimSpace(body.ID), strings.TrimSpace(name)
}

// Package oauthrefresh runs a background loop that proactively
// refreshes OAuth user-access tokens before they expire. It is wired
// to a concrete persistence store (e.g. internal/auth.Store) and a
// concrete token source (e.g. *oauth2.Config) in main; this package
// itself depends on neither, so it stays trivially unit-testable.
package oauthrefresh

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// TokenSource exchanges a refresh token for a fresh access token at the
// upstream identity provider. It is defined here as an interface so the
// refresher loop is decoupled from any concrete oauth2 client and can
// be exercised by fakes in tests.
//
// Implementations MUST return a non-empty newRefreshToken: if the
// provider did not rotate the refresh token they should return the
// SAME refresh token that was passed in, never the empty string. The
// refresher additionally normalises empty returns to the input value
// to defend against misbehaving sources.
type TokenSource interface {
	Refresh(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, expiry time.Time, err error)
}

// Identity is the minimal view of an OAuth identity the refresher
// needs to do its job. It deliberately omits encrypted material and
// scopes: this package only handles refresh + re-persist of tokens.
type Identity struct {
	ID            string
	Provider      string
	ProviderLogin string
	Purpose       string
	RefreshToken  string
	ExpiresAt     time.Time
}

// Store is the narrow persistence surface the refresher consumes. The
// concrete implementation in main wraps internal/auth.Store and maps
// auth.OAuthIdentity → Identity.
type Store interface {
	ListExpiring(ctx context.Context, cutoff time.Time) ([]Identity, error)
	UpdateTokens(ctx context.Context, id, accessToken, refreshToken string, expiresAt time.Time) error
}

// RefreshEvent is delivered to Config.OnRefresh after a successful
// refresh has been persisted. Identity carries the pre-refresh
// snapshot (ID, Provider, Purpose, Login, the OLD ExpiresAt).
// AccessToken and ExpiresAt are the newly issued values. The
// refresh token itself is intentionally NOT in the event payload to
// limit incidental exposure; consumers that need it should look it
// up via the Store.
type RefreshEvent struct {
	Identity    Identity
	AccessToken string
	ExpiresAt   time.Time
}

// Config wires a Refresher.
type Config struct {
	Store         Store
	Tokens        TokenSource
	Logger        *slog.Logger
	Interval      time.Duration
	RefreshWindow time.Duration
	OnRefresh     func(RefreshEvent)
	Now           func() time.Time
}

// ErrInvalidConfig is returned by New when Config is missing a Store
// or a TokenSource (the two non-defaultable dependencies).
var ErrInvalidConfig = errors.New("oauthrefresh: invalid config")

const (
	defaultInterval      = 5 * time.Minute
	defaultRefreshWindow = 15 * time.Minute
)

// Refresher periodically scans the Store for identities whose access
// token expires within RefreshWindow and exchanges their refresh
// token for a fresh access token via TokenSource. It is single-
// goroutine (the Run ticker loop) and holds no shared mutable state.
type Refresher struct {
	store         Store
	tokens        TokenSource
	log           *slog.Logger
	interval      time.Duration
	refreshWindow time.Duration
	onRefresh     func(RefreshEvent)
	now           func() time.Time
}

// New validates cfg and returns a ready-to-Run Refresher. Store and
// Tokens are mandatory. Logger defaults to slog.Default; Now defaults
// to time.Now; Interval defaults to 5m; RefreshWindow defaults to 15m.
// OnRefresh is optional.
func New(cfg Config) (*Refresher, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: Store is nil", ErrInvalidConfig)
	}
	if cfg.Tokens == nil {
		return nil, fmt.Errorf("%w: Tokens is nil", ErrInvalidConfig)
	}
	r := &Refresher{
		store:         cfg.Store,
		tokens:        cfg.Tokens,
		log:           cfg.Logger,
		interval:      cfg.Interval,
		refreshWindow: cfg.RefreshWindow,
		onRefresh:     cfg.OnRefresh,
		now:           cfg.Now,
	}
	if r.log == nil {
		r.log = slog.Default()
	}
	if r.interval <= 0 {
		r.interval = defaultInterval
	}
	if r.refreshWindow <= 0 {
		r.refreshWindow = defaultRefreshWindow
	}
	if r.now == nil {
		r.now = time.Now
	}
	return r, nil
}

// Run drives the refresher loop until ctx is cancelled. It executes
// one immediate pass on entry, then a pass every Interval. Per-
// identity failures are logged and skipped; only ctx cancellation
// terminates the loop. Run returns nil on a clean shutdown.
func (r *Refresher) Run(ctx context.Context) error {
	r.refreshDue(ctx)

	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			r.refreshDue(ctx)
		}
	}
}

// RefreshNow performs a single on-demand refresh pass. Useful for
// tests and for a future manual-trigger surface in the admin API.
func (r *Refresher) RefreshNow(ctx context.Context) error {
	r.refreshDue(ctx)
	return nil
}

func (r *Refresher) refreshDue(ctx context.Context) {
	cutoff := r.now().Add(r.refreshWindow)
	ids, err := r.store.ListExpiring(ctx, cutoff)
	if err != nil {
		r.log.Warn("oauthrefresh: list expiring failed", "err", err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		r.refreshOne(ctx, id)
	}
}

func (r *Refresher) refreshOne(ctx context.Context, id Identity) {
	if id.RefreshToken == "" {
		r.log.Debug("oauthrefresh: skipping identity with no refresh token",
			"id", id.ID, "provider", id.Provider, "login", id.ProviderLogin, "purpose", id.Purpose)
		return
	}
	access, newRefresh, expiry, err := r.tokens.Refresh(ctx, id.RefreshToken)
	if err != nil {
		r.log.Warn("oauthrefresh: token refresh failed",
			"id", id.ID, "provider", id.Provider, "login", id.ProviderLogin, "purpose", id.Purpose, "err", err)
		return
	}
	// Providers may legitimately keep the refresh token unchanged and
	// signal that by returning an empty value. Persisting "" would
	// wipe the stored refresh token and break the next refresh, so we
	// fall back to the incoming refresh token in that case.
	persistRefresh := normalizeRefresh(id.RefreshToken, newRefresh)
	if err := r.store.UpdateTokens(ctx, id.ID, access, persistRefresh, expiry); err != nil {
		r.log.Warn("oauthrefresh: persist refreshed tokens failed",
			"id", id.ID, "provider", id.Provider, "login", id.ProviderLogin, "purpose", id.Purpose, "err", err)
		return
	}
	r.log.Info("oauthrefresh: refreshed",
		"id", id.ID, "provider", id.Provider, "login", id.ProviderLogin, "purpose", id.Purpose,
		"expires_at", expiry.UTC())

	if r.onRefresh != nil {
		r.fireHook(RefreshEvent{Identity: id, AccessToken: access, ExpiresAt: expiry})
	}
}

// fireHook isolates a user-supplied callback so a panic in it can
// never tear down the refresher loop.
func (r *Refresher) fireHook(ev RefreshEvent) {
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("oauthrefresh: OnRefresh hook panicked",
				"id", ev.Identity.ID, "panic", rec)
		}
	}()
	r.onRefresh(ev)
}

// normalizeRefresh returns newRefresh when the provider rotated the
// token, otherwise the original value. Empty newRefresh is treated as
// "unchanged" so the stored refresh token is never blanked out.
func normalizeRefresh(original, newRefresh string) string {
	if newRefresh == "" {
		return original
	}
	return newRefresh
}

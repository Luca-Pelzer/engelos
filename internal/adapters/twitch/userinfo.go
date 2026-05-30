package twitch

import (
	"context"
	"fmt"
	"time"

	"github.com/nicklaw5/helix/v2"
)

// UserProfile is the neutral subset of a Twitch user's public profile the
// command layer needs for !accountage and !so. It deliberately omits the
// helix types so callers stay decoupled from the vendored client.
type UserProfile struct {
	ID          string
	Login       string
	DisplayName string
	CreatedAt   time.Time // account creation time (UTC)
}

// UserProfile looks up a user's public profile by login via Helix GetUsers.
// It returns an error when the Helix client is unavailable (anonymous mode),
// the lookup fails, or no such login exists.
func (a *Adapter) UserProfile(ctx context.Context, login string) (UserProfile, error) {
	if err := ctx.Err(); err != nil {
		return UserProfile{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return UserProfile{}, err
	}
	resp, err := hx.GetUsers(&helix.UsersParams{Logins: []string{normalizeLogin(login)}})
	if err != nil {
		return UserProfile{}, fmt.Errorf("twitch: lookup user %q: %w", login, err)
	}
	if err := helixStatusError("user profile", resp.StatusCode, resp.ErrorMessage); err != nil {
		return UserProfile{}, err
	}
	if len(resp.Data.Users) == 0 {
		return UserProfile{}, fmt.Errorf("twitch: no user for login %q", login)
	}
	u := resp.Data.Users[0]
	return UserProfile{
		ID:          u.ID,
		Login:       u.Login,
		DisplayName: u.DisplayName,
		CreatedAt:   u.CreatedAt.Time.UTC(),
	}, nil
}

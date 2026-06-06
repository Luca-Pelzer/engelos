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

// ErrNotFollowing reports that a viewer does not currently follow the channel,
// so !followage can say so rather than surfacing a generic error.
var ErrNotFollowing = fmt.Errorf("twitch: user is not following the channel")

// FollowAge returns when viewerLogin started following channelLogin. It resolves
// both logins to IDs via GetUsers, then queries Helix GetChannelFollows (which
// needs the moderator:read:followers scope and broadcaster/mod auth, so it only
// works for the bot's own authenticated channel). Returns ErrNotFollowing when
// the viewer doesn't follow, or an error when Helix is unavailable or the lookup
// fails.
func (a *Adapter) FollowAge(ctx context.Context, channelLogin, viewerLogin string) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return time.Time{}, err
	}
	channel := normalizeLogin(channelLogin)
	viewer := normalizeLogin(viewerLogin)
	users, err := hx.GetUsers(&helix.UsersParams{Logins: []string{channel, viewer}})
	if err != nil {
		return time.Time{}, fmt.Errorf("twitch: resolve follow ids: %w", err)
	}
	if err := helixStatusError("follow lookup", users.StatusCode, users.ErrorMessage); err != nil {
		return time.Time{}, err
	}
	var broadcasterID, viewerID string
	for _, u := range users.Data.Users {
		switch u.Login {
		case channel:
			broadcasterID = u.ID
		case viewer:
			viewerID = u.ID
		}
	}
	if broadcasterID == "" || viewerID == "" {
		return time.Time{}, fmt.Errorf("twitch: could not resolve channel or viewer for follow lookup")
	}
	follows, err := hx.GetChannelFollows(&helix.GetChannelFollowsParams{
		BroadcasterID: broadcasterID,
		UserID:        viewerID,
		First:         1,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("twitch: get follow: %w", err)
	}
	if err := helixStatusError("get follow", follows.StatusCode, follows.ErrorMessage); err != nil {
		return time.Time{}, err
	}
	if len(follows.Data.Channels) == 0 {
		return time.Time{}, ErrNotFollowing
	}
	return follows.Data.Channels[0].Followed.Time.UTC(), nil
}

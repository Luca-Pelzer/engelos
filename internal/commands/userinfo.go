package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// defaultUserInfoCooldown throttles the read-only !accountage / !so commands.
const defaultUserInfoCooldown = 5 * time.Second

// UserProfile is the read-only profile snapshot the !accountage and !so
// commands need. It is declared here (rather than imported from the twitch
// adapter) so this package stays decoupled; main wires an adapter.
type UserProfile struct {
	Login       string
	DisplayName string
	CreatedAt   time.Time
}

// UserProfileProvider looks up a viewer's public profile by login. An adapter
// over the Twitch adapter is wired in main.
type UserProfileProvider interface {
	UserProfile(ctx context.Context, login string) (UserProfile, error)
}

// ErrNotFollowing is returned by a FollowAgeProvider when the viewer does not
// follow the channel, so !followage can report that distinctly from a failure.
var ErrNotFollowing = errors.New("not following")

// FollowAgeProvider reports how long a viewer has followed a channel. An
// adapter over the Twitch adapter is wired in main; it returns ErrNotFollowing
// when the viewer doesn't follow.
type FollowAgeProvider interface {
	FollowAge(ctx context.Context, channel, viewer string) (time.Time, error)
}

// NewFollowAgeCommand returns "!followage" (MinRole RoleEveryone, ~5s cooldown).
// With no argument it reports how long the caller has followed; with
// "!followage <name>" it reports that viewer's follow age. Replies that the
// user isn't following when applicable, and "couldn't look that up" on error.
func NewFollowAgeCommand(provider FollowAgeProvider) Command {
	return Command{
		Name:     "followage",
		Help:     "Show how long someone has followed the channel.",
		Cooldown: defaultUserInfoCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if provider == nil {
				return Reply{Text: "that's unavailable"}
			}
			channel := strings.TrimPrefix(strings.TrimSpace(msg.Channel), "#")
			if channel == "" {
				return Reply{Text: "couldn't tell which channel to check"}
			}
			viewer := firstTarget(args)
			self := viewer == ""
			if self {
				viewer = strings.TrimSpace(msg.Username)
			}
			if viewer == "" {
				return Reply{Text: "couldn't tell whose follow age to check"}
			}
			since, err := provider.FollowAge(ctx, channel, viewer)
			if errors.Is(err, ErrNotFollowing) {
				if self {
					return Reply{Text: fmt.Sprintf("%syou don't follow this channel (yet! 💜)", mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%s%s doesn't follow this channel", mentionPrefix(msg), viewer)}
			}
			if err != nil {
				return Reply{Text: "couldn't look that up right now"}
			}
			age := formatAge(time.Since(since))
			if self {
				return Reply{Text: fmt.Sprintf("%syou've followed for %s (since %s)", mentionPrefix(msg), age, since.Format("Jan 2006"))}
			}
			return Reply{Text: fmt.Sprintf("%s%s has followed for %s (since %s)", mentionPrefix(msg), viewer, age, since.Format("Jan 2006"))}
		},
	}
}

// NewAccountAgeCommand returns "!accountage" (MinRole RoleEveryone, ~5s
// cooldown). With no argument it reports the caller's account age; with
// "!accountage <name>" it reports that user's. Replies "couldn't look that up
// right now" on error and "that's unavailable" when no provider is wired.
func NewAccountAgeCommand(provider UserProfileProvider) Command {
	return Command{
		Name:     "accountage",
		Help:     "Show how long a Twitch account has existed.",
		Cooldown: defaultUserInfoCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if provider == nil {
				return Reply{Text: "that's unavailable"}
			}
			login := firstTarget(args)
			self := login == ""
			if self {
				login = strings.TrimSpace(msg.Username)
			}
			if login == "" {
				return Reply{Text: "couldn't tell whose account to check"}
			}
			profile, err := provider.UserProfile(ctx, login)
			if err != nil {
				return Reply{Text: "couldn't look that up right now"}
			}
			age := formatAge(time.Since(profile.CreatedAt))
			who := profile.DisplayName
			if who == "" {
				who = login
			}
			if self {
				return Reply{Text: fmt.Sprintf("%syour account is %s old (since %s)",
					mentionPrefix(msg), age, profile.CreatedAt.Format("Jan 2006"))}
			}
			return Reply{Text: fmt.Sprintf("%s%s's account is %s old (since %s)",
				mentionPrefix(msg), who, age, profile.CreatedAt.Format("Jan 2006"))}
		},
	}
}

// NewShoutoutCommand returns "!so" (MinRole RoleModerator, ~5s cooldown). It
// shouts out another streamer: "Go give <name> a follow at twitch.tv/<login> -
// they were last playing <game>!" (omitting the game when unknown). A missing
// argument yields usage. Uses both the profile provider (for the canonical
// login/name) and the stream-status provider (for the last category).
func NewShoutoutCommand(profiles UserProfileProvider, streams StreamStatusProvider) Command {
	return Command{
		Name:     "so",
		Help:     "Shout out another streamer.",
		MinRole:  RoleModerator,
		Cooldown: defaultUserInfoCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			target := firstTarget(args)
			if target == "" {
				return Reply{Text: fmt.Sprintf("%susage: !so <name>", mentionPrefix(msg))}
			}
			login := target
			display := target
			if profiles != nil {
				if p, err := profiles.UserProfile(ctx, target); err == nil {
					login = p.Login
					if p.DisplayName != "" {
						display = p.DisplayName
					}
				}
			}
			game := ""
			if streams != nil {
				if s, err := streams.Status(ctx, login); err == nil {
					game = strings.TrimSpace(s.GameName)
				}
			}
			if game != "" {
				return Reply{Text: fmt.Sprintf("📢 Go give %s a follow at twitch.tv/%s - they were last seen playing %s!",
					display, login, game)}
			}
			return Reply{Text: fmt.Sprintf("📢 Go give %s a follow at twitch.tv/%s - show them some love! 💜",
				display, login)}
		},
	}
}

// formatAge renders a long-lived duration as a human "Xy Ym" / "Ym Zd" / "Zd"
// string for account ages. It uses approximate year (365d) and month (30d)
// lengths - exact calendar math is unnecessary for a chat readout.
func formatAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days < 1 {
		return "less than a day"
	}
	years := days / 365
	rem := days % 365
	months := rem / 30
	switch {
	case years > 0:
		if months > 0 {
			return fmt.Sprintf("%dy %dm", years, months)
		}
		return fmt.Sprintf("%dy", years)
	case months > 0:
		d2 := rem % 30
		if d2 > 0 {
			return fmt.Sprintf("%dm %dd", months, d2)
		}
		return fmt.Sprintf("%dm", months)
	default:
		return fmt.Sprintf("%dd", days)
	}
}

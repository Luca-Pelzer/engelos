package commands

import (
	"context"
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
// shouts out another streamer: "Go give <name> a follow at twitch.tv/<login> —
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
				return Reply{Text: fmt.Sprintf("📢 Go give %s a follow at twitch.tv/%s — they were last seen playing %s!",
					display, login, game)}
			}
			return Reply{Text: fmt.Sprintf("📢 Go give %s a follow at twitch.tv/%s — show them some love! 💜",
				display, login)}
		},
	}
}

// formatAge renders a long-lived duration as a human "Xy Ym" / "Ym Zd" / "Zd"
// string for account ages. It uses approximate year (365d) and month (30d)
// lengths — exact calendar math is unnecessary for a chat readout.
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

package commands

import (
	"context"
	"fmt"
	"strings"
)

// CoHostConfigStore is the narrow management surface the !cohost built-in needs
// to read and change the per-channel AI co-host settings. An adapter over
// [github.com/Luca-Pelzer/engelos/internal/cohost.Store] is wired in main; this
// interface lives HERE so internal/commands does NOT import internal/cohost
// (mirrors [FeatureToggleStore] and [TranslateConfigStore]).
//
// CoHostStatus reports whether the co-host is on and the name viewers address;
// SetCoHostEnabled flips the channel switch; SetCoHostName and SetCoHostPersona
// persist the bot name and persona. main's adapter chooses the tenant_id and
// normalises the channel.
type CoHostConfigStore interface {
	CoHostStatus(ctx context.Context, channel string) (enabled bool, botName string)
	SetCoHostEnabled(ctx context.Context, channel string, enabled bool) error
	SetCoHostName(ctx context.Context, channel, name string) error
	SetCoHostPersona(ctx context.Context, channel, persona string) error
}

// NewCoHostToggleCommand returns "!cohost". Mods-only.
//
// Usage: "!cohost" / "!cohost status" reports the current state;
// "!cohost on" / "!cohost off" flips the AI co-host; "!cohost name <name>"
// sets how viewers address it; "!cohost persona <text>" sets its style. A nil
// store yields a one-line "co-host is unavailable" reply.
func NewCoHostToggleCommand(store CoHostConfigStore) Command {
	return Command{
		Name:         "cohost",
		Help:         "Turn the AI co-host on or off and set its name/persona - !cohost on|off|status|name <name>|persona <text>.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%sco-host is unavailable", mentionPrefix(msg))}
			}
			action := "status"
			if len(args) > 0 {
				action = strings.ToLower(strings.TrimSpace(args[0]))
			}
			switch action {
			case "status", "":
				enabled, name := store.CoHostStatus(ctx, msg.Channel)
				return Reply{Text: fmt.Sprintf("%sAI co-host is %s (name: %s)",
					mentionPrefix(msg), onOff(enabled), name)}
			case "on", "enable", "enabled":
				if err := store.SetCoHostEnabled(ctx, msg.Channel, true); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the co-host setting", mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%sAI co-host is now ON ✅", mentionPrefix(msg))}
			case "off", "disable", "disabled":
				if err := store.SetCoHostEnabled(ctx, msg.Channel, false); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the co-host setting", mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%sAI co-host is now OFF 🚫", mentionPrefix(msg))}
			case "name":
				if len(args) < 2 {
					return Reply{Text: fmt.Sprintf("%susage: !cohost name <name>", mentionPrefix(msg))}
				}
				name := strings.TrimSpace(args[1])
				if err := store.SetCoHostName(ctx, msg.Channel, name); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the co-host name", mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%sco-host name is now %q", mentionPrefix(msg), name)}
			case "persona":
				if len(args) < 2 {
					return Reply{Text: fmt.Sprintf("%susage: !cohost persona <text>", mentionPrefix(msg))}
				}
				persona := strings.TrimSpace(strings.Join(args[1:], " "))
				if err := store.SetCoHostPersona(ctx, msg.Channel, persona); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the co-host persona", mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%sco-host persona updated", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf("%susage: !cohost on|off|status|name <name>|persona <text>",
					mentionPrefix(msg))}
			}
		},
	}
}

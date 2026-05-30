package commands

import (
	"context"
	"fmt"
	"strings"
)

// FeatureToggleStore is the narrow management surface the !economy built-in
// needs to read and flip the per-channel points-economy switch. An adapter over
// [github.com/Luca-Pelzer/engelos/internal/featureflags.Store] is wired in main;
// this interface lives HERE so internal/commands does NOT import
// internal/featureflags (mirrors [CounterStore] and [TimerStore]).
//
// EconomyEnabled reports the channel's current setting (defaulting to enabled
// when no override is stored); SetEconomy persists an explicit on/off override.
// main's adapter chooses the tenant_id and normalises the channel.
type FeatureToggleStore interface {
	EconomyEnabled(ctx context.Context, channel string) bool
	SetEconomy(ctx context.Context, channel string, enabled bool) error
}

// NewEconomyToggleCommand returns "!economy". Mods-only.
//
// Usage: "!economy" or "!economy status" reports the current state;
// "!economy on" / "!economy off" flips the per-channel points economy, which
// switches earning and every points game (gamble, slots, duel, heist, rewards)
// on or off together. A nil store yields a one-line "economy toggle is
// unavailable" reply.
func NewEconomyToggleCommand(store FeatureToggleStore) Command {
	return Command{
		Name:         "economy",
		Help:         "Turn the points economy on or off — !economy on|off|status.",
		MinRole:      RoleModerator,
		UserCooldown: defaultAdminUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if store == nil {
				return Reply{Text: fmt.Sprintf("%seconomy toggle is unavailable",
					mentionPrefix(msg))}
			}
			action := "status"
			if len(args) > 0 {
				action = strings.ToLower(strings.TrimSpace(args[0]))
			}
			switch action {
			case "status", "":
				return Reply{Text: fmt.Sprintf("%spoints economy is %s",
					mentionPrefix(msg), onOff(store.EconomyEnabled(ctx, msg.Channel)))}
			case "on", "enable", "enabled":
				if err := store.SetEconomy(ctx, msg.Channel, true); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the economy setting",
						mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%spoints economy is now ON ✅", mentionPrefix(msg))}
			case "off", "disable", "disabled":
				if err := store.SetEconomy(ctx, msg.Channel, false); err != nil {
					return Reply{Text: fmt.Sprintf("%scouldn't update the economy setting",
						mentionPrefix(msg))}
				}
				return Reply{Text: fmt.Sprintf("%spoints economy is now OFF 🚫", mentionPrefix(msg))}
			default:
				return Reply{Text: fmt.Sprintf("%susage: !economy on|off|status",
					mentionPrefix(msg))}
			}
		},
	}
}

// onOff renders a feature-toggle boolean as the chat-facing "ON"/"OFF" label.
func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

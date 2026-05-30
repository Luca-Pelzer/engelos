package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// defaultRewardCooldown throttles the read-only !rewards listing per channel.
const defaultRewardCooldown = 5 * time.Second

// maxRewardDescLen caps a reward description the same as the store does, so the
// command rejects over-long input with a friendly message before hitting it.
const maxRewardDescLen = 200

// RewardItem is the decoupled view of a store reward the commands need, so the
// commands package never imports internal/rewards. main adapts the store onto
// the RewardCatalog interface below.
type RewardItem struct {
	Name        string
	Description string
	Cost        int64
}

// RewardOutcome is the result of a catalog mutation, mapped from the store's
// sentinels by the adapter in main.
type RewardOutcome int

const (
	// RewardOK signals success.
	RewardOK RewardOutcome = iota
	// RewardNotFound means no reward by that name exists.
	RewardNotFound
	// RewardExists means a reward with that name already exists.
	RewardExists
	// RewardInvalid means the name/cost/description failed validation.
	RewardInvalid
	// RewardUnavailable means the catalog itself is not wired or errored.
	RewardUnavailable
)

// RewardCatalog is the reward-definition surface the commands use. main wires
// an adapter over internal/rewards.Store.
type RewardCatalog interface {
	Add(ctx context.Context, channel, name string, cost int64, description, createdBy string) RewardOutcome
	Remove(ctx context.Context, channel, name string) RewardOutcome
	Get(ctx context.Context, channel, name string) (RewardItem, RewardOutcome)
	List(ctx context.Context, channel string) []RewardItem
}

// RedeemBank spends a viewer's loyalty points to redeem a reward. It mirrors
// the loyalty Spend semantics via LoyaltyError so the command can give precise
// feedback (not enough points, no account, etc.).
type RedeemBank interface {
	Spend(ctx context.Context, channel, viewerID string, amount int64) LoyaltyError
}

// RedeemSender announces a successful redemption to chat so the broadcaster
// sees that an item needs fulfilling.
type RedeemSender interface {
	Send(ctx context.Context, channel, message string) error
}

// NewRewardCommand returns "!reward" (MinRole RoleModerator): manage the reward
// catalog. Subcommands: "add <name> <cost> <description...>", "del <name>".
func NewRewardCommand(catalog RewardCatalog) Command {
	return Command{
		Name:    "reward",
		Help:    "Manage redeemable rewards: !reward add <name> <cost> <desc> | !reward del <name>",
		MinRole: RoleModerator,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if catalog == nil {
				return Reply{Text: "rewards are unavailable"}
			}
			if len(args) == 0 {
				return Reply{Text: fmt.Sprintf("%susage: !reward add <name> <cost> <desc> | !reward del <name>", mentionPrefix(msg))}
			}
			switch strings.ToLower(args[0]) {
			case "add":
				return rewardAdd(ctx, catalog, msg, args[1:])
			case "del", "delete", "remove":
				return rewardDel(ctx, catalog, msg, args[1:])
			default:
				return Reply{Text: fmt.Sprintf("%sunknown subcommand %q. Use add or del.", mentionPrefix(msg), args[0])}
			}
		},
	}
}

func rewardAdd(ctx context.Context, catalog RewardCatalog, msg Message, args []string) Reply {
	if len(args) < 2 {
		return Reply{Text: fmt.Sprintf("%susage: !reward add <name> <cost> <description>", mentionPrefix(msg))}
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	cost, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
	if err != nil || cost <= 0 {
		return Reply{Text: fmt.Sprintf("%scost must be a positive whole number", mentionPrefix(msg))}
	}
	desc := strings.TrimSpace(strings.Join(args[2:], " "))
	if len(desc) > maxRewardDescLen {
		return Reply{Text: fmt.Sprintf("%sdescription is too long (max %d chars)", mentionPrefix(msg), maxRewardDescLen)}
	}
	switch catalog.Add(ctx, msg.Channel, name, cost, desc, msg.Username) {
	case RewardOK:
		return Reply{Text: fmt.Sprintf("%sreward '%s' added for %s points ✅", mentionPrefix(msg), name, formatPoints(cost))}
	case RewardExists:
		return Reply{Text: fmt.Sprintf("%sa reward called '%s' already exists", mentionPrefix(msg), name)}
	case RewardInvalid:
		return Reply{Text: fmt.Sprintf("%sthat reward name is invalid (letters, numbers, underscore)", mentionPrefix(msg))}
	default:
		return Reply{Text: "couldn't add that reward right now"}
	}
}

func rewardDel(ctx context.Context, catalog RewardCatalog, msg Message, args []string) Reply {
	if len(args) < 1 {
		return Reply{Text: fmt.Sprintf("%susage: !reward del <name>", mentionPrefix(msg))}
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	switch catalog.Remove(ctx, msg.Channel, name) {
	case RewardOK:
		return Reply{Text: fmt.Sprintf("%sreward '%s' removed 🗑️", mentionPrefix(msg), name)}
	case RewardNotFound:
		return Reply{Text: fmt.Sprintf("%sthere's no reward called '%s'", mentionPrefix(msg), name)}
	default:
		return Reply{Text: "couldn't remove that reward right now"}
	}
}

// NewRewardsCommand returns "!rewards" (MinRole RoleEveryone): list the
// channel's redeemable rewards with their costs.
func NewRewardsCommand(catalog RewardCatalog) Command {
	return Command{
		Name:     "rewards",
		Help:     "List the rewards you can redeem with " + pointsName + ".",
		Cooldown: defaultRewardCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if catalog == nil {
				return Reply{Text: "rewards are unavailable"}
			}
			items := catalog.List(ctx, msg.Channel)
			if len(items) == 0 {
				return Reply{Text: "no rewards have been set up yet"}
			}
			parts := make([]string, 0, len(items))
			for _, it := range items {
				parts = append(parts, fmt.Sprintf("%s (%s)", it.Name, formatPoints(it.Cost)))
			}
			return Reply{Text: "🎁 Rewards: " + strings.Join(parts, " · ") + " - redeem with !redeem <name>"}
		},
	}
}

// NewRedeemCommand returns "!redeem" (MinRole RoleEveryone): spend points on a
// reward. On success it spends the cost and announces the redemption so the
// broadcaster can fulfil it.
func NewRedeemCommand(catalog RewardCatalog, bank RedeemBank, sender RedeemSender) Command {
	return Command{
		Name:         "redeem",
		Help:         "Redeem a reward with your " + pointsName + ": !redeem <name>",
		UserCooldown: 3 * time.Second,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if catalog == nil || bank == nil {
				return Reply{Text: "rewards are unavailable"}
			}
			if len(args) < 1 {
				return Reply{Text: fmt.Sprintf("%susage: !redeem <name>", mentionPrefix(msg))}
			}
			name := strings.ToLower(strings.TrimSpace(args[0]))
			item, status := catalog.Get(ctx, msg.Channel, name)
			if status != RewardOK {
				return Reply{Text: fmt.Sprintf("%sthere's no reward called '%s'", mentionPrefix(msg), name)}
			}
			switch bank.Spend(ctx, msg.Channel, msg.UserID, item.Cost) {
			case LoyaltyOK:
				// Announce so the streamer sees a reward needs fulfilling. The
				// announcement failing must not undo the spend, so the error is
				// ignored here (the viewer already paid and gets their reply).
				if sender != nil {
					_ = sender.Send(ctx, msg.Channel,
						fmt.Sprintf("🎁 %s redeemed '%s' for %s points!", msg.Username, name, formatPoints(item.Cost)))
				}
				return Reply{Text: fmt.Sprintf("%syou redeemed '%s' for %s points 🎉", mentionPrefix(msg), name, formatPoints(item.Cost))}
			case LoyaltyInsufficient:
				return Reply{Text: fmt.Sprintf("%syou need %s points to redeem '%s'", mentionPrefix(msg), formatPoints(item.Cost), name)}
			case LoyaltyNotFound:
				return Reply{Text: fmt.Sprintf("%syou don't have any %s yet", mentionPrefix(msg), pointsName)}
			default:
				return Reply{Text: "couldn't redeem that right now"}
			}
		},
	}
}

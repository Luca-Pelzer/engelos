package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// defaultLoyaltyCooldown throttles the read-only !points command per user.
const defaultLoyaltyCooldown = 3 * time.Second

// pointsName is the currency label shown in replies. Kept generic ("points")
// rather than channel-configurable for now.
const pointsName = "points"

// LoyaltyError mirrors the loyalty store's sentinel outcomes the commands need
// to react to, without importing the store package (decoupling). main wraps
// the store and maps its sentinels onto these.
type LoyaltyError int

const (
	// LoyaltyOK signals a successful operation.
	LoyaltyOK LoyaltyError = iota
	// LoyaltyNotFound means the viewer has no account yet.
	LoyaltyNotFound
	// LoyaltyInsufficient means the sender lacked the funds to give.
	LoyaltyInsufficient
	// LoyaltyInvalid means the request was malformed (e.g. non-positive amount).
	LoyaltyInvalid
	// LoyaltyUnavailable means the lookup itself failed (store error).
	LoyaltyUnavailable
)

// LoyaltyEntry is one row of the points leaderboard.
type LoyaltyEntry struct {
	Username string
	Balance  int64
}

// LoyaltyProvider is the narrow surface the loyalty commands need. main wires
// an adapter over internal/loyalty.Store. Balance returns a viewer's points;
// Transfer moves points between two viewers (resolved by username within the
// channel); Top returns the leaderboard.
type LoyaltyProvider interface {
	Balance(ctx context.Context, channel, viewerID string) (int64, LoyaltyError)
	Transfer(ctx context.Context, channel, fromViewerID, toUsername string, amount int64) (LoyaltyError, string)
	Top(ctx context.Context, channel string, n int) []LoyaltyEntry
}

// NewPointsCommand returns "!points" (MinRole RoleEveryone). With no argument
// it reports the caller's balance; otherwise it is reserved for future
// subcommands but currently still reports the caller. A viewer with no account
// reads as 0.
func NewPointsCommand(provider LoyaltyProvider) Command {
	return Command{
		Name:         "points",
		Help:         "Check how many " + pointsName + " you have.",
		UserCooldown: defaultLoyaltyCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if provider == nil {
				return Reply{Text: pointsName + " are unavailable"}
			}
			bal, status := provider.Balance(ctx, msg.Channel, msg.UserID)
			switch status {
			case LoyaltyOK:
				return Reply{Text: fmt.Sprintf("%shas %s %s", mentionPrefix(msg), formatPoints(bal), pointsName)}
			case LoyaltyNotFound:
				return Reply{Text: fmt.Sprintf("%shas 0 %s", mentionPrefix(msg), pointsName)}
			default:
				return Reply{Text: "couldn't check that right now"}
			}
		},
	}
}

// NewGiveCommand returns "!give" (MinRole RoleEveryone). "!give <user> <amount>"
// transfers points from the caller to another viewer.
func NewGiveCommand(provider LoyaltyProvider) Command {
	return Command{
		Name:         "give",
		Help:         "Give some of your " + pointsName + " to another viewer.",
		UserCooldown: defaultLoyaltyCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if provider == nil {
				return Reply{Text: pointsName + " are unavailable"}
			}
			if len(args) < 2 {
				return Reply{Text: fmt.Sprintf("%susage: !give <user> <amount>", mentionPrefix(msg))}
			}
			target := strings.TrimPrefix(strings.TrimSpace(args[0]), "@")
			amount, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
			if err != nil || amount <= 0 {
				return Reply{Text: fmt.Sprintf("%samount must be a positive whole number", mentionPrefix(msg))}
			}
			if strings.EqualFold(target, msg.Username) {
				return Reply{Text: fmt.Sprintf("%syou can't give %s to yourself", mentionPrefix(msg), pointsName)}
			}
			status, toName := provider.Transfer(ctx, msg.Channel, msg.UserID, target, amount)
			switch status {
			case LoyaltyOK:
				display := toName
				if display == "" {
					display = target
				}
				return Reply{Text: fmt.Sprintf("%sgave %s %s to %s 🎁",
					mentionPrefix(msg), formatPoints(amount), pointsName, display)}
			case LoyaltyInsufficient:
				return Reply{Text: fmt.Sprintf("%syou don't have enough %s", mentionPrefix(msg), pointsName)}
			case LoyaltyNotFound:
				return Reply{Text: fmt.Sprintf("%syou don't have any %s yet", mentionPrefix(msg), pointsName)}
			case LoyaltyInvalid:
				return Reply{Text: fmt.Sprintf("%scouldn't send that", mentionPrefix(msg))}
			default:
				return Reply{Text: "couldn't send that right now"}
			}
		},
	}
}

// NewPointsLeaderboardCommand returns "!pointslb" (MinRole RoleEveryone, ~10s
// global cooldown): the top points holders in the channel.
func NewPointsLeaderboardCommand(provider LoyaltyProvider) Command {
	return Command{
		Name:     "pointslb",
		Help:     "Show the top " + pointsName + " holders.",
		Cooldown: defaultLeaderboardCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if provider == nil {
				return Reply{Text: pointsName + " are unavailable"}
			}
			top := provider.Top(ctx, msg.Channel, defaultLeaderboardTopN)
			if len(top) == 0 {
				return Reply{Text: "no " + pointsName + " have been earned yet"}
			}
			parts := make([]string, 0, len(top))
			for i, e := range top {
				parts = append(parts, fmt.Sprintf("%d. %s (%s)", i+1, e.Username, formatPoints(e.Balance)))
			}
			return Reply{Text: fmt.Sprintf("🏆 Top %s: %s", pointsName, strings.Join(parts, " · "))}
		},
	}
}

// formatPoints renders a points balance with thousands separators for
// readability in chat (e.g. 12345 → "12,345").
func formatPoints(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	out := b.String()
	if neg {
		return "-" + out
	}
	return out
}

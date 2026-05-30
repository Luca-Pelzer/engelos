package commands

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// gameUserCooldown throttles per-user spam of the gambling games. 5s is long
// enough to stop trivial double-taps but short enough not to annoy a viewer
// who legitimately wants another spin.
const gameUserCooldown = 5 * time.Second

// gambleWinChance is the !gamble win probability as a percentage. A 47% chance
// on an even-money (double-or-nothing) bet gives the house a 6% edge:
// EV = 0.47*2 - 1 = -0.06, so over time the points sink wins — which is the
// point of a loyalty-points game (keeps the economy from inflating).
const gambleWinChance = 47

// slotSymbols are the !slots reel faces, ordered cheapest→rarest by intent.
// All reels draw uniformly from this same slice; the payout table (not the
// distribution) is what tunes the house edge. Index reference for tests:
// 🍒=0 🍋=1 🔔=2 ⭐=3 💎=4 7️⃣=5.
var slotSymbols = []string{"🍒", "🍋", "🔔", "⭐", "💎", "7️⃣"}

// GameBank is the wagering surface the gambling games need. Wager atomically
// removes `bet` from the player (the stake) and, when the game is won, credits
// `payout` (which already INCLUDES the returned stake, i.e. payout = bet*mult
// for a win, 0 for a loss). Implementations must spend-then-credit so a player
// can never go negative. The returned newBalance is the player's balance AFTER
// settlement. status reports the outcome of the spend leg.
type GameBank interface {
	// Wager spends `bet`; if won, credits `payout`. Returns the post-settlement
	// balance and a LoyaltyError (LoyaltyOK on success, LoyaltyInsufficient when
	// the player can't afford the bet, LoyaltyNotFound when they have no account,
	// LoyaltyInvalid for a bad amount, LoyaltyUnavailable on store failure).
	Wager(ctx context.Context, channel, viewerID string, bet, payout int64) (newBalance int64, status LoyaltyError)
	// Balance returns the player's current balance (for "all"/"max" bet parsing).
	Balance(ctx context.Context, channel, viewerID string) (int64, LoyaltyError)
}

// NewGambleCommand returns "!gamble" (MinRole RoleEveryone, 5s per-user
// cooldown). "!gamble <amount|all|50%>" stakes the amount on a coin-flip with
// a 47% win chance (see [gambleWinChance]); a win pays double, a loss forfeits
// the stake. The command picks win/loss with its own RNG and then calls
// [GameBank.Wager] with payout = bet*2 (win) or 0 (loss) so the bank only has
// to move the money. A nil bank yields an "unavailable" reply.
func NewGambleCommand(bank GameBank) Command { return newGambleCommand(bank, rand.Int63) }

// newGambleCommand is the injectable form: randInt63 supplies a non-negative
// int63-ish value mapped onto a 1..100 roll. Tests inject a stub for exact
// outcomes.
func newGambleCommand(bank GameBank, randInt63 func() int64) Command {
	return Command{
		Name:         "gamble",
		Help:         "Bet your " + pointsName + " on a coin flip — !gamble <amount|all|50%>.",
		UserCooldown: gameUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			return playWager(ctx, bank, msg, args, "gamble", "gamble",
				func(bet int64) (int64, func(int64) string, func(int64) string) {
					// Modulo maps the RNG onto [0,100); +1 shifts to 1..100. Modulo
					// bias is negligible and acceptable for a chat game.
					roll := int(randInt63()%100) + 1
					if roll <= gambleWinChance {
						payout := bet * 2
						profit := payout - bet // net winnings on a double == the stake
						win := func(newBal int64) string {
							return fmt.Sprintf("🎲 %srolled %d and WON %s %s! Balance: %s",
								mentionPrefix(msg), roll, formatPoints(profit), pointsName, formatPoints(newBal))
						}
						return payout, win, nil
					}
					loss := func(newBal int64) string {
						return fmt.Sprintf("🎲 %srolled %d and lost %s %s. Balance: %s",
							mentionPrefix(msg), roll, formatPoints(bet), pointsName, formatPoints(newBal))
					}
					return 0, nil, loss
				})
		},
	}
}

// NewSlotsCommand returns "!slots" (MinRole RoleEveryone, 5s per-user
// cooldown). "!slots <amount|all|50%>" spins three reels drawn from
// [slotSymbols]; the payout table is documented in [slotsPayout]. Like
// !gamble, the command decides the payout with its own RNG and then settles
// atomically via [GameBank.Wager]. A nil bank yields an "unavailable" reply.
func NewSlotsCommand(bank GameBank) Command { return newSlotsCommand(bank, rand.Int63) }

// newSlotsCommand is the injectable form: randInt63 is called once per reel
// (three times per spin), each call indexing [slotSymbols]. Tests inject a
// sequential stub for exact reels.
func newSlotsCommand(bank GameBank, randInt63 func() int64) Command {
	return Command{
		Name:         "slots",
		Help:         "Spin the slot machine — !slots <amount|all|50%>.",
		UserCooldown: gameUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			return playWager(ctx, bank, msg, args, "slots", "spin",
				func(bet int64) (int64, func(int64) string, func(int64) string) {
					// Modulo maps each RNG draw onto a reel index. Modulo bias is
					// acceptable for a chat game.
					spin := func() string {
						return slotSymbols[int(randInt63()%int64(len(slotSymbols)))]
					}
					a := spin()
					b := spin()
					c := spin()
					reels := fmt.Sprintf("[%s | %s | %s]", a, b, c)
					payout := slotsPayout(a, b, c, bet)
					if payout > 0 {
						profit := payout - bet
						win := func(newBal int64) string {
							return fmt.Sprintf("🎰 %sspun %s — WON %s %s! Balance: %s",
								mentionPrefix(msg), reels, formatPoints(profit), pointsName, formatPoints(newBal))
						}
						return payout, win, nil
					}
					loss := func(newBal int64) string {
						return fmt.Sprintf("🎰 %sspun %s — no win, lost %s. Balance: %s",
							mentionPrefix(msg), reels, formatPoints(bet), formatPoints(newBal))
					}
					return 0, nil, loss
				})
		},
	}
}

// slotsPayout returns the credited amount (stake included) for a three-reel
// spin. House-edge-tuned payout table:
//
//	three 7️⃣           → bet*10 (jackpot)
//	three 💎           → bet*7
//	three of any other → bet*4
//	exactly two match  → bet*2 (small win)
//	otherwise          → 0     (loss)
//
// Three-of-a-kind is checked first, so the two-match branch means exactly two.
func slotsPayout(a, b, c string, bet int64) int64 {
	if a == b && b == c {
		switch a {
		case "7️⃣":
			return bet * 10
		case "💎":
			return bet * 7
		default:
			return bet * 4
		}
	}
	if a == b || b == c || a == c {
		return bet * 2
	}
	return 0
}

// playWager runs the shared gambling flow: nil-bank guard, usage check, a
// single balance lookup (used to resolve "all"/"%" and as the parse anchor),
// bet parsing, then settlement via [GameBank.Wager]. decide picks the payout
// from the bet (using the caller's RNG) and returns win/loss reply builders;
// only the one matching the outcome (payout>0 ⇒ win) is invoked, with the
// post-settlement balance. name fills the usage line ("!<name> ..."); verb
// fills the insufficient-funds line ("...to <verb>").
func playWager(
	ctx context.Context,
	bank GameBank,
	msg Message,
	args []string,
	name, verb string,
	decide func(bet int64) (payout int64, win func(int64) string, loss func(int64) string),
) Reply {
	if bank == nil {
		return Reply{Text: pointsName + " are unavailable"}
	}
	usage := fmt.Sprintf("%susage: !%s <amount|all|50%%>", mentionPrefix(msg), name)
	if len(args) == 0 {
		return Reply{Text: usage}
	}
	bal, status := bank.Balance(ctx, msg.Channel, msg.UserID)
	switch status {
	case LoyaltyOK:
		// proceed
	case LoyaltyNotFound:
		return Reply{Text: fmt.Sprintf("%syou don't have any %s yet", mentionPrefix(msg), pointsName)}
	default:
		return Reply{Text: "couldn't check that right now"}
	}
	bet, ok := parseBet(args[0], bal)
	if !ok {
		return Reply{Text: usage}
	}
	payout, win, loss := decide(bet)
	newBal, wstatus := bank.Wager(ctx, msg.Channel, msg.UserID, bet, payout)
	switch wstatus {
	case LoyaltyOK:
		if payout > 0 {
			return Reply{Text: win(newBal)}
		}
		return Reply{Text: loss(newBal)}
	case LoyaltyInsufficient:
		return Reply{Text: fmt.Sprintf("%syou don't have %s %s to %s",
			mentionPrefix(msg), formatPoints(bet), pointsName, verb)}
	case LoyaltyNotFound:
		return Reply{Text: fmt.Sprintf("%syou don't have any %s yet", mentionPrefix(msg), pointsName)}
	default:
		return Reply{Text: "couldn't place that bet right now"}
	}
}

// parseBet resolves a bet argument against the player's balance. It accepts
// "all"/"max" (the whole balance), an integer percent like "50%" (floored,
// 1..100), or a plain positive integer. ok is false for any non-positive or
// malformed result (including "all"/"%" when the balance is 0).
func parseBet(arg string, balance int64) (int64, bool) {
	s := strings.TrimSpace(arg)
	switch strings.ToLower(s) {
	case "all", "max":
		if balance > 0 {
			return balance, true
		}
		return 0, false
	}
	if strings.HasSuffix(s, "%") {
		pct, err := strconv.ParseInt(strings.TrimSuffix(s, "%"), 10, 64)
		if err != nil || pct < 1 || pct > 100 {
			return 0, false
		}
		bet := balance * pct / 100
		if bet > 0 {
			return bet, true
		}
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

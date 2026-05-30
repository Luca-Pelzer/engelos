package commands

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// duelChallengeUserCooldown throttles per-user spam of !duel. 10s is long
// enough to stop a viewer machine-gunning challenges at the whole chat.
const duelChallengeUserCooldown = 10 * time.Second

// duelAcceptUserCooldown throttles per-user spam of !accept. 3s mirrors the
// other low-friction loyalty commands.
const duelAcceptUserCooldown = 3 * time.Second

// duelTTL is how long a pending challenge stays acceptable before it expires.
const duelTTL = 60 * time.Second

// DuelBank settles a player-vs-player wager. Both players staked `amount`;
// Settle must take `amount` from EACH (spend) and award the whole pot
// (amount*2) to the winner. It returns the winner's new balance. The
// implementation spends both stakes BEFORE crediting the pot so no one goes
// negative. status reports the first blocking problem (e.g. a player who can no
// longer afford the stake by the time the duel is accepted).
type DuelBank interface {
	// CanAfford reports whether a viewer currently has at least `amount`.
	CanAfford(ctx context.Context, channel, viewerID string, amount int64) bool
	// Balance returns a viewer's balance (for all/% bet parsing).
	Balance(ctx context.Context, channel, viewerID string) (int64, LoyaltyError)
	// Settle takes `amount` from both players and gives the pot (amount*2) to
	// winnerID. Returns the winner's post-settlement balance, or a non-OK
	// status if either stake could not be taken (in which case NO money moves —
	// the implementation must verify both can afford BEFORE spending).
	Settle(ctx context.Context, channel, winnerID, loserID string, amount int64) (winnerBalance int64, status LoyaltyError)
}

// challenge is one pending duel: a challenger waiting for a specific target to
// !accept. targetID may be "" if only the login was known at challenge time;
// targetLogin is the lower-cased login the accepter's username must match.
type challenge struct {
	challengerID, challengerName string
	targetID, targetName         string
	targetLogin                  string
	amount                       int64
	createdAt                    time.Time
}

// duelRegistry tracks pending challenges in memory, keyed by channel then
// target login. It is concurrency-safe: every read/write takes mu.
type duelRegistry struct {
	mu     sync.Mutex
	byChan map[string]map[string]*challenge
	ttl    time.Duration
	now    func() time.Time
}

// newDuelRegistry returns an empty registry whose challenges expire after ttl.
func newDuelRegistry(ttl time.Duration) *duelRegistry {
	return &duelRegistry{
		byChan: make(map[string]map[string]*challenge),
		ttl:    ttl,
		now:    time.Now,
	}
}

// NewDuelGame builds the !duel and !accept commands sharing a single challenge
// registry (challenges expire after 60s). It is the exported entry point for
// wiring both commands, since the registry type is package-private.
func NewDuelGame(bank DuelBank) (duel Command, accept Command) {
	reg := newDuelRegistry(60 * time.Second)
	return NewDuelCommand(bank, reg), NewAcceptCommand(bank, reg)
}

// withClock overrides the registry's time source (tests inject a fake clock).
func (r *duelRegistry) withClock(now func() time.Time) *duelRegistry {
	r.now = now
	return r
}

// add stores a challenge keyed by (channel, targetLogin). It returns false if
// there is ALREADY a live (un-expired) challenge for that target in that
// channel; an expired entry is overwritten.
func (r *duelRegistry) add(channel string, c *challenge) bool {
	ch := strings.ToLower(strings.TrimSpace(channel))
	login := strings.ToLower(strings.TrimSpace(c.targetLogin))
	c.targetLogin = login

	r.mu.Lock()
	defer r.mu.Unlock()

	targets := r.byChan[ch]
	if targets == nil {
		targets = make(map[string]*challenge)
		r.byChan[ch] = targets
	}
	if existing, ok := targets[login]; ok && !r.expired(existing) {
		return false
	}
	targets[login] = c
	return true
}

// take atomically finds+removes a live challenge for (channel, accepterLogin)
// and returns it; ok is false when none exists or it has expired. An expired
// challenge is purged so a stale one can never be accepted.
func (r *duelRegistry) take(channel, accepterLogin string) (*challenge, bool) {
	ch := strings.ToLower(strings.TrimSpace(channel))
	login := strings.ToLower(strings.TrimSpace(accepterLogin))

	r.mu.Lock()
	defer r.mu.Unlock()

	targets := r.byChan[ch]
	if targets == nil {
		return nil, false
	}
	c, ok := targets[login]
	if !ok {
		return nil, false
	}
	delete(targets, login)
	if r.expired(c) {
		return nil, false
	}
	return c, true
}

// expired reports whether c has outlived the registry TTL. Caller holds mu.
func (r *duelRegistry) expired(c *challenge) bool {
	return r.now().Sub(c.createdAt) >= r.ttl
}

// NewDuelCommand returns "!duel" (MinRole RoleEveryone, 10s per-user cooldown).
// "!duel <user> <amount|all|50%>" challenges another viewer to a winner-take-all
// wager: no money moves yet, it just records a pending challenge the target can
// !accept within the TTL. The caller must currently afford the stake. A nil
// bank or registry yields an "unavailable" reply.
func NewDuelCommand(bank DuelBank, reg *duelRegistry) Command {
	return Command{
		Name:         "duel",
		Help:         "Challenge another viewer to wager " + pointsName + " — !duel <user> <amount|all|50%>.",
		UserCooldown: duelChallengeUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if bank == nil || reg == nil {
				return Reply{Text: pointsName + " are unavailable"}
			}
			if len(args) < 2 {
				return Reply{Text: fmt.Sprintf("%susage: !duel <user> <amount>", mentionPrefix(msg))}
			}
			target := strings.TrimPrefix(strings.TrimSpace(args[0]), "@")
			if target == "" {
				return Reply{Text: fmt.Sprintf("%susage: !duel <user> <amount>", mentionPrefix(msg))}
			}
			if strings.EqualFold(target, msg.Username) {
				return Reply{Text: fmt.Sprintf("%syou can't duel yourself", mentionPrefix(msg))}
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
			bet, ok := parseBet(args[1], bal)
			if !ok {
				return Reply{Text: fmt.Sprintf("%susage: !duel <user> <amount>", mentionPrefix(msg))}
			}
			if !bank.CanAfford(ctx, msg.Channel, msg.UserID, bet) {
				return Reply{Text: fmt.Sprintf("%syou don't have %s %s to duel",
					mentionPrefix(msg), formatPoints(bet), pointsName)}
			}
			c := &challenge{
				challengerID:   msg.UserID,
				challengerName: msg.Username,
				targetName:     target,
				targetLogin:    strings.ToLower(target),
				amount:         bet,
				createdAt:      reg.now(),
			}
			if !reg.add(msg.Channel, c) {
				return Reply{Text: fmt.Sprintf("%sthere's already a pending duel for %s",
					mentionPrefix(msg), target)}
			}
			return Reply{Text: fmt.Sprintf("⚔️ %schallenges %s to a %s-%s duel! %s, type !accept within 60s.",
				mentionPrefix(msg), target, formatPoints(bet), pointsName, target)}
		},
	}
}

// NewAcceptCommand returns "!accept" (MinRole RoleEveryone, 3s per-user
// cooldown). "!accept" takes the pending duel aimed at the caller and resolves
// it: it re-checks that BOTH players can still afford the stake, flips a fair
// coin, and settles the pot to the winner via [DuelBank.Settle]. A nil bank or
// registry yields an "unavailable" reply.
func NewAcceptCommand(bank DuelBank, reg *duelRegistry) Command {
	return newAcceptCommand(bank, reg, rand.Int63)
}

// newAcceptCommand is the injectable form: randInt63 supplies the coin flip.
// Tests inject a stub for a deterministic winner.
func newAcceptCommand(bank DuelBank, reg *duelRegistry, randInt63 func() int64) Command {
	return Command{
		Name:         "accept",
		Help:         "Accept a pending duel challenge — !accept.",
		UserCooldown: duelAcceptUserCooldown,
		Handler: func(ctx context.Context, msg Message, _ []string) Reply {
			if bank == nil || reg == nil {
				return Reply{Text: pointsName + " are unavailable"}
			}
			c, ok := reg.take(msg.Channel, msg.Username)
			if !ok {
				return Reply{Text: fmt.Sprintf("%syou have no pending duel to accept.", mentionPrefix(msg))}
			}
			// The challenger may have spent points between !duel and !accept, so
			// re-verify both stakes before any settlement.
			if !bank.CanAfford(ctx, msg.Channel, c.challengerID, c.amount) {
				return Reply{Text: fmt.Sprintf("%s%s can no longer cover the duel.",
					mentionPrefix(msg), c.challengerName)}
			}
			if !bank.CanAfford(ctx, msg.Channel, msg.UserID, c.amount) {
				return Reply{Text: fmt.Sprintf("%syou don't have %s %s to accept.",
					mentionPrefix(msg), formatPoints(c.amount), pointsName)}
			}
			// Fair coin: modulo 2 of the RNG draw (0 = challenger wins, 1 =
			// accepter wins). Modulo bias on two outcomes is nil.
			coin := randInt63() % 2
			winnerID, winnerName := c.challengerID, c.challengerName
			loserID, loserName := msg.UserID, msg.Username
			if coin == 1 {
				winnerID, winnerName = msg.UserID, msg.Username
				loserID, loserName = c.challengerID, c.challengerName
			}
			_, status := bank.Settle(ctx, msg.Channel, winnerID, loserID, c.amount)
			switch status {
			case LoyaltyOK:
				pot := c.amount * 2
				return Reply{Text: fmt.Sprintf("⚔️ %s beat %s and won the %s-%s pot! 🏆",
					winnerName, loserName, formatPoints(pot), pointsName)}
			case LoyaltyInsufficient:
				return Reply{Text: fmt.Sprintf("%sthe duel fell through — someone couldn't cover it.",
					mentionPrefix(msg))}
			default:
				return Reply{Text: "couldn't settle the duel right now."}
			}
		},
	}
}

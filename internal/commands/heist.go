package commands

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// heistJoinWindow is the default duration the crew can join after the first
// player starts a heist. 45s is long enough for chat to react, short enough to
// keep the lobby snappy.
const heistJoinWindow = 45 * time.Second

// heistUserCooldown throttles per-user spam of !heist. 5s mirrors the other
// low-friction gambling commands.
const heistUserCooldown = 5 * time.Second

// heistMaxNamedSurvivors caps how many survivor names are listed in the
// outcome announcement before it summarises by count, keeping the message well
// under the chat length limit.
const heistMaxNamedSurvivors = 6

// HeistBank moves loyalty points for the heist. Collect takes a player's stake
// up front (when they join); Payout credits a surviving player's winnings after
// the heist resolves. Both are atomic in the implementation.
type HeistBank interface {
	Balance(ctx context.Context, channel, viewerID string) (int64, LoyaltyError)
	// Collect spends `amount` from the player as their heist stake. Returns ok
	// only if it was actually taken (false on insufficient/no-account/error).
	Collect(ctx context.Context, channel, viewerID string, amount int64) bool
	// Payout credits `amount` to a surviving player.
	Payout(ctx context.Context, channel, viewerID string, amount int64)
}

// HeistSender announces the heist outcome to chat after the join window (the
// game resolves asynchronously, so it cannot use the command's Reply).
type HeistSender interface {
	Send(ctx context.Context, channel, message string) error
}

// joinResult enumerates the outcomes of a player attempting to join/start a
// heist via [heistManager.join].
type joinResult int

const (
	// JoinStarted means this player opened a brand-new heist lobby.
	JoinStarted joinResult = iota
	// JoinedExisting means this player joined a heist already in progress.
	JoinedExisting
	// JoinDup means this player is already part of the live heist.
	JoinDup
	// JoinPoor means the player could not afford the stake (Collect failed).
	JoinPoor
)

// joinOutcome is what [heistManager.join] reports back to the command: the
// result, the current crew size, and the join window (for the reply text).
type joinOutcome struct {
	result joinResult
	count  int
	window time.Duration
}

// heistPlayer is one member of a heist crew and the stake they put in.
type heistPlayer struct {
	viewerID, username string
	stake              int64
}

// heist is one in-progress lobby: the crew, a dedup set keyed by viewerID, the
// start time, and whether it has already been resolved.
type heist struct {
	players   []heistPlayer
	byID      map[string]bool
	startedAt time.Time
	resolved  bool
}

// heistManager owns one heist lobby per channel and drives the asynchronous
// resolution. It is concurrency-safe: every lobby read/write takes mu.
type heistManager struct {
	mu       sync.Mutex
	byChan   map[string]*heist
	window   time.Duration
	now      func() time.Time
	randIntN func(n int) int
	bank     HeistBank
	sender   HeistSender
	logger   *slog.Logger
	// afterFn schedules resolution after the join window. It is a seam so tests
	// can drive resolution synchronously instead of waiting on a real timer.
	afterFn func(d time.Duration, f func())
}

// newHeistManager builds a manager wired to bank/sender with production
// defaults: a 45s window, the wall clock, math/rand for the survival roll, and
// a time.AfterFunc-based timer for resolution.
func newHeistManager(bank HeistBank, sender HeistSender) *heistManager {
	return &heistManager{
		byChan:   make(map[string]*heist),
		window:   heistJoinWindow,
		now:      time.Now,
		randIntN: rand.Intn,
		bank:     bank,
		sender:   sender,
		logger:   slog.Default(),
		// Default seam: a real timer fires resolution on its own goroutine.
		afterFn: func(d time.Duration, f func()) { time.AfterFunc(d, f) },
	}
}

// join adds (channel, viewerID) to the channel's heist, starting a new one if
// none is live. Starting collects the stake and schedules resolution via
// afterFn(window). Returns a joinOutcome describing the result, the crew size,
// and the window. JoinPoor means Collect refused the stake; JoinDup means the
// player is already in the live heist (no second Collect).
func (m *heistManager) join(ctx context.Context, channel, viewerID, username string, amount int64) joinOutcome {
	m.mu.Lock()

	h := m.byChan[channel]
	if h == nil || h.resolved {
		// No live heist: try to take this player's stake before opening one.
		if !m.bank.Collect(ctx, channel, viewerID, amount) {
			m.mu.Unlock()
			return joinOutcome{result: JoinPoor, window: m.window}
		}
		nh := &heist{
			players:   []heistPlayer{{viewerID: viewerID, username: username, stake: amount}},
			byID:      map[string]bool{viewerID: true},
			startedAt: m.now(),
		}
		m.byChan[channel] = nh
		window := m.window
		m.mu.Unlock()
		// Schedule resolution outside the lock; afterFn must not re-enter join.
		m.afterFn(window, func() { m.resolve(channel) })
		return joinOutcome{result: JoinStarted, count: 1, window: window}
	}

	if h.byID[viewerID] {
		count := len(h.players)
		m.mu.Unlock()
		return joinOutcome{result: JoinDup, count: count, window: m.window}
	}

	if !m.bank.Collect(ctx, channel, viewerID, amount) {
		m.mu.Unlock()
		return joinOutcome{result: JoinPoor, window: m.window}
	}
	h.players = append(h.players, heistPlayer{viewerID: viewerID, username: username, stake: amount})
	h.byID[viewerID] = true
	count := len(h.players)
	m.mu.Unlock()
	return joinOutcome{result: JoinedExisting, count: count, window: m.window}
}

// resolve runs the heist for channel once the join window elapses. It snapshots
// the crew, marks the lobby resolved, and removes it — all under the lock —
// then releases the lock BEFORE any Payout/Send I/O so the mutex is never held
// across a blocking call. Each player independently survives on a 50% roll;
// survivors get their stake doubled (Payout = stake*2).
func (m *heistManager) resolve(channel string) {
	m.mu.Lock()
	h := m.byChan[channel]
	if h == nil || h.resolved {
		m.mu.Unlock()
		return
	}
	h.resolved = true
	delete(m.byChan, channel)
	players := make([]heistPlayer, len(h.players))
	copy(players, h.players)
	m.mu.Unlock()

	// From here on we hold no lock: Payout and Send may block on I/O.
	var survivors, caught []string
	for _, p := range players {
		// 50% survival: randIntN(2)==0 survives, ==1 is caught. Even split, no
		// modulo bias on two outcomes.
		if m.randIntN(2) == 0 {
			m.bank.Payout(context.Background(), channel, p.viewerID, p.stake*2)
			survivors = append(survivors, p.username)
		} else {
			caught = append(caught, p.username)
		}
	}

	msg := heistOutcomeMessage(survivors, caught)
	if err := m.sender.Send(context.Background(), channel, msg); err != nil {
		m.logger.Error("heist: failed to announce outcome",
			"channel", channel,
			"error", err,
		)
	}
}

// heistOutcomeMessage builds the single (<400 char) announcement. With no
// survivors it reports a total wipe. Otherwise it lists survivor names when the
// crew is small (<= heistMaxNamedSurvivors) and summarises by count when large,
// always naming who got caught (or "none").
func heistOutcomeMessage(survivors, caught []string) string {
	if len(survivors) == 0 {
		return "💀 The heist failed! Everyone got caught and lost their loot."
	}
	var survivorPart string
	if len(survivors) <= heistMaxNamedSurvivors {
		survivorPart = strings.Join(survivors, ", ")
	} else {
		survivorPart = fmt.Sprintf("%d survivors split the loot", len(survivors))
	}
	caughtPart := "none"
	if len(caught) > 0 {
		if len(caught) <= heistMaxNamedSurvivors {
			caughtPart = strings.Join(caught, ", ")
		} else {
			caughtPart = fmt.Sprintf("%d crew", len(caught))
		}
	}
	return fmt.Sprintf("💰 The heist is done! Survivors: %s each doubled their stake. Caught: %s.",
		survivorPart, caughtPart)
}

// NewHeistGame builds the !heist command backed by a manager that uses bank to
// move points and sender to announce the asynchronous result. The heist
// resolves on a background timer after the join window, so the outcome is sent
// via sender (not the command's Reply).
func NewHeistGame(bank HeistBank, sender HeistSender) Command {
	m := newHeistManager(bank, sender)
	return newHeistCommand(m)
}

// newHeistCommand returns the "!heist" command bound to manager m. It is the
// injectable form so tests can supply a manager with stubbed seams.
func newHeistCommand(m *heistManager) Command {
	return Command{
		Name:         "heist",
		Help:         "Start or join a group heist — !heist <amount|all|50%>.",
		MinRole:      RoleEveryone,
		UserCooldown: heistUserCooldown,
		Handler: func(ctx context.Context, msg Message, args []string) Reply {
			if m == nil || m.bank == nil || m.sender == nil {
				return Reply{Text: "heists are unavailable"}
			}
			if len(args) == 0 {
				return Reply{Text: fmt.Sprintf("%susage: !heist <amount>", mentionPrefix(msg))}
			}
			bal, status := m.bank.Balance(ctx, msg.Channel, msg.UserID)
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
				return Reply{Text: fmt.Sprintf("%susage: !heist <amount>", mentionPrefix(msg))}
			}
			out := m.join(ctx, msg.Channel, msg.UserID, msg.Username, bet)
			switch out.result {
			case JoinStarted:
				return Reply{Text: fmt.Sprintf(
					"🏦 %sis starting a heist for %s %s! Type !heist <amount> in the next %ds to join the crew.",
					mentionPrefix(msg), formatPoints(bet), pointsName, int(out.window/time.Second))}
			case JoinedExisting:
				return Reply{Text: fmt.Sprintf("💼 %sjoined the heist! (%d in the crew)",
					mentionPrefix(msg), out.count)}
			case JoinDup:
				return Reply{Text: fmt.Sprintf("%syou're already in this heist.", mentionPrefix(msg))}
			default: // JoinPoor
				return Reply{Text: fmt.Sprintf("%syou don't have %s %s to join.",
					mentionPrefix(msg), formatPoints(bet), pointsName)}
			}
		},
	}
}

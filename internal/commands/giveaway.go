package commands

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// enterUserCooldown throttles per-user !enter spam. 2s is friction enough to
// stop a viewer machine-gunning the command while still feeling instant.
const enterUserCooldown = 2 * time.Second

// enterResult is the outcome of an [giveawayManager.enter] attempt. It is an
// enum (not a bool/error pair) so the command layer can map each distinct
// state to its own chat reply without re-deriving the cause.
type enterResult int

const (
	// EnterOK means the viewer was newly added to the open giveaway.
	EnterOK enterResult = iota
	// EnterDup means the viewer had already entered this giveaway.
	EnterDup
	// EnterNoGiveaway means no giveaway exists in the channel.
	EnterNoGiveaway
	// EnterClosed means a giveaway exists but entries are closed.
	EnterClosed
)

// giveaway is one keyword giveaway in a single channel. Entries dedup by
// viewerID; `order` preserves entry order for display, while the draw itself
// sorts eligible IDs for a stable, reproducible result. `seed` is fixed when
// the giveaway OPENS and announced so viewers can later verify the draw.
type giveaway struct {
	keyword     string
	open        bool
	entrants    map[string]string // viewerID -> username (one entry per viewer)
	order       []string          // viewerIDs in entry order
	pastWinners map[string]bool   // viewerIDs already drawn (excluded from reroll)
	seed        int64             // provably-fair seed, fixed at open time
	drawCount   int               // number of draws so far; feeds the draw hash
	createdAt   time.Time
}

// giveawayManager holds at most one giveaway per channel, in memory. It is
// concurrency-safe: every read/write takes mu. `now` and `seedFn` are
// injectable seams so tests can pin the clock and the provably-fair seed.
type giveawayManager struct {
	mu     sync.Mutex
	byChan map[string]*giveaway
	now    func() time.Time
	seedFn func() int64
}

// newGiveawayManager returns an empty manager with a wall clock and a
// time-derived seed source (UnixNano at open time).
func newGiveawayManager() *giveawayManager {
	return &giveawayManager{
		byChan: make(map[string]*giveaway),
		now:    time.Now,
		seedFn: func() int64 { return time.Now().UnixNano() },
	}
}

// withClock overrides the manager's time source (tests inject a fake clock).
func (m *giveawayManager) withClock(now func() time.Time) *giveawayManager {
	m.now = now
	return m
}

// withSeed overrides the manager's seed source (tests inject a fixed seed so
// the provably-fair draw is exactly reproducible).
func (m *giveawayManager) withSeed(seedFn func() int64) *giveawayManager {
	m.seedFn = seedFn
	return m
}

// chanKey normalises a channel name to its map key (trimmed, lower-cased).
func chanKey(channel string) string {
	return strings.ToLower(strings.TrimSpace(channel))
}

// open starts a fresh OPEN giveaway in channel with a newly-drawn seed and
// returns that seed. ok is false when a giveaway is already present in the
// channel - the caller must cancel or draw the existing one first.
func (m *giveawayManager) open(channel, keyword string) (seed int64, ok bool) {
	ch := chanKey(channel)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byChan[ch]; exists {
		return 0, false
	}
	seed = m.seedFn()
	m.byChan[ch] = &giveaway{
		keyword:     keyword,
		open:        true,
		entrants:    make(map[string]string),
		pastWinners: make(map[string]bool),
		seed:        seed,
		createdAt:   m.now(),
	}
	return seed, true
}

// enter adds (viewerID, username) to the channel's open giveaway. The returned
// [enterResult] distinguishes a fresh entry, a duplicate, no giveaway, and a
// closed-for-entries giveaway.
func (m *giveawayManager) enter(channel, viewerID, username string) enterResult {
	ch := chanKey(channel)
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.byChan[ch]
	if !ok {
		return EnterNoGiveaway
	}
	if !g.open {
		return EnterClosed
	}
	if _, dup := g.entrants[viewerID]; dup {
		return EnterDup
	}
	g.entrants[viewerID] = username
	g.order = append(g.order, viewerID)
	return EnterOK
}

// close stops new entries on the channel's giveaway but keeps it (so a draw can
// still run). It returns the current entrant count. ok is false if no giveaway
// exists in the channel.
func (m *giveawayManager) close(channel string) (count int, ok bool) {
	ch := chanKey(channel)
	m.mu.Lock()
	defer m.mu.Unlock()
	g, exists := m.byChan[ch]
	if !exists {
		return 0, false
	}
	g.open = false
	return len(g.entrants), true
}

// draw selects a provably-fair winner from the entrants NOT already drawn,
// marks them a past winner, and advances the draw counter so a subsequent
// reroll resolves to a different deterministic pick. ok is false when there is
// no giveaway or no eligible entrant remains. A draw works on an open OR a
// closed giveaway - closing only stops new !enter.
func (m *giveawayManager) draw(channel string) (winnerID, winnerName string, seed int64, ok bool) {
	ch := chanKey(channel)
	m.mu.Lock()
	defer m.mu.Unlock()
	g, exists := m.byChan[ch]
	if !exists {
		return "", "", 0, false
	}
	eligible := make([]string, 0, len(g.entrants))
	for id := range g.entrants {
		if !g.pastWinners[id] {
			eligible = append(eligible, id)
		}
	}
	if len(eligible) == 0 {
		return "", "", 0, false
	}
	// Sort so the index returned by provablyFairIndex (computed over the same
	// sorted order) addresses the correct entrant.
	sort.Strings(eligible)
	idx := provablyFairIndex(g.seed, g.drawCount, eligible)
	winnerID = eligible[idx]
	winnerName = g.entrants[winnerID]
	g.pastWinners[winnerID] = true
	g.drawCount++
	return winnerID, winnerName, g.seed, true
}

// cancel removes the channel's giveaway entirely. It returns false if there was
// nothing to cancel.
func (m *giveawayManager) cancel(channel string) bool {
	ch := chanKey(channel)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byChan[ch]; !exists {
		return false
	}
	delete(m.byChan, ch)
	return true
}

// status reports the channel giveaway's keyword, open state, and entrant count
// for display. ok is false when no giveaway exists in the channel.
func (m *giveawayManager) status(channel string) (keyword string, open bool, count int, ok bool) {
	ch := chanKey(channel)
	m.mu.Lock()
	defer m.mu.Unlock()
	g, exists := m.byChan[ch]
	if !exists {
		return "", false, 0, false
	}
	return g.keyword, g.open, len(g.entrants), true
}

// provablyFairIndex deterministically maps (seed, drawNumber, entrant set) to a
// winner index. It is the heart of the giveaway's fairness guarantee.
//
// Provably-fair: winner index = H(seed || drawNumber || joinedSortedEntrantIDs) mod N.
// Because the seed is published at open time and the entrant set is observable,
// anyone can recompute the winner - the streamer cannot secretly bias it.
//
// The eligible IDs are sorted before hashing so the result depends only on the
// SET of entrants (not their map iteration order), and drawNumber increments
// per draw so a reroll deterministically picks a different winner. The index is
// the first 8 bytes of the SHA-256 digest read big-endian, taken mod N.
func provablyFairIndex(seed int64, drawNumber int, eligible []string) int {
	sorted := make([]string, len(eligible))
	copy(sorted, eligible)
	sort.Strings(sorted)
	payload := fmt.Sprintf("%d|%d|%s", seed, drawNumber, strings.Join(sorted, ","))
	sum := sha256.Sum256([]byte(payload))
	n := binary.BigEndian.Uint64(sum[:8])
	return int(n % uint64(len(sorted)))
}

// NewGiveawayCommands returns the four giveaway commands - !giveaway, !enter,
// !draw, and !reroll - all sharing one in-memory [giveawayManager]. It is the
// exported wiring entry point, since the manager type is package-private.
//
// The giveaway is a free, keyword-labelled raffle: a moderator opens it with a
// keyword and an announced seed, viewers join with !enter, and the moderator
// draws a provably-fair winner anyone can verify from the seed and entrant set.
func NewGiveawayCommands() []Command {
	m := newGiveawayManager()
	return []Command{
		newGiveawayCommand(m),
		newEnterCommand(m),
		newDrawCommand(m),
		newRerollCommand(m),
	}
}

// newGiveawayCommand builds "!giveaway <keyword>" (MinRole RoleModerator): it
// opens a giveaway, or runs the cancel/close/status subcommands. Kept
// unexported so tests can inject a manager with a pinned clock/seed.
func newGiveawayCommand(m *giveawayManager) Command {
	return Command{
		Name:    "giveaway",
		Help:    "Open a keyword giveaway - !giveaway <keyword> | cancel | close | status.",
		MinRole: RoleModerator,
		Handler: func(_ context.Context, msg Message, args []string) Reply {
			if len(args) > 0 {
				switch strings.ToLower(args[0]) {
				case "cancel":
					if !m.cancel(msg.Channel) {
						return Reply{Text: fmt.Sprintf("%sthere's no giveaway to cancel.", mentionPrefix(msg))}
					}
					return Reply{Text: "Giveaway cancelled."}
				case "close":
					count, ok := m.close(msg.Channel)
					if !ok {
						return Reply{Text: fmt.Sprintf("%sthere's no giveaway to close.", mentionPrefix(msg))}
					}
					return Reply{Text: fmt.Sprintf("Entries closed - %d entrants. Use !draw.", count)}
				case "status":
					kw, open, count, ok := m.status(msg.Channel)
					if !ok {
						return Reply{Text: fmt.Sprintf("%sthere's no giveaway running right now.", mentionPrefix(msg))}
					}
					state := "open"
					if !open {
						state = "closed"
					}
					return Reply{Text: fmt.Sprintf("Giveaway \"%s\" is %s - %d entrants.", kw, state, count)}
				}
			}
			keyword := strings.TrimSpace(strings.Join(args, " "))
			if keyword == "" {
				return Reply{Text: fmt.Sprintf("%susage: !giveaway <keyword>", mentionPrefix(msg))}
			}
			seed, ok := m.open(msg.Channel, keyword)
			if !ok {
				return Reply{Text: fmt.Sprintf("%sa giveaway is already running - !draw or !giveaway cancel first.", mentionPrefix(msg))}
			}
			return Reply{Text: fmt.Sprintf("🎉 Giveaway open! Type !enter to join. (keyword: %s, seed: %d)", keyword, seed)}
		},
	}
}

// newEnterCommand builds "!enter" (MinRole RoleEveryone, 2s per-user cooldown):
// it joins the channel's open giveaway, replying per [enterResult].
func newEnterCommand(m *giveawayManager) Command {
	return Command{
		Name:         "enter",
		Help:         "Enter the running giveaway - !enter.",
		UserCooldown: enterUserCooldown,
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			switch m.enter(msg.Channel, msg.UserID, msg.Username) {
			case EnterOK:
				return Reply{Text: fmt.Sprintf("%syou're in! 🎟️", mentionPrefix(msg))}
			case EnterDup:
				return Reply{Text: fmt.Sprintf("%syou're already entered.", mentionPrefix(msg))}
			case EnterClosed:
				return Reply{Text: fmt.Sprintf("%sentries are closed for this giveaway.", mentionPrefix(msg))}
			default: // EnterNoGiveaway
				return Reply{Text: fmt.Sprintf("%sthere's no giveaway running right now.", mentionPrefix(msg))}
			}
		},
	}
}

// newDrawCommand builds "!draw" (MinRole RoleModerator): it draws a
// provably-fair winner from the entrants not yet drawn.
func newDrawCommand(m *giveawayManager) Command {
	return Command{
		Name:    "draw",
		Help:    "Draw a provably-fair giveaway winner - !draw.",
		MinRole: RoleModerator,
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			_, winnerName, seed, ok := m.draw(msg.Channel)
			if !ok {
				return Reply{Text: fmt.Sprintf("%sno eligible entrants to draw.", mentionPrefix(msg))}
			}
			return Reply{Text: fmt.Sprintf("🏆 The winner is @%s! (verifiable with seed %d)", winnerName, seed)}
		},
	}
}

// newRerollCommand builds "!reroll" (MinRole RoleModerator): another draw that
// excludes past winners, so it always picks someone new (or reports none left).
func newRerollCommand(m *giveawayManager) Command {
	return Command{
		Name:    "reroll",
		Help:    "Draw another winner, excluding past winners - !reroll.",
		MinRole: RoleModerator,
		Handler: func(_ context.Context, msg Message, _ []string) Reply {
			_, winnerName, seed, ok := m.draw(msg.Channel)
			if !ok {
				return Reply{Text: fmt.Sprintf("%sno more eligible entrants to reroll.", mentionPrefix(msg))}
			}
			return Reply{Text: fmt.Sprintf("🏆 The winner is @%s! (verifiable with seed %d)", winnerName, seed)}
		},
	}
}

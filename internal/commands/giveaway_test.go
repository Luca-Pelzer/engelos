package commands

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedGiveawayClock is a frozen instant used by every giveaway test so the
// createdAt timestamp is deterministic and never depends on wall time.
var fixedGiveawayClock = time.Unix(1_700_000_000, 0)

// newTestManager builds a manager with a pinned clock and a fixed seed (42) so
// the provably-fair draw is exactly reproducible across runs.
func newTestManager() *giveawayManager {
	return newGiveawayManager().
		withClock(func() time.Time { return fixedGiveawayClock }).
		withSeed(func() int64 { return 42 })
}

// --- manager: open / enter -------------------------------------------------

func TestGiveaway_OpenThenEnterOK(t *testing.T) {
	m := newTestManager()
	seed, ok := m.open("chan", "win")
	require.True(t, ok)
	assert.Equal(t, int64(42), seed)

	assert.Equal(t, EnterOK, m.enter("chan", "u1", "alice"))
	_, _, count, ok := m.status("chan")
	require.True(t, ok)
	assert.Equal(t, 1, count)
}

func TestGiveaway_DuplicateEnter(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)

	assert.Equal(t, EnterOK, m.enter("chan", "u1", "alice"))
	assert.Equal(t, EnterDup, m.enter("chan", "u1", "alice"))

	_, _, count, _ := m.status("chan")
	assert.Equal(t, 1, count) // dedup: still one entrant
}

func TestGiveaway_EnterNoGiveaway(t *testing.T) {
	m := newTestManager()
	assert.Equal(t, EnterNoGiveaway, m.enter("chan", "u1", "alice"))
}

func TestGiveaway_CloseRejectsEntries(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)
	require.Equal(t, EnterOK, m.enter("chan", "u1", "alice"))

	count, ok := m.close("chan")
	require.True(t, ok)
	assert.Equal(t, 1, count)

	// New entries are now rejected, but the giveaway still exists.
	assert.Equal(t, EnterClosed, m.enter("chan", "u2", "bob"))
}

func TestGiveaway_CloseNoGiveaway(t *testing.T) {
	m := newTestManager()
	_, ok := m.close("chan")
	assert.False(t, ok)
}

// --- manager: provably-fair draw -------------------------------------------

// expectedWinner recomputes the deterministic winner the same way the manager
// does, so the assertion validates the algorithm rather than hard-coding a
// magic name.
func expectedWinner(seed int64, drawNumber int, eligible []string) string {
	return eligible[provablyFairIndex(seed, drawNumber, eligible)]
}

func TestGiveaway_DrawDeterministicWinner(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)
	ids := []string{"u1", "u2", "u3", "u4", "u5"}
	for _, id := range ids {
		require.Equal(t, EnterOK, m.enter("chan", id, "name-"+id))
	}

	winnerID, winnerName, seed, ok := m.draw("chan")
	require.True(t, ok)
	assert.Equal(t, int64(42), seed)
	// draw #0 over the full sorted set.
	wantID := expectedWinner(42, 0, ids)
	assert.Equal(t, wantID, winnerID)
	assert.Equal(t, "name-"+wantID, winnerName)
}

func TestGiveaway_DrawStableAcrossManagers(t *testing.T) {
	ids := []string{"u1", "u2", "u3", "u4", "u5"}
	draw := func() string {
		m := newTestManager()
		_, ok := m.open("chan", "win")
		require.True(t, ok)
		for _, id := range ids {
			require.Equal(t, EnterOK, m.enter("chan", id, id))
		}
		winnerID, _, _, ok := m.draw("chan")
		require.True(t, ok)
		return winnerID
	}
	// Same seed + same entrants ⇒ same winner, every time.
	assert.Equal(t, draw(), draw())
}

func TestGiveaway_RerollExcludesPastWinnerAndPicksDifferent(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)
	ids := []string{"u1", "u2", "u3", "u4", "u5"}
	for _, id := range ids {
		require.Equal(t, EnterOK, m.enter("chan", id, id))
	}

	first, _, _, ok := m.draw("chan")
	require.True(t, ok)

	second, _, _, ok := m.draw("chan")
	require.True(t, ok)
	assert.NotEqual(t, first, second, "reroll must pick a different entrant")

	// The reroll's winner is the deterministic draw #1 over the remaining set.
	remaining := make([]string, 0, len(ids)-1)
	for _, id := range ids {
		if id != first {
			remaining = append(remaining, id)
		}
	}
	assert.Equal(t, expectedWinner(42, 1, remaining), second)
}

func TestGiveaway_DrawNoEntrants(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)

	_, _, _, ok = m.draw("chan")
	assert.False(t, ok)
}

func TestGiveaway_DrawNoGiveaway(t *testing.T) {
	m := newTestManager()
	_, _, _, ok := m.draw("chan")
	assert.False(t, ok)
}

func TestGiveaway_RerollPastAllEntrants(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)
	require.Equal(t, EnterOK, m.enter("chan", "u1", "alice"))
	require.Equal(t, EnterOK, m.enter("chan", "u2", "bob"))

	_, _, _, ok = m.draw("chan")
	require.True(t, ok)
	_, _, _, ok = m.draw("chan")
	require.True(t, ok)
	// All entrants are past winners now.
	_, _, _, ok = m.draw("chan")
	assert.False(t, ok)
}

func TestGiveaway_DrawWorksOnClosedGiveaway(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)
	require.Equal(t, EnterOK, m.enter("chan", "u1", "alice"))
	_, ok = m.close("chan")
	require.True(t, ok)

	_, name, _, ok := m.draw("chan")
	require.True(t, ok)
	assert.Equal(t, "alice", name)
}

// --- manager: cancel / status / double-open --------------------------------

func TestGiveaway_CancelRemovesIt(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)
	require.Equal(t, EnterOK, m.enter("chan", "u1", "alice"))

	assert.True(t, m.cancel("chan"))
	// After cancel there is no giveaway at all.
	assert.Equal(t, EnterNoGiveaway, m.enter("chan", "u2", "bob"))
	assert.False(t, m.cancel("chan")) // nothing left to cancel
}

func TestGiveaway_DoubleOpenRejected(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)
	_, ok = m.open("chan", "again")
	assert.False(t, ok)
}

func TestGiveaway_Status(t *testing.T) {
	m := newTestManager()
	_, _, _, ok := m.status("chan")
	assert.False(t, ok)

	_, ok = m.open("chan", "win")
	require.True(t, ok)
	require.Equal(t, EnterOK, m.enter("chan", "u1", "alice"))

	kw, open, count, ok := m.status("chan")
	require.True(t, ok)
	assert.Equal(t, "win", kw)
	assert.True(t, open)
	assert.Equal(t, 1, count)

	_, ok = m.close("chan")
	require.True(t, ok)
	_, open, _, _ = m.status("chan")
	assert.False(t, open)
}

func TestGiveaway_ChannelKeyNormalised(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("  ChAn  ", "win")
	require.True(t, ok)
	// Differently-cased/padded channel resolves to the same giveaway.
	assert.Equal(t, EnterOK, m.enter("chan", "u1", "alice"))
	_, _, count, ok := m.status("CHAN")
	require.True(t, ok)
	assert.Equal(t, 1, count)
}

// --- manager: concurrency --------------------------------------------------

func TestGiveaway_ConcurrentEnter(t *testing.T) {
	m := newTestManager()
	_, ok := m.open("chan", "win")
	require.True(t, ok)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "u" + string(rune('A'+n%26)) + string(rune('0'+n/26))
			m.enter("chan", id, id)
		}(i)
	}
	wg.Wait()

	_, _, count, ok := m.status("chan")
	require.True(t, ok)
	assert.Equal(t, 50, count) // 50 unique IDs ⇒ 50 entrants
}

// --- commands: wiring + roles ----------------------------------------------

func TestGiveaway_NewGiveawayCommandsWiring(t *testing.T) {
	cmds := NewGiveawayCommands()
	require.Len(t, cmds, 4)
	byName := make(map[string]Command, len(cmds))
	for _, c := range cmds {
		byName[c.Name] = c
	}
	require.Contains(t, byName, "giveaway")
	require.Contains(t, byName, "enter")
	require.Contains(t, byName, "draw")
	require.Contains(t, byName, "reroll")

	assert.Equal(t, RoleModerator, byName["giveaway"].MinRole)
	assert.Equal(t, RoleEveryone, byName["enter"].MinRole)
	assert.Equal(t, RoleModerator, byName["draw"].MinRole)
	assert.Equal(t, RoleModerator, byName["reroll"].MinRole)
	assert.Equal(t, enterUserCooldown, byName["enter"].UserCooldown)
}

func msg(channel, userID, username string) Message {
	return Message{Channel: channel, UserID: userID, Username: username}
}

func TestGiveaway_CommandOpenAndEnterFlow(t *testing.T) {
	m := newTestManager()
	give := newGiveawayCommand(m)
	enter := newEnterCommand(m)

	open := give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"win"})
	assert.Contains(t, open.Text, "Giveaway open!")
	assert.Contains(t, open.Text, "keyword: win")
	assert.Contains(t, open.Text, "seed: 42")

	r := enter.Handler(context.Background(), msg("chan", "u1", "alice"), nil)
	assert.Equal(t, "@alice you're in! 🎟️", r.Text)

	dup := enter.Handler(context.Background(), msg("chan", "u1", "alice"), nil)
	assert.Equal(t, "@alice you're already entered.", dup.Text)
}

func TestGiveaway_CommandEnterNoGiveaway(t *testing.T) {
	m := newTestManager()
	enter := newEnterCommand(m)
	r := enter.Handler(context.Background(), msg("chan", "u1", "alice"), nil)
	assert.Equal(t, "@alice there's no giveaway running right now.", r.Text)
}

func TestGiveaway_CommandAlreadyRunning(t *testing.T) {
	m := newTestManager()
	give := newGiveawayCommand(m)
	give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"win"})
	again := give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"again"})
	assert.Equal(t, "@moddy a giveaway is already running - !draw or !giveaway cancel first.", again.Text)
}

func TestGiveaway_CommandUsageNoArg(t *testing.T) {
	m := newTestManager()
	give := newGiveawayCommand(m)
	r := give.Handler(context.Background(), msg("chan", "mod", "moddy"), nil)
	assert.Equal(t, "@moddy usage: !giveaway <keyword>", r.Text)
}

func TestGiveaway_CommandSubcommands(t *testing.T) {
	m := newTestManager()
	give := newGiveawayCommand(m)

	// status with nothing running.
	none := give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"status"})
	assert.Contains(t, none.Text, "no giveaway running")

	give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"win"})
	newEnterCommand(m).Handler(context.Background(), msg("chan", "u1", "alice"), nil)

	status := give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"status"})
	assert.Contains(t, status.Text, `"win"`)
	assert.Contains(t, status.Text, "open")
	assert.Contains(t, status.Text, "1 entrants")

	closed := give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"close"})
	assert.Equal(t, "Entries closed - 1 entrants. Use !draw.", closed.Text)

	cancel := give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"cancel"})
	assert.Equal(t, "Giveaway cancelled.", cancel.Text)

	// cancel/close with nothing running.
	assert.Contains(t, give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"cancel"}).Text, "no giveaway to cancel")
	assert.Contains(t, give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"close"}).Text, "no giveaway to close")
}

func TestGiveaway_CommandDrawAndReroll(t *testing.T) {
	m := newTestManager()
	give := newGiveawayCommand(m)
	enter := newEnterCommand(m)
	draw := newDrawCommand(m)
	reroll := newRerollCommand(m)

	give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"win"})
	for _, id := range []string{"u1", "u2", "u3"} {
		enter.Handler(context.Background(), msg("chan", id, "name-"+id), nil)
	}

	d := draw.Handler(context.Background(), msg("chan", "mod", "moddy"), nil)
	assert.Contains(t, d.Text, "🏆 The winner is @")
	assert.Contains(t, d.Text, "seed 42")

	r := reroll.Handler(context.Background(), msg("chan", "mod", "moddy"), nil)
	assert.Contains(t, r.Text, "🏆 The winner is @")

	// Drain the last entrant, then reroll has nobody left.
	reroll.Handler(context.Background(), msg("chan", "mod", "moddy"), nil)
	empty := reroll.Handler(context.Background(), msg("chan", "mod", "moddy"), nil)
	assert.Equal(t, "@moddy no more eligible entrants to reroll.", empty.Text)
}

func TestGiveaway_CommandDrawNoEntrants(t *testing.T) {
	m := newTestManager()
	give := newGiveawayCommand(m)
	draw := newDrawCommand(m)
	give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"win"})
	d := draw.Handler(context.Background(), msg("chan", "mod", "moddy"), nil)
	assert.Equal(t, "@moddy no eligible entrants to draw.", d.Text)
}

func TestGiveaway_CommandEnterClosed(t *testing.T) {
	m := newTestManager()
	give := newGiveawayCommand(m)
	enter := newEnterCommand(m)
	give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"win"})
	give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"close"})
	r := enter.Handler(context.Background(), msg("chan", "u1", "alice"), nil)
	assert.Equal(t, "@alice entries are closed for this giveaway.", r.Text)
}

// --- reply length sanity ---------------------------------------------------

func TestGiveaway_RepliesUnder400Chars(t *testing.T) {
	m := newTestManager()
	give := newGiveawayCommand(m)
	enter := newEnterCommand(m)
	draw := newDrawCommand(m)

	replies := []Reply{
		give.Handler(context.Background(), msg("chan", "mod", "moddy"), []string{"win"}),
		enter.Handler(context.Background(), msg("chan", "u1", "alice"), nil),
		draw.Handler(context.Background(), msg("chan", "mod", "moddy"), nil),
	}
	for _, r := range replies {
		assert.Less(t, len(r.Text), 400)
	}
}

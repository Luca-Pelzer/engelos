package commands

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectCall records one HeistBank.Collect invocation.
type collectCall struct {
	viewerID string
	amount   int64
}

// payoutCall records one HeistBank.Payout invocation.
type payoutCall struct {
	viewerID string
	amount   int64
}

// fakeHeistBank is a configurable HeistBank stub. It records every Collect and
// Payout call so tests can assert exact amounts. Collect's success is decided
// per-viewer (affordable) falling back to canCollectDefault.
type fakeHeistBank struct {
	balance       int64
	balanceStatus LoyaltyError

	affordable        map[string]bool
	canCollectDefault bool

	mu           sync.Mutex
	balanceCalls int
	collects     []collectCall
	payouts      []payoutCall
}

func (f *fakeHeistBank) Balance(_ context.Context, _, _ string) (int64, LoyaltyError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balanceCalls++
	return f.balance, f.balanceStatus
}

func (f *fakeHeistBank) Collect(_ context.Context, _, viewerID string, amount int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := f.canCollectDefault
	if f.affordable != nil {
		if v, present := f.affordable[viewerID]; present {
			ok = v
		}
	}
	if ok {
		f.collects = append(f.collects, collectCall{viewerID: viewerID, amount: amount})
	}
	return ok
}

func (f *fakeHeistBank) Payout(_ context.Context, _, viewerID string, amount int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payouts = append(f.payouts, payoutCall{viewerID: viewerID, amount: amount})
}

func (f *fakeHeistBank) collectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.collects)
}

func (f *fakeHeistBank) payoutCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.payouts)
}

// fakeHeistSender captures the announced messages.
type fakeHeistSender struct {
	mu       sync.Mutex
	messages []string
	err      error
}

func (s *fakeHeistSender) Send(_ context.Context, _, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	return s.err
}

func (s *fakeHeistSender) sendCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *fakeHeistSender) lastMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return ""
	}
	return s.messages[len(s.messages)-1]
}

func okHeistBank() *fakeHeistBank {
	return &fakeHeistBank{
		balance:           1000,
		balanceStatus:     LoyaltyOK,
		canCollectDefault: true,
	}
}

// testManager wires a manager with deterministic seams: a fixed clock, a
// stored (manually-triggered) afterFn, and a stub survival roll. Call the
// returned trigger() to run the scheduled resolution synchronously.
func testManager(bank HeistBank, sender HeistSender, roll func(n int) int) (*heistManager, func()) {
	var stored func()
	m := newHeistManager(bank, sender)
	m.window = 45 * time.Second
	m.now = func() time.Time { return time.Unix(1000, 0) }
	m.randIntN = roll
	m.afterFn = func(_ time.Duration, f func()) { stored = f }
	return m, func() {
		if stored != nil {
			stored()
		}
	}
}

// allSurvive makes every survival roll return 0 (survives).
func allSurvive(int) int { return 0 }

// allDie makes every survival roll return 1 (caught).
func allDie(int) int { return 1 }

func bg() context.Context { return context.Background() }

// --- manager.join ----------------------------------------------------------

func TestHeist_StartCollectsAndSchedules(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, trigger := testManager(bank, sender, allSurvive)

	out := m.join(bg(), "chan", "u1", "bob", 100)

	assert.Equal(t, JoinStarted, out.result)
	assert.Equal(t, 1, out.count)
	assert.Equal(t, 45*time.Second, out.window)
	require.Equal(t, 1, bank.collectCount())
	assert.Equal(t, collectCall{viewerID: "u1", amount: 100}, bank.collects[0])
	// Resolution is scheduled, not yet run.
	assert.Equal(t, 0, sender.sendCount())
	require.NotNil(t, trigger)
}

func TestHeist_SecondPlayerJoinsExisting(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)

	m.join(bg(), "chan", "u1", "bob", 100)
	out := m.join(bg(), "chan", "u2", "alice", 200)

	assert.Equal(t, JoinedExisting, out.result)
	assert.Equal(t, 2, out.count)
	require.Equal(t, 2, bank.collectCount())
	assert.Equal(t, collectCall{viewerID: "u2", amount: 200}, bank.collects[1])
}

func TestHeist_DuplicateJoinRejected(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)

	m.join(bg(), "chan", "u1", "bob", 100)
	out := m.join(bg(), "chan", "u1", "bob", 100)

	assert.Equal(t, JoinDup, out.result)
	assert.Equal(t, 1, out.count)
	// No second Collect for a dup.
	assert.Equal(t, 1, bank.collectCount())
}

func TestHeist_PoorPlayerStartCannotAfford(t *testing.T) {
	bank := okHeistBank()
	bank.canCollectDefault = false
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)

	out := m.join(bg(), "chan", "u1", "bob", 100)

	assert.Equal(t, JoinPoor, out.result)
	assert.Equal(t, 0, bank.collectCount())
	// No heist was created.
	_, ok := m.byChan["chan"]
	assert.False(t, ok)
}

func TestHeist_PoorPlayerJoinExistingCannotAfford(t *testing.T) {
	bank := okHeistBank()
	bank.affordable = map[string]bool{"u1": true, "u2": false}
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)

	m.join(bg(), "chan", "u1", "bob", 100)
	out := m.join(bg(), "chan", "u2", "alice", 200)

	assert.Equal(t, JoinPoor, out.result)
	// Only the starter's stake was taken.
	assert.Equal(t, 1, bank.collectCount())
}

// --- manager.resolve -------------------------------------------------------

func TestHeist_ResolveAllSurvivePaysDouble(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, trigger := testManager(bank, sender, allSurvive)

	m.join(bg(), "chan", "u1", "bob", 100)
	m.join(bg(), "chan", "u2", "alice", 200)
	trigger()

	require.Equal(t, 2, bank.payoutCount())
	assert.Equal(t, payoutCall{viewerID: "u1", amount: 200}, bank.payouts[0])
	assert.Equal(t, payoutCall{viewerID: "u2", amount: 400}, bank.payouts[1])
	require.Equal(t, 1, sender.sendCount())
	msg := sender.lastMessage()
	assert.Contains(t, msg, "bob")
	assert.Contains(t, msg, "alice")
	assert.Contains(t, msg, "doubled their stake")
	assert.Contains(t, msg, "Caught: none")
	// The lobby is removed after resolution.
	_, ok := m.byChan["chan"]
	assert.False(t, ok)
}

func TestHeist_ResolveAllDieNoPayout(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, trigger := testManager(bank, sender, allDie)

	m.join(bg(), "chan", "u1", "bob", 100)
	m.join(bg(), "chan", "u2", "alice", 200)
	trigger()

	assert.Equal(t, 0, bank.payoutCount())
	require.Equal(t, 1, sender.sendCount())
	assert.Equal(t, "💀 The heist failed! Everyone got caught and lost their loot.",
		sender.lastMessage())
}

func TestHeist_ResolveMixedSurvivors(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	// First player survives (0), second caught (1).
	rolls := []int{0, 1}
	i := 0
	roll := func(int) int {
		v := rolls[i]
		i++
		return v
	}
	m, trigger := testManager(bank, sender, roll)

	m.join(bg(), "chan", "u1", "bob", 100)
	m.join(bg(), "chan", "u2", "alice", 200)
	trigger()

	require.Equal(t, 1, bank.payoutCount())
	assert.Equal(t, payoutCall{viewerID: "u1", amount: 200}, bank.payouts[0])
	msg := sender.lastMessage()
	assert.Contains(t, msg, "Survivors: bob")
	assert.Contains(t, msg, "Caught: alice")
}

func TestHeist_ResolveTwiceIsNoop(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, trigger := testManager(bank, sender, allSurvive)

	m.join(bg(), "chan", "u1", "bob", 100)
	trigger()
	trigger() // second resolve must do nothing

	assert.Equal(t, 1, bank.payoutCount())
	assert.Equal(t, 1, sender.sendCount())
}

func TestHeist_ResolveSendErrorDoesNotPanic(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{err: assertErr{}}
	m, trigger := testManager(bank, sender, allSurvive)

	m.join(bg(), "chan", "u1", "bob", 100)
	require.NotPanics(t, trigger)
	// Payout still happened despite the announce failure.
	assert.Equal(t, 1, bank.payoutCount())
}

type assertErr struct{}

func (assertErr) Error() string { return "send failed" }

func TestHeist_LargeCrewSummarisesSurvivors(t *testing.T) {
	survivors := []string{"a", "b", "c", "d", "e", "f", "g"}
	msg := heistOutcomeMessage(survivors, nil)
	assert.Contains(t, msg, "7 survivors split the loot")
	assert.Less(t, len(msg), 400)
}

func TestHeist_OutcomeMessageUnderLimit(t *testing.T) {
	var survivors, caught []string
	for range 50 {
		survivors = append(survivors, "surv")
		caught = append(caught, "dead")
	}
	msg := heistOutcomeMessage(survivors, caught)
	assert.Less(t, len(msg), 400)
}

// --- command ---------------------------------------------------------------

func TestHeistCommand_StartReply(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	reply := cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"100"})

	assert.Contains(t, reply.Text, "🏦")
	assert.Contains(t, reply.Text, "@bob")
	assert.Contains(t, reply.Text, "100")
	assert.Contains(t, reply.Text, "45s")
}

func TestHeistCommand_JoinReply(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"100"})
	reply := cmd.Handler(bg(), Message{Channel: "chan", UserID: "u2", Username: "alice"}, []string{"50"})

	assert.Equal(t, "💼 @alice joined the heist! (2 in the crew)", reply.Text)
}

func TestHeistCommand_DupReply(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"100"})
	reply := cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"100"})

	assert.Equal(t, "@bob you're already in this heist.", reply.Text)
}

func TestHeistCommand_PoorReply(t *testing.T) {
	bank := okHeistBank()
	bank.canCollectDefault = false
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	reply := cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"100"})

	assert.Equal(t, "@bob you don't have 100 points to join.", reply.Text)
}

func TestHeistCommand_AllBetUsesBalance(t *testing.T) {
	bank := okHeistBank()
	bank.balance = 777
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"all"})

	require.Equal(t, 1, bank.collectCount())
	assert.Equal(t, int64(777), bank.collects[0].amount)
}

func TestHeistCommand_PercentBet(t *testing.T) {
	bank := okHeistBank()
	bank.balance = 1000
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"50%"})

	require.Equal(t, 1, bank.collectCount())
	assert.Equal(t, int64(500), bank.collects[0].amount)
}

func TestHeistCommand_BadAmountUsage(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	reply := cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"abc"})

	assert.Equal(t, "@bob usage: !heist <amount>", reply.Text)
	assert.Equal(t, 0, bank.collectCount())
}

func TestHeistCommand_NoArgUsage(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	reply := cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, nil)

	assert.Equal(t, "@bob usage: !heist <amount>", reply.Text)
}

func TestHeistCommand_NoAccount(t *testing.T) {
	bank := okHeistBank()
	bank.balanceStatus = LoyaltyNotFound
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	reply := cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"100"})

	assert.Equal(t, "@bob you don't have any points yet", reply.Text)
}

func TestHeistCommand_BalanceErrorGeneric(t *testing.T) {
	bank := okHeistBank()
	bank.balanceStatus = LoyaltyUnavailable
	sender := &fakeHeistSender{}
	m, _ := testManager(bank, sender, allSurvive)
	cmd := newHeistCommand(m)

	reply := cmd.Handler(bg(), Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"100"})

	assert.Equal(t, "couldn't check that right now", reply.Text)
}

func TestHeistCommand_NilBankUnavailable(t *testing.T) {
	cmd := NewHeistGame(nil, &fakeHeistSender{})
	reply := cmd.Handler(bg(), Message{Username: "bob"}, []string{"100"})
	assert.Equal(t, "heists are unavailable", reply.Text)
}

func TestHeistCommand_NilSenderUnavailable(t *testing.T) {
	cmd := NewHeistGame(okHeistBank(), nil)
	reply := cmd.Handler(bg(), Message{Username: "bob"}, []string{"100"})
	assert.Equal(t, "heists are unavailable", reply.Text)
}

func TestHeistCommand_Exported(t *testing.T) {
	cmd := NewHeistGame(okHeistBank(), &fakeHeistSender{})
	assert.Equal(t, "heist", cmd.Name)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
	assert.Equal(t, heistUserCooldown, cmd.UserCooldown)
}

// --- concurrency -----------------------------------------------------------

func TestHeist_ConcurrentJoins(t *testing.T) {
	bank := okHeistBank()
	sender := &fakeHeistSender{}
	m, trigger := testManager(bank, sender, allSurvive)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			uid := "u" + string(rune('a'+id%26)) + string(rune('a'+id/26))
			m.join(bg(), "chan", uid, "viewer", 10)
		}(i)
	}
	wg.Wait()
	trigger()

	assert.Equal(t, 1, sender.sendCount())
}

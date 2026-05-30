package commands

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDuelBank is a configurable DuelBank stub. It records the (winnerID,
// loserID, amount) of the last Settle call so tests can assert exactly who the
// command picked, and lets each leg's behaviour be configured.
type fakeDuelBank struct {
	balance       int64
	balanceStatus LoyaltyError

	// affordable maps viewerID -> whether CanAfford returns true. A viewerID
	// absent from the map defaults to canAffordDefault.
	affordable       map[string]bool
	canAffordDefault bool

	settleBalance int64
	settleStatus  LoyaltyError

	mu           sync.Mutex
	balanceCalls int
	affordCalls  int
	settleCalls  int
	gotWinnerID  string
	gotLoserID   string
	gotAmount    int64
}

func (f *fakeDuelBank) CanAfford(_ context.Context, _, viewerID string, _ int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.affordCalls++
	if f.affordable != nil {
		if v, ok := f.affordable[viewerID]; ok {
			return v
		}
	}
	return f.canAffordDefault
}

func (f *fakeDuelBank) Balance(_ context.Context, _, _ string) (int64, LoyaltyError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balanceCalls++
	return f.balance, f.balanceStatus
}

func (f *fakeDuelBank) Settle(_ context.Context, _, winnerID, loserID string, amount int64) (int64, LoyaltyError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settleCalls++
	f.gotWinnerID = winnerID
	f.gotLoserID = loserID
	f.gotAmount = amount
	return f.settleBalance, f.settleStatus
}

// fakeClock is a controllable monotonic clock for TTL tests.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.t = c.t.Add(d)
}

func okBank() *fakeDuelBank {
	return &fakeDuelBank{
		balance:          1000,
		balanceStatus:    LoyaltyOK,
		canAffordDefault: true,
		settleBalance:    2000,
		settleStatus:     LoyaltyOK,
	}
}

// --- !duel -----------------------------------------------------------------

func TestDuel_HappyPathStoresChallenge(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"alice", "100"})

	assert.Contains(t, reply.Text, "alice")
	assert.Contains(t, reply.Text, "!accept")
	assert.Contains(t, reply.Text, "100")
	assert.Equal(t, RoleEveryone, cmd.MinRole)

	// A challenge keyed by the target login now exists.
	c, ok := reg.take("chan", "alice")
	require.True(t, ok)
	assert.Equal(t, "u1", c.challengerID)
	assert.Equal(t, "bob", c.challengerName)
	assert.Equal(t, "alice", c.targetLogin)
	assert.Equal(t, int64(100), c.amount)
}

func TestDuel_StripsAtPrefixAndCaseInsensitiveTarget(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"@Alice", "100"})

	_, ok := reg.take("chan", "alice")
	assert.True(t, ok)
}

func TestDuel_SelfErrors(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"bob", "100"})

	assert.Equal(t, "@bob you can't duel yourself", reply.Text)
	assert.Equal(t, 0, bank.balanceCalls)
}

func TestDuel_InsufficientErrors(t *testing.T) {
	bank := okBank()
	bank.canAffordDefault = false
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"alice", "100"})

	assert.Equal(t, "@bob you don't have 100 points to duel", reply.Text)
	_, ok := reg.take("chan", "alice")
	assert.False(t, ok) // nothing stored
}

func TestDuel_UsageOnMissingArgs(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"alice"})

	assert.Equal(t, "@bob usage: !duel <user> <amount>", reply.Text)
	assert.Equal(t, 0, bank.balanceCalls)
}

func TestDuel_BadAmountUsage(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"alice", "abc"})

	assert.Equal(t, "@bob usage: !duel <user> <amount>", reply.Text)
}

func TestDuel_NoAccountErrors(t *testing.T) {
	bank := okBank()
	bank.balanceStatus = LoyaltyNotFound
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"alice", "100"})

	assert.Equal(t, "@bob you don't have any points yet", reply.Text)
}

func TestDuel_DuplicateChallengeRejected(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	first := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"alice", "100"})
	assert.Contains(t, first.Text, "challenges")

	second := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "carl"}, []string{"alice", "50"})
	assert.Equal(t, "@carl there's already a pending duel for alice", second.Text)
}

func TestDuel_AllBetUsesBalance(t *testing.T) {
	bank := okBank()
	bank.balance = 777
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(bank, reg)

	cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u1", Username: "bob"}, []string{"alice", "all"})

	c, ok := reg.take("chan", "alice")
	require.True(t, ok)
	assert.Equal(t, int64(777), c.amount)
}

func TestDuel_NilBankUnavailable(t *testing.T) {
	reg := newDuelRegistry(duelTTL)
	cmd := NewDuelCommand(nil, reg)
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"alice", "100"})
	assert.Equal(t, "points are unavailable", reply.Text)
}

func TestDuel_NilRegistryUnavailable(t *testing.T) {
	bank := okBank()
	cmd := NewDuelCommand(bank, nil)
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"alice", "100"})
	assert.Equal(t, "points are unavailable", reply.Text)
}

func TestDuel_Exported(t *testing.T) {
	cmd := NewDuelCommand(okBank(), newDuelRegistry(duelTTL))
	assert.Equal(t, "duel", cmd.Name)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

// --- !accept ---------------------------------------------------------------

func TestAccept_NoPendingDuel(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	cmd := newAcceptCommand(bank, reg, stubRand(0))

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "alice"}, nil)

	assert.Equal(t, "@alice you have no pending duel to accept.", reply.Text)
	assert.Equal(t, 0, bank.settleCalls)
}

func TestAccept_ChallengerWins(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: reg.now(),
	}))
	// coin = 0 % 2 = 0 → challenger wins.
	cmd := newAcceptCommand(bank, reg, stubRand(0))

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "alice"}, nil)

	assert.Equal(t, 1, bank.settleCalls)
	assert.Equal(t, "u1", bank.gotWinnerID)
	assert.Equal(t, "u2", bank.gotLoserID)
	assert.Equal(t, int64(100), bank.gotAmount)
	// Pot = amount*2 = 200.
	assert.Equal(t, "⚔️ bob beat alice and won the 200-points pot! 🏆", reply.Text)
}

func TestAccept_AccepterWins(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: reg.now(),
	}))
	// coin = 1 % 2 = 1 → accepter wins.
	cmd := newAcceptCommand(bank, reg, stubRand(1))

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "alice"}, nil)

	assert.Equal(t, "u2", bank.gotWinnerID)
	assert.Equal(t, "u1", bank.gotLoserID)
	assert.Equal(t, int64(100), bank.gotAmount)
	assert.Equal(t, "⚔️ alice beat bob and won the 200-points pot! 🏆", reply.Text)
}

func TestAccept_AccepterUsernameCaseInsensitive(t *testing.T) {
	bank := okBank()
	reg := newDuelRegistry(duelTTL)
	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: reg.now(),
	}))
	cmd := newAcceptCommand(bank, reg, stubRand(0))

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "Alice"}, nil)

	assert.Equal(t, 1, bank.settleCalls)
	assert.Contains(t, reply.Text, "won the 200-points pot")
}

func TestAccept_ChallengerCantAffordAnymore(t *testing.T) {
	bank := okBank()
	bank.affordable = map[string]bool{"u1": false, "u2": true}
	reg := newDuelRegistry(duelTTL)
	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: reg.now(),
	}))
	cmd := newAcceptCommand(bank, reg, stubRand(0))

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "alice"}, nil)

	assert.Equal(t, "@alice bob can no longer cover the duel.", reply.Text)
	assert.Equal(t, 0, bank.settleCalls)
}

func TestAccept_AccepterCantAfford(t *testing.T) {
	bank := okBank()
	bank.affordable = map[string]bool{"u1": true, "u2": false}
	reg := newDuelRegistry(duelTTL)
	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: reg.now(),
	}))
	cmd := newAcceptCommand(bank, reg, stubRand(0))

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "alice"}, nil)

	assert.Equal(t, "@alice you don't have 100 points to accept.", reply.Text)
	assert.Equal(t, 0, bank.settleCalls)
}

func TestAccept_SettleInsufficientFellThrough(t *testing.T) {
	bank := okBank()
	bank.settleStatus = LoyaltyInsufficient
	reg := newDuelRegistry(duelTTL)
	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: reg.now(),
	}))
	cmd := newAcceptCommand(bank, reg, stubRand(0))

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "alice"}, nil)

	assert.Equal(t, "@alice the duel fell through — someone couldn't cover it.", reply.Text)
}

func TestAccept_SettleErrorGeneric(t *testing.T) {
	bank := okBank()
	bank.settleStatus = LoyaltyUnavailable
	reg := newDuelRegistry(duelTTL)
	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: reg.now(),
	}))
	cmd := newAcceptCommand(bank, reg, stubRand(0))

	reply := cmd.Handler(context.Background(),
		Message{Channel: "chan", UserID: "u2", Username: "alice"}, nil)

	assert.Equal(t, "couldn't settle the duel right now.", reply.Text)
}

func TestAccept_NilBankUnavailable(t *testing.T) {
	reg := newDuelRegistry(duelTTL)
	cmd := NewAcceptCommand(nil, reg)
	reply := cmd.Handler(context.Background(), Message{Username: "alice"}, nil)
	assert.Equal(t, "points are unavailable", reply.Text)
}

func TestAccept_Exported(t *testing.T) {
	cmd := NewAcceptCommand(okBank(), newDuelRegistry(duelTTL))
	assert.Equal(t, "accept", cmd.Name)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

// --- registry: TTL expiry --------------------------------------------------

func TestRegistry_ExpiryPurgesOnTake(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	reg := newDuelRegistry(duelTTL).withClock(clk.now)

	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: clk.now(),
	}))

	// Advance past the TTL: the challenge must no longer be acceptable.
	clk.advance(duelTTL + time.Second)
	_, ok := reg.take("chan", "alice")
	assert.False(t, ok)
}

func TestRegistry_LiveBeforeTTL(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	reg := newDuelRegistry(duelTTL).withClock(clk.now)

	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: clk.now(),
	}))

	clk.advance(duelTTL - time.Second)
	c, ok := reg.take("chan", "alice")
	require.True(t, ok)
	assert.Equal(t, int64(100), c.amount)
}

func TestRegistry_ExpiredChallengeOverwritable(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	reg := newDuelRegistry(duelTTL).withClock(clk.now)

	require.True(t, reg.add("chan", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: clk.now(),
	}))
	clk.advance(duelTTL + time.Second)
	// The old challenge is expired, so a new one for the same target is allowed.
	assert.True(t, reg.add("chan", &challenge{
		challengerID: "u3", challengerName: "dave",
		targetLogin: "alice", amount: 50, createdAt: clk.now(),
	}))
}

func TestRegistry_TakeUnknownTarget(t *testing.T) {
	reg := newDuelRegistry(duelTTL)
	_, ok := reg.take("chan", "nobody")
	assert.False(t, ok)
}

func TestRegistry_ChannelScoped(t *testing.T) {
	reg := newDuelRegistry(duelTTL)
	require.True(t, reg.add("chanA", &challenge{
		challengerID: "u1", challengerName: "bob",
		targetLogin: "alice", amount: 100, createdAt: reg.now(),
	}))
	// Same target login in a different channel must not collide.
	assert.True(t, reg.add("chanB", &challenge{
		challengerID: "u2", challengerName: "carl",
		targetLogin: "alice", amount: 50, createdAt: reg.now(),
	}))
	_, ok := reg.take("chanA", "alice")
	assert.True(t, ok)
	_, ok = reg.take("chanB", "alice")
	assert.True(t, ok)
}

// --- registry: concurrency -------------------------------------------------

func TestRegistry_ConcurrentAddTake(t *testing.T) {
	reg := newDuelRegistry(duelTTL)
	var wg sync.WaitGroup
	for i := range 50 {
		login := "t" + string(rune('a'+i%26))
		wg.Add(2)
		go func(l string) {
			defer wg.Done()
			reg.add("chan", &challenge{
				challengerID: "u", challengerName: "bob",
				targetLogin: l, amount: 10, createdAt: reg.now(),
			})
		}(login)
		go func(l string) {
			defer wg.Done()
			reg.take("chan", l)
		}(login)
	}
	wg.Wait()
}

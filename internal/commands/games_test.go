package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeBank is a configurable GameBank stub. It records the (bet, payout) of the
// last Wager call so tests can assert the exact stake/credit the command chose,
// and returns a fixed balance/status for both Balance and Wager.
type fakeBank struct {
	balance       int64
	balanceStatus LoyaltyError
	wagerBalance  int64
	wagerStatus   LoyaltyError

	balanceCalls int
	wagerCalls   int
	gotBet       int64
	gotPayout    int64
}

func (f *fakeBank) Balance(_ context.Context, _, _ string) (int64, LoyaltyError) {
	f.balanceCalls++
	return f.balance, f.balanceStatus
}

func (f *fakeBank) Wager(_ context.Context, _, _ string, bet, payout int64) (int64, LoyaltyError) {
	f.wagerCalls++
	f.gotBet = bet
	f.gotPayout = payout
	return f.wagerBalance, f.wagerStatus
}

// seqRand returns a stub that yields the given int63 values in order, repeating
// the last one once exhausted (slots draws three times per spin).
func seqRand(vs ...int64) func() int64 {
	i := 0
	return func() int64 {
		v := vs[i]
		if i < len(vs)-1 {
			i++
		}
		return v
	}
}

// --- !gamble ---------------------------------------------------------------

func TestGamble_Win(t *testing.T) {
	// roll = 0%100 + 1 = 1, which is <= 47 → win, payout = bet*2.
	bank := &fakeBank{balance: 500, balanceStatus: LoyaltyOK, wagerBalance: 600, wagerStatus: LoyaltyOK}
	cmd := newGambleCommand(bank, stubRand(0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, int64(100), bank.gotBet)
	assert.Equal(t, int64(200), bank.gotPayout) // bet*2 on a win
	assert.Equal(t, "🎲 @bob rolled 1 and WON 100 points! Balance: 600", reply.Text)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
	assert.Equal(t, 1, bank.balanceCalls)
	assert.Equal(t, 1, bank.wagerCalls)
}

func TestGamble_Loss(t *testing.T) {
	// roll = 47%100 + 1 = 48, which is > 47 → loss, payout = 0.
	bank := &fakeBank{balance: 500, balanceStatus: LoyaltyOK, wagerBalance: 400, wagerStatus: LoyaltyOK}
	cmd := newGambleCommand(bank, stubRand(47))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, int64(100), bank.gotBet)
	assert.Equal(t, int64(0), bank.gotPayout) // 0 on a loss
	assert.Equal(t, "🎲 @bob rolled 48 and lost 100 points. Balance: 400", reply.Text)
}

func TestGamble_BoundaryWinAt47(t *testing.T) {
	// roll = 46%100 + 1 = 47, exactly the win threshold → win.
	bank := &fakeBank{balance: 500, balanceStatus: LoyaltyOK, wagerBalance: 510, wagerStatus: LoyaltyOK}
	cmd := newGambleCommand(bank, stubRand(46))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"10"})

	assert.Equal(t, int64(20), bank.gotPayout)
	assert.Contains(t, reply.Text, "rolled 47 and WON")
}

func TestGamble_AllBetsFullBalance(t *testing.T) {
	bank := &fakeBank{balance: 1234, balanceStatus: LoyaltyOK, wagerBalance: 2468, wagerStatus: LoyaltyOK}
	cmd := newGambleCommand(bank, stubRand(0)) // win
	cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"all"})

	assert.Equal(t, int64(1234), bank.gotBet) // staked entire balance
	assert.Equal(t, int64(2468), bank.gotPayout)
}

func TestGamble_PercentBet(t *testing.T) {
	bank := &fakeBank{balance: 1000, balanceStatus: LoyaltyOK, wagerBalance: 1500, wagerStatus: LoyaltyOK}
	cmd := newGambleCommand(bank, stubRand(0)) // win
	cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"50%"})

	assert.Equal(t, int64(500), bank.gotBet) // 50% of 1000
	assert.Equal(t, int64(1000), bank.gotPayout)
}

func TestGamble_BadAmountUsage(t *testing.T) {
	bank := &fakeBank{balance: 500, balanceStatus: LoyaltyOK}
	cmd := newGambleCommand(bank, stubRand(0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"abc"})

	assert.Equal(t, "@bob usage: !gamble <amount|all|50%>", reply.Text)
	assert.Equal(t, 0, bank.wagerCalls) // never settled on a parse failure
}

func TestGamble_NoArgsUsage(t *testing.T) {
	bank := &fakeBank{balance: 500, balanceStatus: LoyaltyOK}
	cmd := newGambleCommand(bank, stubRand(0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)

	assert.Equal(t, "@bob usage: !gamble <amount|all|50%>", reply.Text)
	assert.Equal(t, 0, bank.balanceCalls)
}

func TestGamble_InsufficientMapsThrough(t *testing.T) {
	bank := &fakeBank{balance: 50, balanceStatus: LoyaltyOK, wagerStatus: LoyaltyInsufficient}
	cmd := newGambleCommand(bank, stubRand(0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, "@bob you don't have 100 points to gamble", reply.Text)
}

func TestGamble_NoAccount(t *testing.T) {
	bank := &fakeBank{balanceStatus: LoyaltyNotFound}
	cmd := newGambleCommand(bank, stubRand(0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, "@bob you don't have any points yet", reply.Text)
	assert.Equal(t, 0, bank.wagerCalls)
}

func TestGamble_NilBankUnavailable(t *testing.T) {
	cmd := NewGambleCommand(nil)
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})
	assert.Equal(t, "points are unavailable", reply.Text)
}

func TestGamble_Exported(t *testing.T) {
	bank := &fakeBank{balance: 100, balanceStatus: LoyaltyOK, wagerBalance: 200, wagerStatus: LoyaltyOK}
	cmd := NewGambleCommand(bank)
	assert.Equal(t, "gamble", cmd.Name)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

// --- !slots ----------------------------------------------------------------

// idx7 is the index of "7️⃣" and idxDiamond of "💎" in slotSymbols.
const (
	idx7       = 5
	idxDiamond = 4
)

func TestSlots_JackpotThreeSevens(t *testing.T) {
	bank := &fakeBank{balance: 1000, balanceStatus: LoyaltyOK, wagerBalance: 1900, wagerStatus: LoyaltyOK}
	cmd := newSlotsCommand(bank, seqRand(idx7, idx7, idx7))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, int64(100), bank.gotBet)
	assert.Equal(t, int64(1000), bank.gotPayout) // bet*10 jackpot
	assert.Contains(t, reply.Text, "🎰 @bob spun [7️⃣ | 7️⃣ | 7️⃣] — WON")
	assert.Contains(t, reply.Text, "Balance: 1,900")
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestSlots_ThreeDiamonds(t *testing.T) {
	bank := &fakeBank{balance: 1000, balanceStatus: LoyaltyOK, wagerBalance: 1600, wagerStatus: LoyaltyOK}
	cmd := newSlotsCommand(bank, seqRand(idxDiamond, idxDiamond, idxDiamond))
	cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, int64(700), bank.gotPayout) // bet*7
}

func TestSlots_ThreeOtherSymbol(t *testing.T) {
	// three 🍒 (index 0) → bet*4.
	bank := &fakeBank{balance: 1000, balanceStatus: LoyaltyOK, wagerBalance: 1300, wagerStatus: LoyaltyOK}
	cmd := newSlotsCommand(bank, seqRand(0, 0, 0))
	cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, int64(400), bank.gotPayout) // bet*4
}

func TestSlots_TwoMatch(t *testing.T) {
	// 🍒 🍒 🍋 → exactly two match → bet*2.
	bank := &fakeBank{balance: 1000, balanceStatus: LoyaltyOK, wagerBalance: 1100, wagerStatus: LoyaltyOK}
	cmd := newSlotsCommand(bank, seqRand(0, 0, 1))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, int64(200), bank.gotPayout) // bet*2 small win
	assert.Contains(t, reply.Text, "spun [🍒 | 🍒 | 🍋] — WON")
}

func TestSlots_Loss(t *testing.T) {
	// 🍒 🍋 🔔 → all distinct → 0.
	bank := &fakeBank{balance: 1000, balanceStatus: LoyaltyOK, wagerBalance: 900, wagerStatus: LoyaltyOK}
	cmd := newSlotsCommand(bank, seqRand(0, 1, 2))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, int64(0), bank.gotPayout)
	assert.Equal(t, "🎰 @bob spun [🍒 | 🍋 | 🔔] — no win, lost 100. Balance: 900", reply.Text)
}

func TestSlots_AllBetsFullBalance(t *testing.T) {
	bank := &fakeBank{balance: 250, balanceStatus: LoyaltyOK, wagerBalance: 2500, wagerStatus: LoyaltyOK}
	cmd := newSlotsCommand(bank, seqRand(idx7, idx7, idx7))
	cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"all"})

	assert.Equal(t, int64(250), bank.gotBet)     // whole balance staked
	assert.Equal(t, int64(2500), bank.gotPayout) // bet*10
}

func TestSlots_PercentBet(t *testing.T) {
	bank := &fakeBank{balance: 400, balanceStatus: LoyaltyOK, wagerBalance: 600, wagerStatus: LoyaltyOK}
	cmd := newSlotsCommand(bank, seqRand(0, 0, 1)) // two-match
	cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"25%"})

	assert.Equal(t, int64(100), bank.gotBet)    // 25% of 400
	assert.Equal(t, int64(200), bank.gotPayout) // bet*2
}

func TestSlots_BadAmountUsage(t *testing.T) {
	bank := &fakeBank{balance: 500, balanceStatus: LoyaltyOK}
	cmd := newSlotsCommand(bank, seqRand(0, 0, 0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"-5"})

	assert.Equal(t, "@bob usage: !slots <amount|all|50%>", reply.Text)
	assert.Equal(t, 0, bank.wagerCalls)
}

func TestSlots_InsufficientMapsThrough(t *testing.T) {
	bank := &fakeBank{balance: 50, balanceStatus: LoyaltyOK, wagerStatus: LoyaltyInsufficient}
	cmd := newSlotsCommand(bank, seqRand(0, 0, 0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})

	assert.Equal(t, "@bob you don't have 100 points to spin", reply.Text)
}

func TestSlots_NilBankUnavailable(t *testing.T) {
	cmd := NewSlotsCommand(nil)
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"100"})
	assert.Equal(t, "points are unavailable", reply.Text)
}

func TestSlots_Exported(t *testing.T) {
	bank := &fakeBank{balance: 100, balanceStatus: LoyaltyOK, wagerBalance: 200, wagerStatus: LoyaltyOK}
	cmd := NewSlotsCommand(bank)
	assert.Equal(t, "slots", cmd.Name)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

// --- parseBet --------------------------------------------------------------

func TestParseBet(t *testing.T) {
	cases := []struct {
		arg     string
		balance int64
		want    int64
		ok      bool
	}{
		{"100", 500, 100, true},
		{"all", 500, 500, true},
		{"ALL", 500, 500, true},
		{"max", 500, 500, true},
		{"Max", 0, 0, false}, // empty balance can't bet all
		{"all", 0, 0, false}, // empty balance
		{"50%", 1000, 500, true},
		{"1%", 1000, 10, true},
		{"100%", 1000, 1000, true},
		{"0%", 1000, 0, false},   // out of 1..100 range
		{"101%", 1000, 0, false}, // out of range
		{"50%", 1, 0, false},     // floors to 0
		{"abc", 500, 0, false},
		{"-5", 500, 0, false},
		{"0", 500, 0, false},
		{"", 500, 0, false},
	}
	for _, c := range cases {
		got, ok := parseBet(c.arg, c.balance)
		assert.Equal(t, c.ok, ok, "parseBet(%q,%d) ok", c.arg, c.balance)
		if c.ok {
			assert.Equal(t, c.want, got, "parseBet(%q,%d) value", c.arg, c.balance)
		}
	}
}

func TestSlotsPayoutTable(t *testing.T) {
	assert.Equal(t, int64(1000), slotsPayout("7️⃣", "7️⃣", "7️⃣", 100)) // jackpot
	assert.Equal(t, int64(700), slotsPayout("💎", "💎", "💎", 100))        // diamonds
	assert.Equal(t, int64(400), slotsPayout("🍒", "🍒", "🍒", 100))        // other triple
	assert.Equal(t, int64(200), slotsPayout("🍒", "🍒", "🍋", 100))        // two match
	assert.Equal(t, int64(200), slotsPayout("🍋", "🍒", "🍒", 100))        // two match (b==c)
	assert.Equal(t, int64(200), slotsPayout("🍒", "🍋", "🍒", 100))        // two match (a==c)
	assert.Equal(t, int64(0), slotsPayout("🍒", "🍋", "🔔", 100))          // loss
}

func TestGamesRepliesUnder400Chars(t *testing.T) {
	bank := &fakeBank{balance: 1000000000, balanceStatus: LoyaltyOK, wagerBalance: 2000000000, wagerStatus: LoyaltyOK}
	cmds := []Command{
		newGambleCommand(bank, stubRand(0)),
		newSlotsCommand(bank, seqRand(idx7, idx7, idx7)),
	}
	for _, cmd := range cmds {
		r := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"all"})
		assert.LessOrEqual(t, len(r.Text), 400, "command %q reply too long", cmd.Name)
		assert.False(t, strings.Contains(r.Text, "%!"), "format glitch in %q", cmd.Name)
	}
}

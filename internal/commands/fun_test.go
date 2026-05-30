package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRand returns a fixed int63 value, letting tests assert the exact
// output of the injectable-RNG commands.
func stubRand(v int64) func() int64 { return func() int64 { return v } }

func TestNewEightBallCommand_StubbedAnswer(t *testing.T) {
	// stub 0 -> first answer; mentionPrefix yields "@bob ".
	cmd := newEightBallCommand(stubRand(0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "🎱 @bob It is certain.", reply.Text)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewEightBallCommand_NoQuestionStillAnswers(t *testing.T) {
	cmd := newEightBallCommand(stubRand(9)) // index 9 -> "Reply hazy, try again."
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "🎱 @bob Reply hazy, try again.", reply.Text)
}

func TestNewEightBallCommand_Exported(t *testing.T) {
	cmd := NewEightBallCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"will", "i", "win?"})
	assert.True(t, strings.HasPrefix(reply.Text, "🎱 @bob "))
	assert.Equal(t, "8ball", cmd.Name)
}

func TestNewLurkCommand(t *testing.T) {
	cmd := NewLurkCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "@bob is now lurking in the shadows 👻 thanks for the support!", reply.Text)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewUnlurkCommand(t *testing.T) {
	cmd := NewUnlurkCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "@bob is back from the shadows! 👋", reply.Text)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewDiceCommand_StubbedDefault(t *testing.T) {
	// stub 3, span 6 -> 3%6 + 1 = 4.
	cmd := newDiceCommand(stubRand(3))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "@bob rolled a 🎲 4", reply.Text)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewDiceCommand_CustomSides(t *testing.T) {
	// stub 0, span 20 -> 0%20 + 1 = 1.
	cmd := newDiceCommand(stubRand(0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"20"})
	assert.Equal(t, "@bob rolled a 🎲 1", reply.Text)
}

func TestNewDiceCommand_InvalidSidesClampToDefault(t *testing.T) {
	cmd := newDiceCommand(stubRand(5))
	// non-numeric -> default 6; 5%6 + 1 = 6.
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"abc"})
	assert.Equal(t, "@bob rolled a 🎲 6", reply.Text)
	// below min (1 < 2) -> clamped to 2; 5%2 + 1 = 2.
	reply = cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"1"})
	assert.Equal(t, "@bob rolled a 🎲 2", reply.Text)
	// above max (9999 > 1000) -> clamped to 1000; 5%1000 + 1 = 6.
	reply = cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"9999"})
	assert.Equal(t, "@bob rolled a 🎲 6", reply.Text)
}

func TestNewDiceCommand_Exported(t *testing.T) {
	cmd := NewDiceCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.True(t, strings.HasPrefix(reply.Text, "@bob rolled a 🎲 "))
	assert.Equal(t, "dice", cmd.Name)
}

func TestNewRollCommand_StubbedDefault(t *testing.T) {
	// stub 41, default span 100 -> 41%100 + 1 = 42.
	cmd := newRollCommand(stubRand(41))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "@bob rolled 42 (1-100)", reply.Text)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewRollCommand_CustomSides(t *testing.T) {
	// stub 0, span 6 -> 0%6 + 1 = 1.
	cmd := newRollCommand(stubRand(0))
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"6"})
	assert.Equal(t, "@bob rolled 1 (1-6)", reply.Text)
}

func TestNewRollCommand_InvalidSidesClampToDefault(t *testing.T) {
	cmd := newRollCommand(stubRand(9))
	// non-numeric -> default 100; 9%100 + 1 = 10.
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"xyz"})
	assert.Equal(t, "@bob rolled 10 (1-100)", reply.Text)
}

func TestNewRollCommand_Exported(t *testing.T) {
	cmd := NewRollCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.True(t, strings.HasPrefix(reply.Text, "@bob rolled "))
	assert.Contains(t, reply.Text, "(1-100)")
	assert.Equal(t, "roll", cmd.Name)
}

func TestNewLoveCommand_Deterministic(t *testing.T) {
	cmd := NewLoveCommand()
	r1 := cmd.Handler(context.Background(), Message{Username: "alice"}, []string{"bob"})
	r2 := cmd.Handler(context.Background(), Message{Username: "alice"}, []string{"@bob"})
	assert.Equal(t, r1.Text, r2.Text)
	assert.Contains(t, r1.Text, "@alice loves bob ❤️ ")
	assert.Contains(t, r1.Text, "%")
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewLoveCommand_Usage(t *testing.T) {
	cmd := NewLoveCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "@bob usage: !love <name>", reply.Text)
}

func TestNewShipCommand_Deterministic(t *testing.T) {
	cmd := NewShipCommand()
	r1 := cmd.Handler(context.Background(), Message{Username: "mod"}, []string{"alice", "bob"})
	r2 := cmd.Handler(context.Background(), Message{Username: "mod"}, []string{"@alice", "@bob"})
	assert.Equal(t, r1.Text, r2.Text)
	// order-independence: ship(a,b) == ship(b,a) for the percentage.
	r3 := cmd.Handler(context.Background(), Message{Username: "mod"}, []string{"bob", "alice"})
	assert.Equal(t, pctOf(t, r1.Text), pctOf(t, r3.Text))
	assert.True(t, strings.HasPrefix(r1.Text, "💕 alice + bob = "))
	assert.Contains(t, r1.Text, "% compatible!")
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewShipCommand_Usage(t *testing.T) {
	cmd := NewShipCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"alice"})
	assert.Equal(t, "@bob usage: !ship <name1> <name2>", reply.Text)
}

func TestNewHugCommand(t *testing.T) {
	cmd := NewHugCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"@alice"})
	assert.Equal(t, "@bob hugs alice 🤗", reply.Text)
	self := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "@bob hugs themselves 🤗 aww.", self.Text)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestNewSlapCommand(t *testing.T) {
	cmd := NewSlapCommand()
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"@alice"})
	assert.Equal(t, "@bob slaps alice around a bit with a large trout 🐟", reply.Text)
	usage := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Equal(t, "@bob usage: !slap <name>", usage.Text)
	assert.Equal(t, RoleEveryone, cmd.MinRole)
}

func TestCompatibility_OrderIndependentAndStable(t *testing.T) {
	a := compatibility("Alice", "bob")
	b := compatibility("bob", "ALICE")
	require.Equal(t, a, b)
	assert.GreaterOrEqual(t, a, 0)
	assert.LessOrEqual(t, a, 100)
}

func TestFirstTarget(t *testing.T) {
	assert.Equal(t, "", firstTarget(nil))
	assert.Equal(t, "alice", firstTarget([]string{"@alice"}))
	assert.Equal(t, "alice", firstTarget([]string{" alice "}))
}

func TestParseSides(t *testing.T) {
	assert.Equal(t, 6, parseSides(nil, 6, 2, 1000))
	assert.Equal(t, 6, parseSides([]string{"abc"}, 6, 2, 1000))
	assert.Equal(t, 2, parseSides([]string{"1"}, 6, 2, 1000))
	assert.Equal(t, 1000, parseSides([]string{"5000"}, 6, 2, 1000))
	assert.Equal(t, 20, parseSides([]string{"20"}, 6, 2, 1000))
}

func TestFunRepliesUnder400Chars(t *testing.T) {
	cmds := []Command{
		NewEightBallCommand(), NewLurkCommand(), NewUnlurkCommand(),
		NewDiceCommand(), NewRollCommand(), NewLoveCommand(),
		NewShipCommand(), NewHugCommand(), NewSlapCommand(),
	}
	for _, cmd := range cmds {
		r := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"alice", "carol"})
		assert.LessOrEqual(t, len(r.Text), 400, "command %q reply too long", cmd.Name)
	}
}

// pctOf extracts the integer percentage embedded in a fun reply for
// order-independence assertions.
func pctOf(t *testing.T, text string) string {
	t.Helper()
	// Replies are like "...= 42% compatible!"; grab the token before '%'.
	i := strings.IndexByte(text, '%')
	require.GreaterOrEqual(t, i, 0)
	j := strings.LastIndexByte(text[:i], ' ')
	return text[j+1 : i]
}

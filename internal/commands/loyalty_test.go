package commands

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeLoyalty struct {
	bal        int64
	balStatus  LoyaltyError
	xferStatus LoyaltyError
	xferName   string
	top        []LoyaltyEntry
}

func (f fakeLoyalty) Balance(_ context.Context, _, _ string) (int64, LoyaltyError) {
	return f.bal, f.balStatus
}

func (f fakeLoyalty) Transfer(_ context.Context, _, _, _ string, _ int64) (LoyaltyError, string) {
	return f.xferStatus, f.xferName
}

func (f fakeLoyalty) Top(_ context.Context, _ string, _ int) []LoyaltyEntry {
	return f.top
}

func TestPoints_Balance(t *testing.T) {
	cmd := NewPointsCommand(fakeLoyalty{bal: 12345, balStatus: LoyaltyOK})
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Contains(t, reply.Text, "12,345")
	assert.Contains(t, reply.Text, "points")
}

func TestPoints_NoAccount(t *testing.T) {
	cmd := NewPointsCommand(fakeLoyalty{balStatus: LoyaltyNotFound})
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Contains(t, reply.Text, "0 points")
}

func TestPoints_NilProvider(t *testing.T) {
	reply := NewPointsCommand(nil).Handler(context.Background(), Message{Username: "bob"}, nil)
	assert.Contains(t, reply.Text, "unavailable")
}

func TestGive_Success(t *testing.T) {
	cmd := NewGiveCommand(fakeLoyalty{xferStatus: LoyaltyOK, xferName: "Alice"})
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"@alice", "100"})
	assert.Contains(t, reply.Text, "gave")
	assert.Contains(t, reply.Text, "Alice")
	assert.Contains(t, reply.Text, "100")
}

func TestGive_Usage(t *testing.T) {
	reply := NewGiveCommand(fakeLoyalty{}).Handler(context.Background(), Message{Username: "bob"}, []string{"@alice"})
	assert.Contains(t, reply.Text, "usage")
}

func TestGive_BadAmount(t *testing.T) {
	reply := NewGiveCommand(fakeLoyalty{}).Handler(context.Background(), Message{Username: "bob"}, []string{"@alice", "-5"})
	assert.Contains(t, reply.Text, "positive whole number")
}

func TestGive_Self(t *testing.T) {
	reply := NewGiveCommand(fakeLoyalty{}).Handler(context.Background(), Message{Username: "bob"}, []string{"@bob", "10"})
	assert.Contains(t, reply.Text, "yourself")
}

func TestGive_Insufficient(t *testing.T) {
	cmd := NewGiveCommand(fakeLoyalty{xferStatus: LoyaltyInsufficient})
	reply := cmd.Handler(context.Background(), Message{Username: "bob"}, []string{"@alice", "100"})
	assert.Contains(t, reply.Text, "don't have enough")
}

func TestPointsLeaderboard(t *testing.T) {
	cmd := NewPointsLeaderboardCommand(fakeLoyalty{top: []LoyaltyEntry{
		{Username: "alice", Balance: 500},
		{Username: "bob", Balance: 300},
	}})
	reply := cmd.Handler(context.Background(), Message{}, nil)
	assert.Contains(t, reply.Text, "alice")
	assert.Contains(t, reply.Text, "500")
	assert.Contains(t, reply.Text, "1.")
}

func TestPointsLeaderboard_Empty(t *testing.T) {
	reply := NewPointsLeaderboardCommand(fakeLoyalty{}).Handler(context.Background(), Message{}, nil)
	assert.Contains(t, reply.Text, "no points")
}

func TestFormatPoints(t *testing.T) {
	cases := map[int64]string{0: "0", 5: "5", 100: "100", 1000: "1,000", 12345: "12,345", 1000000: "1,000,000", -2500: "-2,500"}
	for in, want := range cases {
		assert.Equal(t, want, formatPoints(in), "n=%d", in)
	}
}

func TestLoyaltyCommands_MinRoleEveryone(t *testing.T) {
	assert.Equal(t, RoleEveryone, NewPointsCommand(nil).MinRole)
	assert.Equal(t, RoleEveryone, NewGiveCommand(nil).MinRole)
	assert.Equal(t, RoleEveryone, NewPointsLeaderboardCommand(nil).MinRole)
}

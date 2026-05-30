package commands

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeCatalog struct {
	addOutcome RewardOutcome
	delOutcome RewardOutcome
	getItem    RewardItem
	getOutcome RewardOutcome
	items      []RewardItem
	lastAdd    struct {
		channel, name, desc, by string
		cost                    int64
	}
}

func (f *fakeCatalog) Add(_ context.Context, channel, name string, cost int64, desc, by string) RewardOutcome {
	f.lastAdd.channel, f.lastAdd.name, f.lastAdd.cost, f.lastAdd.desc, f.lastAdd.by = channel, name, cost, desc, by
	return f.addOutcome
}
func (f *fakeCatalog) Remove(_ context.Context, _, _ string) RewardOutcome { return f.delOutcome }
func (f *fakeCatalog) Get(_ context.Context, _, _ string) (RewardItem, RewardOutcome) {
	return f.getItem, f.getOutcome
}
func (f *fakeCatalog) List(_ context.Context, _ string) []RewardItem { return f.items }

type fakeRedeemBank struct {
	status   LoyaltyError
	spentAmt int64
}

func (f *fakeRedeemBank) Spend(_ context.Context, _, _ string, amount int64) LoyaltyError {
	f.spentAmt = amount
	return f.status
}

type fakeRedeemSender struct {
	msg    string
	called bool
}

func (f *fakeRedeemSender) Send(_ context.Context, _, message string) error {
	f.called = true
	f.msg = message
	return nil
}

func TestReward_Add(t *testing.T) {
	cat := &fakeCatalog{addOutcome: RewardOK}
	cmd := NewRewardCommand(cat)
	reply := cmd.Handler(context.Background(), Message{Channel: "c", Username: "mod"}, []string{"add", "VIP", "500", "a", "shoutout"})
	assert.Contains(t, reply.Text, "added")
	assert.Equal(t, "vip", cat.lastAdd.name)
	assert.Equal(t, int64(500), cat.lastAdd.cost)
	assert.Equal(t, "a shoutout", cat.lastAdd.desc)
	assert.Equal(t, "mod", cat.lastAdd.by)
}

func TestReward_AddDuplicate(t *testing.T) {
	cmd := NewRewardCommand(&fakeCatalog{addOutcome: RewardExists})
	reply := cmd.Handler(context.Background(), Message{Channel: "c"}, []string{"add", "vip", "500", "x"})
	assert.Contains(t, reply.Text, "already exists")
}

func TestReward_AddBadCost(t *testing.T) {
	cmd := NewRewardCommand(&fakeCatalog{})
	reply := cmd.Handler(context.Background(), Message{}, []string{"add", "vip", "-5", "x"})
	assert.Contains(t, reply.Text, "positive whole number")
}

func TestReward_AddUsage(t *testing.T) {
	cmd := NewRewardCommand(&fakeCatalog{})
	reply := cmd.Handler(context.Background(), Message{}, []string{"add", "vip"})
	assert.Contains(t, reply.Text, "usage")
}

func TestReward_Del(t *testing.T) {
	cmd := NewRewardCommand(&fakeCatalog{delOutcome: RewardOK})
	reply := cmd.Handler(context.Background(), Message{}, []string{"del", "vip"})
	assert.Contains(t, reply.Text, "removed")
}

func TestReward_DelMissing(t *testing.T) {
	cmd := NewRewardCommand(&fakeCatalog{delOutcome: RewardNotFound})
	reply := cmd.Handler(context.Background(), Message{}, []string{"del", "vip"})
	assert.Contains(t, reply.Text, "no reward")
}

func TestReward_MinRoleModerator(t *testing.T) {
	assert.Equal(t, RoleModerator, NewRewardCommand(nil).MinRole)
}

func TestRewards_List(t *testing.T) {
	cmd := NewRewardsCommand(&fakeCatalog{items: []RewardItem{{Name: "vip", Cost: 500}, {Name: "song", Cost: 1000}}})
	reply := cmd.Handler(context.Background(), Message{Channel: "c"}, nil)
	assert.Contains(t, reply.Text, "vip")
	assert.Contains(t, reply.Text, "500")
	assert.Contains(t, reply.Text, "!redeem")
}

func TestRewards_Empty(t *testing.T) {
	reply := NewRewardsCommand(&fakeCatalog{}).Handler(context.Background(), Message{}, nil)
	assert.Contains(t, reply.Text, "no rewards")
}

func TestRewards_MinRoleEveryone(t *testing.T) {
	assert.Equal(t, RoleEveryone, NewRewardsCommand(nil).MinRole)
}

func TestRedeem_Success(t *testing.T) {
	cat := &fakeCatalog{getItem: RewardItem{Name: "vip", Cost: 500}, getOutcome: RewardOK}
	bank := &fakeRedeemBank{status: LoyaltyOK}
	sender := &fakeRedeemSender{}
	cmd := NewRedeemCommand(cat, bank, sender)
	reply := cmd.Handler(context.Background(), Message{Channel: "c", Username: "bob", UserID: "u1"}, []string{"vip"})
	assert.Contains(t, reply.Text, "redeemed")
	assert.Equal(t, int64(500), bank.spentAmt)
	assert.True(t, sender.called)
	assert.Contains(t, sender.msg, "bob")
	assert.Contains(t, sender.msg, "vip")
}

func TestRedeem_Insufficient(t *testing.T) {
	cat := &fakeCatalog{getItem: RewardItem{Name: "vip", Cost: 500}, getOutcome: RewardOK}
	cmd := NewRedeemCommand(cat, &fakeRedeemBank{status: LoyaltyInsufficient}, &fakeRedeemSender{})
	reply := cmd.Handler(context.Background(), Message{Username: "bob", UserID: "u1"}, []string{"vip"})
	assert.Contains(t, reply.Text, "need")
}

func TestRedeem_UnknownReward(t *testing.T) {
	cat := &fakeCatalog{getOutcome: RewardNotFound}
	cmd := NewRedeemCommand(cat, &fakeRedeemBank{}, &fakeRedeemSender{})
	reply := cmd.Handler(context.Background(), Message{}, []string{"nope"})
	assert.Contains(t, reply.Text, "no reward")
}

func TestRedeem_Usage(t *testing.T) {
	cmd := NewRedeemCommand(&fakeCatalog{}, &fakeRedeemBank{}, nil)
	reply := cmd.Handler(context.Background(), Message{}, nil)
	assert.Contains(t, reply.Text, "usage")
}

func TestRedeem_NilDeps(t *testing.T) {
	reply := NewRedeemCommand(nil, nil, nil).Handler(context.Background(), Message{}, []string{"vip"})
	assert.Contains(t, reply.Text, "unavailable")
}

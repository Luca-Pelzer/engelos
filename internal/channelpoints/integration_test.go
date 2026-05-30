package channelpoints

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/redemptions"
)

// openRealStore opens an in-memory redemptions store backed by the real
// SQLite implementation, isolated per test via a unique shared-cache name.
func openRealStore(t *testing.T, name string) redemptions.Store {
	t.Helper()
	store, err := redemptions.OpenSQLiteStore(
		context.Background(),
		"file:"+name+"?mode=memory&cache=shared",
		discardLogger(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestIntegration_ChatRedemption_RealStore exercises the full redemption
// path with the REAL redemptions store and REAL executor — only the Twitch
// boundary (chat + fulfiller) is faked. This is the closest we can get to a
// live affiliate redemption without an affiliate channel: it proves a stored
// binding is found, its templated chat action runs, and the redemption is
// fulfilled. The binding is created under the lower-cased channel because
// the executor normalises BroadcasterUserLogin to lower case.
func TestIntegration_ChatRedemption_RealStore(t *testing.T) {
	store := openRealStore(t, "cp_integ_chat")
	chat := &fakeChat{}
	ful := &fakeFulfiller{}

	evt := sampleEvent() // BroadcasterUserLogin "SomeChannel", RewardID "reward-1"
	channel := "somechannel"

	_, err := store.Create(context.Background(), testTenant, channel, redemptions.Binding{
		RewardID:    evt.RewardID,
		RewardTitle: "Hydrate",
		ActionType:  redemptions.ActionChatMessage,
		ActionParam: "$user redeemed $reward for $cost points: $input",
		Enabled:     true,
		AutoFulfill: true,
	})
	require.NoError(t, err)

	e := New(Config{
		TenantID:  testTenant,
		Store:     store,
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), evt)

	sent := chat.calls()
	require.Len(t, sent, 1)
	assert.Equal(t, channel, sent[0].channel)
	assert.Equal(t, "Viewer1 redeemed Hydrate for 500 points: hello there", sent[0].text)

	settled := ful.recorded()
	require.Len(t, settled, 1)
	assert.Equal(t, "fulfill", settled[0].op)
	assert.Equal(t, channel, settled[0].channel)
	assert.Equal(t, evt.RewardID, settled[0].rewardID)
	assert.Equal(t, evt.ID, settled[0].redemptionID)
}

// TestIntegration_CounterRedemption_RealStore proves a counter_increment
// binding routes through the real store to the counter admin and fulfils.
func TestIntegration_CounterRedemption_RealStore(t *testing.T) {
	store := openRealStore(t, "cp_integ_counter")
	counters := &fakeCounters{}
	ful := &fakeFulfiller{}

	evt := sampleEvent()
	channel := "somechannel"

	_, err := store.Create(context.Background(), testTenant, channel, redemptions.Binding{
		RewardID:    evt.RewardID,
		RewardTitle: "Add a death",
		ActionType:  redemptions.ActionCounterIncr,
		ActionParam: "deaths",
		Enabled:     true,
		AutoFulfill: true,
	})
	require.NoError(t, err)

	e := New(Config{
		TenantID:  testTenant,
		Store:     store,
		Counters:  counters,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), evt)

	recorded := counters.recorded()
	require.Len(t, recorded, 1)
	assert.Equal(t, "incr", recorded[0].op)
	assert.Equal(t, channel, recorded[0].channel)
	assert.Equal(t, "deaths", recorded[0].name)

	settled := ful.recorded()
	require.Len(t, settled, 1)
	assert.Equal(t, "fulfill", settled[0].op)
}

// TestIntegration_DisabledBinding_RealStore proves a disabled binding that
// physically exists in the real store is skipped: no action, no settle.
func TestIntegration_DisabledBinding_RealStore(t *testing.T) {
	store := openRealStore(t, "cp_integ_disabled")
	chat := &fakeChat{}
	ful := &fakeFulfiller{}

	evt := sampleEvent()
	channel := "somechannel"

	_, err := store.Create(context.Background(), testTenant, channel, redemptions.Binding{
		RewardID:    evt.RewardID,
		ActionType:  redemptions.ActionChatMessage,
		ActionParam: "ignored",
		Enabled:     false,
		AutoFulfill: true,
	})
	require.NoError(t, err)

	e := New(Config{
		TenantID:  testTenant,
		Store:     store,
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), evt)

	assert.Empty(t, chat.calls())
	assert.Empty(t, ful.recorded())
}

// TestIntegration_UnboundReward_RealStore proves a redemption for a reward
// with no binding in the real store is ignored without touching the
// redemption (no fulfill, no cancel) — exactly the "not ours to manage"
// contract Firebot-style bots rely on.
func TestIntegration_UnboundReward_RealStore(t *testing.T) {
	store := openRealStore(t, "cp_integ_unbound")
	chat := &fakeChat{}
	ful := &fakeFulfiller{}

	e := New(Config{
		TenantID:  testTenant,
		Store:     store,
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	assert.Empty(t, chat.calls())
	assert.Empty(t, ful.recorded())

	_, err := store.GetByReward(context.Background(), testTenant, "somechannel", "reward-1")
	assert.ErrorIs(t, err, redemptions.ErrNotFound)
}

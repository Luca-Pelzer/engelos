package channelpoints

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/adapters/twitch/eventsub"
	"github.com/Luca-Pelzer/engelos/internal/redemptions"
)

const testTenant = "tenant-1"

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeStore struct {
	binding redemptions.Binding
	err     error
}

func (s fakeStore) GetByReward(_ context.Context, _, _, _ string) (redemptions.Binding, error) {
	if s.err != nil {
		return redemptions.Binding{}, s.err
	}
	return s.binding, nil
}

type sentChat struct {
	channel string
	text    string
}

type fakeChat struct {
	mu   sync.Mutex
	sent []sentChat
	err  error
}

func (c *fakeChat) Send(_ context.Context, channel, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.sent = append(c.sent, sentChat{channel: channel, text: text})
	return nil
}

func (c *fakeChat) calls() []sentChat {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]sentChat, len(c.sent))
	copy(out, c.sent)
	return out
}

type counterCall struct {
	op      string
	channel string
	name    string
}

type fakeCounters struct {
	mu    sync.Mutex
	calls []counterCall
	err   error
}

func (c *fakeCounters) Increment(_ context.Context, channel, name string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, counterCall{op: "incr", channel: channel, name: name})
	if c.err != nil {
		return 0, c.err
	}
	return 1, nil
}

func (c *fakeCounters) Reset(_ context.Context, channel, name string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, counterCall{op: "reset", channel: channel, name: name})
	if c.err != nil {
		return 0, c.err
	}
	return 0, nil
}

func (c *fakeCounters) recorded() []counterCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]counterCall, len(c.calls))
	copy(out, c.calls)
	return out
}

type settleCall struct {
	op           string
	channel      string
	rewardID     string
	redemptionID string
}

type fakeFulfiller struct {
	mu    sync.Mutex
	calls []settleCall
}

func (f *fakeFulfiller) FulfillRedemption(_ context.Context, login, rewardID, redemptionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, settleCall{op: "fulfill", channel: login, rewardID: rewardID, redemptionID: redemptionID})
	return nil
}

func (f *fakeFulfiller) CancelRedemption(_ context.Context, login, rewardID, redemptionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, settleCall{op: "cancel", channel: login, rewardID: rewardID, redemptionID: redemptionID})
	return nil
}

func (f *fakeFulfiller) recorded() []settleCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]settleCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func sampleEvent() eventsub.RedemptionEvent {
	return eventsub.RedemptionEvent{
		ID:                   "red-7",
		BroadcasterUserLogin: "SomeChannel",
		UserLogin:            "viewer1",
		UserName:             "Viewer1",
		UserInput:            "hello there",
		RewardID:             "reward-1",
		RewardTitle:          "Hydrate",
		RewardCost:           500,
	}
}

func TestHandle_UnknownReward_NothingCalled(t *testing.T) {
	chat := &fakeChat{}
	counters := &fakeCounters{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID:  testTenant,
		Store:     fakeStore{err: redemptions.ErrNotFound},
		Chat:      chat,
		Counters:  counters,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	assert.Empty(t, chat.calls())
	assert.Empty(t, counters.recorded())
	assert.Empty(t, ful.recorded())
}

func TestHandle_DisabledBinding_ActionNotRun(t *testing.T) {
	chat := &fakeChat{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionChatMessage,
			ActionParam: "hi $user",
			Enabled:     false,
			AutoFulfill: true,
		}},
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	assert.Empty(t, chat.calls())
	assert.Empty(t, ful.recorded())
}

func TestHandle_ChatMessage_ExpandsTemplateAndFulfills(t *testing.T) {
	chat := &fakeChat{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			RewardTitle: "Hydrate",
			ActionType:  redemptions.ActionChatMessage,
			ActionParam: "$user redeemed $reward ($cost) saying: $input",
			Enabled:     true,
			AutoFulfill: true,
		}},
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	calls := chat.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "somechannel", calls[0].channel)
	assert.Equal(t, "Viewer1 redeemed Hydrate (500) saying: hello there", calls[0].text)

	settle := ful.recorded()
	require.Len(t, settle, 1)
	assert.Equal(t, "fulfill", settle[0].op)
	assert.Equal(t, "somechannel", settle[0].channel)
	assert.Equal(t, "reward-1", settle[0].rewardID)
	assert.Equal(t, "red-7", settle[0].redemptionID)
}

func TestHandle_ChatMessage_FallsBackToUserLogin(t *testing.T) {
	chat := &fakeChat{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionChatMessage,
			ActionParam: "thanks $user",
			Enabled:     true,
		}},
		Chat:   chat,
		Logger: discardLogger(),
	})

	evt := sampleEvent()
	evt.UserName = ""
	e.Handle(context.Background(), evt)

	calls := chat.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "thanks viewer1", calls[0].text)
}

func TestHandle_ChatNil_AutoFulfill_Cancels(t *testing.T) {
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionChatMessage,
			ActionParam: "hi $user",
			Enabled:     true,
			AutoFulfill: true,
		}},
		Chat:      nil,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	settle := ful.recorded()
	require.Len(t, settle, 1)
	assert.Equal(t, "cancel", settle[0].op)
	assert.Equal(t, "red-7", settle[0].redemptionID)
}

func TestHandle_CounterIncrement_FulfillsOnSuccess(t *testing.T) {
	counters := &fakeCounters{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionCounterIncr,
			ActionParam: "deaths",
			Enabled:     true,
			AutoFulfill: true,
		}},
		Counters:  counters,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	rec := counters.recorded()
	require.Len(t, rec, 1)
	assert.Equal(t, "incr", rec[0].op)
	assert.Equal(t, "somechannel", rec[0].channel)
	assert.Equal(t, "deaths", rec[0].name)

	settle := ful.recorded()
	require.Len(t, settle, 1)
	assert.Equal(t, "fulfill", settle[0].op)
}

func TestHandle_CounterReset_Success(t *testing.T) {
	counters := &fakeCounters{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionCounterReset,
			ActionParam: "wins",
			Enabled:     true,
			AutoFulfill: true,
		}},
		Counters:  counters,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	rec := counters.recorded()
	require.Len(t, rec, 1)
	assert.Equal(t, "reset", rec[0].op)
	assert.Equal(t, "wins", rec[0].name)
	require.Len(t, ful.recorded(), 1)
	assert.Equal(t, "fulfill", ful.recorded()[0].op)
}

func TestHandle_CounterError_AutoFulfill_Cancels(t *testing.T) {
	counters := &fakeCounters{err: errors.New("db down")}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionCounterIncr,
			ActionParam: "deaths",
			Enabled:     true,
			AutoFulfill: true,
		}},
		Counters:  counters,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	settle := ful.recorded()
	require.Len(t, settle, 1)
	assert.Equal(t, "cancel", settle[0].op)
}

func TestHandle_CounterNil_AutoFulfill_Cancels(t *testing.T) {
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionCounterIncr,
			ActionParam: "deaths",
			Enabled:     true,
			AutoFulfill: true,
		}},
		Counters:  nil,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	settle := ful.recorded()
	require.Len(t, settle, 1)
	assert.Equal(t, "cancel", settle[0].op)
}

func TestHandle_AutoFulfillFalse_NeitherFulfillNorCancel(t *testing.T) {
	chat := &fakeChat{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionChatMessage,
			ActionParam: "hi $user",
			Enabled:     true,
			AutoFulfill: false,
		}},
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	require.Len(t, chat.calls(), 1)
	assert.Empty(t, ful.recorded())
}

func TestHandle_NoneAction_FulfillsWhenAuto(t *testing.T) {
	chat := &fakeChat{}
	counters := &fakeCounters{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionNone,
			Enabled:     true,
			AutoFulfill: true,
		}},
		Chat:      chat,
		Counters:  counters,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	assert.Empty(t, chat.calls())
	assert.Empty(t, counters.recorded())
	settle := ful.recorded()
	require.Len(t, settle, 1)
	assert.Equal(t, "fulfill", settle[0].op)
}

func TestHandle_EmptyBroadcasterLogin_NothingCalled(t *testing.T) {
	chat := &fakeChat{}
	ful := &fakeFulfiller{}
	store := &recordingStore{}
	e := New(Config{
		TenantID:  testTenant,
		Store:     store,
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	evt := sampleEvent()
	evt.BroadcasterUserLogin = "   "
	e.Handle(context.Background(), evt)

	assert.Empty(t, chat.calls())
	assert.Empty(t, ful.recorded())
	assert.False(t, store.queried, "store must not be queried without a channel")
}

func TestHandle_EmptyExpandedTemplate_NoSendStillFulfills(t *testing.T) {
	chat := &fakeChat{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionChatMessage,
			ActionParam: "   ",
			Enabled:     true,
			AutoFulfill: true,
		}},
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	assert.Empty(t, chat.calls())
	settle := ful.recorded()
	require.Len(t, settle, 1)
	assert.Equal(t, "fulfill", settle[0].op)
}

func TestHandle_MessageTruncatedAt480(t *testing.T) {
	chat := &fakeChat{}
	long := strings.Repeat("a", 600)
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionChatMessage,
			ActionParam: long,
			Enabled:     true,
		}},
		Chat:   chat,
		Logger: discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	calls := chat.calls()
	require.Len(t, calls, 1)
	assert.Len(t, calls[0].text, 480)
}

func TestHandle_FulfillerNil_AutoFulfill_NoPanic(t *testing.T) {
	chat := &fakeChat{}
	e := New(Config{
		TenantID: testTenant,
		Store: fakeStore{binding: redemptions.Binding{
			RewardID:    "reward-1",
			ActionType:  redemptions.ActionChatMessage,
			ActionParam: "hi $user",
			Enabled:     true,
			AutoFulfill: true,
		}},
		Chat:      chat,
		Fulfiller: nil,
		Logger:    discardLogger(),
	})

	assert.NotPanics(t, func() {
		e.Handle(context.Background(), sampleEvent())
	})
	require.Len(t, chat.calls(), 1)
}

func TestHandle_StoreError_NothingSettled(t *testing.T) {
	chat := &fakeChat{}
	ful := &fakeFulfiller{}
	e := New(Config{
		TenantID:  testTenant,
		Store:     fakeStore{err: errors.New("boom")},
		Chat:      chat,
		Fulfiller: ful,
		Logger:    discardLogger(),
	})

	e.Handle(context.Background(), sampleEvent())

	assert.Empty(t, chat.calls())
	assert.Empty(t, ful.recorded())
}

type recordingStore struct {
	queried bool
}

func (s *recordingStore) GetByReward(_ context.Context, _, _, _ string) (redemptions.Binding, error) {
	s.queried = true
	return redemptions.Binding{}, redemptions.ErrNotFound
}

package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
	"github.com/Luca-Pelzer/engelos/internal/adapters/mock"
	"github.com/Luca-Pelzer/engelos/internal/runtime"
)

type fakePity struct {
	mu      sync.Mutex
	grants  []grantCall
	failErr error
}

type grantCall struct {
	TenantID, Channel, ViewerID, Username, Reason string
	Amount                                        int
}

func (f *fakePity) GrantPoints(_ context.Context, tenant, channel, viewer, username, reason string, amount int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return 0, f.failErr
	}
	f.grants = append(f.grants, grantCall{tenant, channel, viewer, username, reason, amount})
	return amount * len(f.grants), nil
}

func (f *fakePity) Grants() []grantCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]grantCall, len(f.grants))
	copy(out, f.grants)
	return out
}

type fakeBroadcaster struct {
	count atomic.Int64
}

func (b *fakeBroadcaster) Broadcast(_ string, _ any) { b.count.Add(1) }

type fakeStreak struct {
	mu      sync.Mutex
	calls   []streakCall
	outcome runtime.StreakOutcome
	failErr error
}

type streakCall struct {
	TenantID, Channel, ViewerID, Username string
}

func (f *fakeStreak) TickStreak(_ context.Context, tenant, channel, viewer, username string) (runtime.StreakOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, streakCall{tenant, channel, viewer, username})
	if f.failErr != nil {
		return runtime.StreakOutcome{}, f.failErr
	}
	return f.outcome, nil
}

func (f *fakeStreak) Calls() []streakCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]streakCall, len(f.calls))
	copy(out, f.calls)
	return out
}

type recordedEvent struct {
	Type    string
	Payload any
}

type recordingBroadcaster struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (b *recordingBroadcaster) Broadcast(eventType string, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, recordedEvent{Type: eventType, Payload: payload})
}

func (b *recordingBroadcaster) Events() []recordedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]recordedEvent, len(b.events))
	copy(out, b.events)
	return out
}

func (b *recordingBroadcaster) FilterByType(eventType string) []recordedEvent {
	all := b.Events()
	var out []recordedEvent
	for _, ev := range all {
		if ev.Type == eventType {
			out = append(out, ev)
		}
	}
	return out
}

func newDispatcher(t *testing.T, platforms []adapters.Platform, pity runtime.PityGranter, bcast runtime.Broadcaster) *runtime.Dispatcher {
	t.Helper()
	return runtime.New(runtime.Config{
		TenantID:         "test",
		Platforms:        platforms,
		Pity:             pity,
		PointsPerMessage: 1,
		Broadcaster:      bcast,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestDispatcher_MessageGrantsPity(t *testing.T) {
	t.Parallel()
	plat := mock.New("test-platform")
	require.NoError(t, plat.Connect(context.Background()))

	pity := &fakePity{}
	bcast := &fakeBroadcaster{}
	d := newDispatcher(t, []adapters.Platform{plat}, pity, bcast)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()

	plat.EmitEvent(adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   "test-platform",
		Channel:    "engelswtf",
		OccurredAt: time.Now(),
		Message: &adapters.MessageEvent{
			ID:       "irc-msg-1",
			UserID:   "user-42",
			Username: "alice",
			Content:  "hello chat",
		},
	})

	require.Eventually(t, func() bool {
		return len(pity.Grants()) == 1
	}, time.Second, 5*time.Millisecond)

	got := pity.Grants()[0]
	assert.Equal(t, "test", got.TenantID)
	assert.Equal(t, "engelswtf", got.Channel)
	assert.Equal(t, "user-42", got.ViewerID)
	assert.Equal(t, "alice", got.Username)
	assert.Equal(t, "chat:test-platform", got.Reason)
	assert.Equal(t, 1, got.Amount)
	assert.GreaterOrEqual(t, bcast.count.Load(), int64(1))

	cancel()
	require.NoError(t, plat.Disconnect(context.Background()))
	<-done

	stats := d.Stats()
	assert.EqualValues(t, 1, stats.Messages)
	assert.EqualValues(t, 0, stats.Subscriptions)
}

func TestDispatcher_NoPityWhenDisabled(t *testing.T) {
	t.Parallel()
	plat := mock.New("test")
	require.NoError(t, plat.Connect(context.Background()))

	pity := &fakePity{}
	d := runtime.New(runtime.Config{
		TenantID:         "test",
		Platforms:        []adapters.Platform{plat},
		Pity:             pity,
		PointsPerMessage: 0,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()

	plat.EmitEvent(adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   "test",
		Channel:    "c",
		OccurredAt: time.Now(),
		Message:    &adapters.MessageEvent{UserID: "u", Username: "u"},
	})

	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, pity.Grants(), "PointsPerMessage=0 must disable grants")

	cancel()
	require.NoError(t, plat.Disconnect(context.Background()))
	<-done
}

func TestDispatcher_CountsSubsAndRaids(t *testing.T) {
	t.Parallel()
	plat := mock.New("test")
	require.NoError(t, plat.Connect(context.Background()))

	d := newDispatcher(t, []adapters.Platform{plat}, &fakePity{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()

	for i := 0; i < 3; i++ {
		plat.EmitEvent(adapters.Event{
			ID:         adapters.NewEventID(),
			Type:       adapters.EventUserSubscribed,
			Platform:   "test",
			Channel:    "c",
			OccurredAt: time.Now(),
			Subscription: &adapters.SubscriptionEvent{
				UserID: "u", Username: "u", Tier: "1000",
			},
		})
	}
	plat.EmitEvent(adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventChannelRaided,
		Platform:   "test",
		Channel:    "c",
		OccurredAt: time.Now(),
		Raid:       &adapters.RaidEvent{FromUsername: "raider", ViewerCount: 100},
	})

	require.Eventually(t, func() bool {
		s := d.Stats()
		return s.Subscriptions == 3 && s.Raids == 1
	}, time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, plat.Disconnect(context.Background()))
	<-done
}

func TestDispatcher_RecordsPityErrors(t *testing.T) {
	t.Parallel()
	plat := mock.New("test")
	require.NoError(t, plat.Connect(context.Background()))

	pity := &fakePity{failErr: errors.New("simulated failure")}
	d := newDispatcher(t, []adapters.Platform{plat}, pity, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()

	plat.EmitEvent(adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   "test",
		Channel:    "c",
		OccurredAt: time.Now(),
		Message:    &adapters.MessageEvent{UserID: "u", Username: "u"},
	})

	require.Eventually(t, func() bool {
		return d.Stats().PityGrantErrors == 1
	}, time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, plat.Disconnect(context.Background()))
	<-done
}

func TestDispatcher_DoubleRunRejected(t *testing.T) {
	t.Parallel()
	d := runtime.New(runtime.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, d.Run(ctx))
	err := d.Run(context.Background())
	require.Error(t, err)
}

func TestDispatcher_StopsOnPlatformChannelClose(t *testing.T) {
	t.Parallel()
	plat := mock.New("test")
	require.NoError(t, plat.Connect(context.Background()))

	d := newDispatcher(t, []adapters.Platform{plat}, &fakePity{}, nil)
	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(context.Background()) }()

	require.NoError(t, plat.Disconnect(context.Background()))
	select {
	case err := <-runErr:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not stop after platform closed")
	}
}

func newStreakDispatcher(t *testing.T, plat adapters.Platform, streak runtime.StreakTicker, bcast runtime.Broadcaster) *runtime.Dispatcher {
	t.Helper()
	return runtime.New(runtime.Config{
		TenantID:         "test",
		Platforms:        []adapters.Platform{plat},
		Streak:           streak,
		PointsPerMessage: 0,
		Broadcaster:      bcast,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func emitChatMsg(plat interface {
	EmitEvent(adapters.Event)
}, platName, channel, userID, username string) {
	plat.EmitEvent(adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   platName,
		Channel:    channel,
		OccurredAt: time.Now(),
		Message: &adapters.MessageEvent{
			ID: "msg-1", UserID: userID, Username: username, Content: "hi",
		},
	})
}

func TestDispatcher_BroadcastsStreakMilestone(t *testing.T) {
	t.Parallel()
	plat := mock.New("twitch")
	require.NoError(t, plat.Connect(context.Background()))

	streak := &fakeStreak{outcome: runtime.StreakOutcome{
		DaysCurrent: 7, DaysLongest: 7, Milestone: 7,
	}}
	bcast := &recordingBroadcaster{}
	d := newStreakDispatcher(t, plat, streak, bcast)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()

	emitChatMsg(plat, "twitch", "engelswtf", "user-42", "alice")

	require.Eventually(t, func() bool {
		return len(bcast.FilterByType("feature.streak.milestone")) == 1
	}, time.Second, 5*time.Millisecond)

	evs := bcast.FilterByType("feature.streak.milestone")
	require.Len(t, evs, 1)
	buf, err := json.Marshal(evs[0].Payload)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(buf, &got))
	assert.Equal(t, "twitch", got["platform"])
	assert.Equal(t, "engelswtf", got["channel"])
	assert.Equal(t, "user-42", got["viewer_id"])
	assert.Equal(t, "alice", got["username"])
	assert.EqualValues(t, 7, got["days_current"])
	assert.EqualValues(t, 7, got["days_longest"])
	assert.EqualValues(t, 7, got["milestone"])

	cancel()
	require.NoError(t, plat.Disconnect(context.Background()))
	<-done
}

func TestDispatcher_BroadcastsStreakBroken(t *testing.T) {
	t.Parallel()
	plat := mock.New("twitch")
	require.NoError(t, plat.Connect(context.Background()))

	streak := &fakeStreak{outcome: runtime.StreakOutcome{
		DaysCurrent: 1, BrokenFromDays: 30,
	}}
	bcast := &recordingBroadcaster{}
	d := newStreakDispatcher(t, plat, streak, bcast)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()

	emitChatMsg(plat, "twitch", "engelswtf", "user-42", "alice")

	require.Eventually(t, func() bool {
		return len(bcast.FilterByType("feature.streak.broken")) == 1
	}, time.Second, 5*time.Millisecond)

	evs := bcast.FilterByType("feature.streak.broken")
	require.Len(t, evs, 1)
	buf, err := json.Marshal(evs[0].Payload)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(buf, &got))
	assert.EqualValues(t, 30, got["broken_from_days"])
	assert.EqualValues(t, 1, got["days_current"])
	assert.Equal(t, "engelswtf", got["channel"])

	cancel()
	require.NoError(t, plat.Disconnect(context.Background()))
	<-done
}

func TestDispatcher_NoStreakBroadcastOnNoOp(t *testing.T) {
	t.Parallel()
	plat := mock.New("twitch")
	require.NoError(t, plat.Connect(context.Background()))

	streak := &fakeStreak{outcome: runtime.StreakOutcome{
		DaysCurrent: 5, SameDayReTick: true,
	}}
	bcast := &recordingBroadcaster{}
	d := newStreakDispatcher(t, plat, streak, bcast)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()

	emitChatMsg(plat, "twitch", "engelswtf", "user-42", "alice")

	require.Eventually(t, func() bool {
		return len(streak.Calls()) == 1
	}, time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, bcast.FilterByType("feature.streak.milestone"))
	assert.Empty(t, bcast.FilterByType("feature.streak.broken"))

	cancel()
	require.NoError(t, plat.Disconnect(context.Background()))
	<-done
}

func TestDispatcher_StreakTickErrorStillCountsAndNoBroadcast(t *testing.T) {
	t.Parallel()
	plat := mock.New("twitch")
	require.NoError(t, plat.Connect(context.Background()))

	streak := &fakeStreak{failErr: errors.New("simulated streak failure")}
	bcast := &recordingBroadcaster{}
	d := newStreakDispatcher(t, plat, streak, bcast)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()

	emitChatMsg(plat, "twitch", "engelswtf", "user-42", "alice")

	require.Eventually(t, func() bool {
		return d.Stats().StreakTickErrors == 1
	}, time.Second, 5*time.Millisecond)

	assert.Empty(t, bcast.FilterByType("feature.streak.milestone"))
	assert.Empty(t, bcast.FilterByType("feature.streak.broken"))

	cancel()
	require.NoError(t, plat.Disconnect(context.Background()))
	<-done
}

func TestDispatcher_NoPlatformsIdle(t *testing.T) {
	t.Parallel()
	d := runtime.New(runtime.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	require.NoError(t, d.Run(ctx))
}

type fakeCommandRouter struct {
	mu      sync.Mutex
	calls   []runtime.CommandInvocation
	reply   runtime.CommandReply
	handled bool
}

func (f *fakeCommandRouter) Route(_ context.Context, inv runtime.CommandInvocation) (runtime.CommandReply, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, inv)
	return f.reply, f.handled
}

func (f *fakeCommandRouter) Calls() []runtime.CommandInvocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]runtime.CommandInvocation, len(f.calls))
	copy(out, f.calls)
	return out
}

func runDispatcherWithCommands(t *testing.T, plat *mock.Mock, router runtime.CommandRouter) (*runtime.Dispatcher, func()) {
	t.Helper()
	d := runtime.New(runtime.Config{
		TenantID:  "test",
		Platforms: []adapters.Platform{plat},
		Commands:  router,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()
	return d, func() {
		cancel()
		require.NoError(t, plat.Disconnect(context.Background()))
		<-done
	}
}

func emitMessage(plat *mock.Mock, content string) {
	plat.EmitEvent(adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   "test-platform",
		Channel:    "engelswtf",
		OccurredAt: time.Now(),
		Message: &adapters.MessageEvent{
			ID:       "irc-msg",
			UserID:   "user-7",
			Username: "bob",
			Content:  content,
		},
	})
}

func TestDispatcher_CommandReplySent(t *testing.T) {
	t.Parallel()
	plat := mock.New("test-platform")
	require.NoError(t, plat.Connect(context.Background()))
	router := &fakeCommandRouter{reply: runtime.CommandReply{Text: "@bob you have 5 pity points"}, handled: true}
	_, stop := runDispatcherWithCommands(t, plat, router)
	defer stop()

	emitMessage(plat, "!pity")

	require.Eventually(t, func() bool {
		return len(plat.Actions()) == 1
	}, time.Second, 5*time.Millisecond)

	act := plat.Actions()[0]
	assert.Equal(t, adapters.ActionSendMessage, act.Type)
	assert.Equal(t, "engelswtf", act.Channel)
	require.NotNil(t, act.SendMessage)
	assert.Equal(t, "@bob you have 5 pity points", act.SendMessage.Text)

	calls := router.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "!pity", calls[0].Text)
	assert.Equal(t, "user-7", calls[0].UserID)
	assert.Equal(t, "bob", calls[0].Username)
	assert.Equal(t, "engelswtf", calls[0].Channel)
}

func TestDispatcher_NonCommandSendsNothing(t *testing.T) {
	t.Parallel()
	plat := mock.New("test-platform")
	require.NoError(t, plat.Connect(context.Background()))
	router := &fakeCommandRouter{handled: false}
	_, stop := runDispatcherWithCommands(t, plat, router)
	defer stop()

	emitMessage(plat, "just a normal chat message")

	require.Eventually(t, func() bool {
		return len(router.Calls()) == 1
	}, time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	assert.Empty(t, plat.Actions(), "non-command must not trigger a send")
}

func TestDispatcher_HandledEmptyReplySendsNothing(t *testing.T) {
	t.Parallel()
	plat := mock.New("test-platform")
	require.NoError(t, plat.Connect(context.Background()))
	router := &fakeCommandRouter{reply: runtime.CommandReply{Text: ""}, handled: true}
	_, stop := runDispatcherWithCommands(t, plat, router)
	defer stop()

	emitMessage(plat, "!quiet")

	require.Eventually(t, func() bool {
		return len(router.Calls()) == 1
	}, time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	assert.Empty(t, plat.Actions(), "empty reply must not trigger a send")
}

package runtime_test

import (
	"context"
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

package mock_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/engelswtf/engelos/internal/adapters"
	"github.com/engelswtf/engelos/internal/adapters/mock"
)

func sampleMessageEvent(text string) adapters.Event {
	return adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       adapters.EventMessageCreated,
		Platform:   "twitch",
		Channel:    "engelswtf",
		OccurredAt: time.Now().UTC(),
		Message: &adapters.MessageEvent{
			ID:       "msg-1",
			UserID:   "u-1",
			Username: "alice",
			Content:  text,
		},
	}
}

func TestConnectDisconnectLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")

	require.Equal(t, "twitch", m.Name())
	require.ErrorIs(t, m.Health(), mock.ErrNotConnected)

	require.NoError(t, m.Connect(ctx))
	require.NoError(t, m.Health())

	require.ErrorIs(t, m.Connect(ctx), mock.ErrAlreadyConnected)

	require.NoError(t, m.Disconnect(ctx))
	require.ErrorIs(t, m.Health(), mock.ErrNotConnected)

	require.NoError(t, m.Disconnect(ctx))
}

func TestEmitEventDelivers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")
	require.NoError(t, m.Connect(ctx))
	t.Cleanup(func() { _ = m.Disconnect(ctx) })

	want := sampleMessageEvent("hi")
	m.EmitEvent(want)

	select {
	case got := <-m.Events():
		assert.Equal(t, want.ID, got.ID)
		require.NotNil(t, got.Message)
		assert.Equal(t, "hi", got.Message.Content)
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
}

func TestDoRecordsActions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")
	require.NoError(t, m.Connect(ctx))
	t.Cleanup(func() { _ = m.Disconnect(ctx) })

	a1 := adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     "engelswtf",
		SendMessage: &adapters.SendMessageAction{Text: "hello"},
	}
	a2 := adapters.Action{
		Type:    adapters.ActionTimeout,
		Channel: "engelswtf",
		Timeout: &adapters.TimeoutAction{UserID: "u-1", Duration: 10 * time.Second},
	}

	require.NoError(t, m.Do(ctx, a1))
	require.NoError(t, m.Do(ctx, a2))

	actions := m.Actions()
	require.Len(t, actions, 2)
	assert.Equal(t, adapters.ActionSendMessage, actions[0].Type)
	assert.Equal(t, "hello", actions[0].SendMessage.Text)
	assert.Equal(t, adapters.ActionTimeout, actions[1].Type)

	actions[0].Channel = "tampered"
	assert.Equal(t, "engelswtf", m.Actions()[0].Channel, "Actions must return a copy")
}

func TestDoFailsWhenDisconnected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")

	err := m.Do(ctx, adapters.Action{Type: adapters.ActionSendMessage})
	assert.ErrorIs(t, err, mock.ErrNotConnected)
}

func TestSimulateErrorIsOneShot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")
	require.NoError(t, m.Connect(ctx))
	t.Cleanup(func() { _ = m.Disconnect(ctx) })

	boom := errors.New("rate limited")
	m.SimulateError(boom)

	action := adapters.Action{
		Type:        adapters.ActionSendMessage,
		Channel:     "engelswtf",
		SendMessage: &adapters.SendMessageAction{Text: "x"},
	}
	assert.ErrorIs(t, m.Do(ctx, action), boom)
	assert.Empty(t, m.Actions(), "failed action should not be recorded")

	require.NoError(t, m.Do(ctx, action))
	assert.Len(t, m.Actions(), 1)
}

func TestDoHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	m := mock.New("twitch")
	require.NoError(t, m.Connect(context.Background()))
	t.Cleanup(func() { _ = m.Disconnect(context.Background()) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Do(ctx, adapters.Action{Type: adapters.ActionSendMessage})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestForceDisconnectClosesEventsChannel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")
	require.NoError(t, m.Connect(ctx))

	events := m.Events()
	m.ForceDisconnect()

	select {
	case _, ok := <-events:
		assert.False(t, ok, "channel must be closed after ForceDisconnect")
	case <-time.After(time.Second):
		t.Fatal("Events channel was not closed")
	}

	assert.ErrorIs(t, m.Health(), mock.ErrNotConnected)
	m.ForceDisconnect()
}

func TestSetHealth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")
	require.NoError(t, m.Connect(ctx))
	t.Cleanup(func() { _ = m.Disconnect(ctx) })

	require.NoError(t, m.Health())

	flaky := errors.New("flaky network")
	m.SetHealth(flaky)
	assert.ErrorIs(t, m.Health(), flaky)

	m.SetHealth(nil)
	assert.NoError(t, m.Health())
}

func TestEmitEventAfterDisconnectPanics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")
	require.NoError(t, m.Connect(ctx))
	require.NoError(t, m.Disconnect(ctx))

	assert.PanicsWithValue(t, mock.ErrEmitAfterShutdown, func() {
		m.EmitEvent(sampleMessageEvent("late"))
	})
}

func TestConcurrentEmitAndConsume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := mock.New("twitch")
	require.NoError(t, m.Connect(ctx))

	const producers = 8
	const perProducer = 50
	total := producers * perProducer

	var wg sync.WaitGroup
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				m.EmitEvent(sampleMessageEvent("p"))
				_ = m.Do(ctx, adapters.Action{
					Type:        adapters.ActionSendMessage,
					Channel:     "engelswtf",
					SendMessage: &adapters.SendMessageAction{Text: "ack"},
				})
			}
		}(i)
	}

	received := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range m.Events() {
			received++
			if received == total {
				return
			}
		}
	}()

	wg.Wait()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("expected %d events, only saw %d", total, received)
	}

	require.NoError(t, m.Disconnect(ctx))
	assert.Equal(t, total, received)
	assert.Len(t, m.Actions(), total)
}

func TestImplementsPlatformInterface(t *testing.T) {
	t.Parallel()
	var _ adapters.Platform = mock.New("any")
}

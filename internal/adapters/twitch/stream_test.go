package twitch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

func TestEmitStreamEvent_OnlineNormalizes(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	started := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	a.EmitStreamEvent(true, "#Broadcaster", started)

	select {
	case e := <-a.Events():
		require.Equal(t, adapters.EventStreamOnline, e.Type)
		assert.Equal(t, "twitch", e.Platform)
		assert.Equal(t, "broadcaster", e.Channel)
		require.NotNil(t, e.Stream)
		assert.True(t, e.Stream.IsLive)
		assert.Equal(t, started, e.Stream.StartedAt.UTC())
	case <-time.After(time.Second):
		t.Fatal("expected stream.online event on Events channel")
	}
}

func TestEmitStreamEvent_OfflineNormalizes(t *testing.T) {
	a, fake, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })
	<-fake.connectStarted

	a.EmitStreamEvent(false, "broadcaster", time.Time{})

	select {
	case e := <-a.Events():
		require.Equal(t, adapters.EventStreamOffline, e.Type)
		assert.Equal(t, "broadcaster", e.Channel)
		require.NotNil(t, e.Stream)
		assert.False(t, e.Stream.IsLive)
	case <-time.After(time.Second):
		t.Fatal("expected stream.offline event on Events channel")
	}
}

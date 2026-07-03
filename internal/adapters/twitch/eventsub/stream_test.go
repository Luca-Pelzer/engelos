package eventsub

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
)

func streamOnlineMsg() string {
	return `{"metadata":{"message_id":"s-1","message_type":"notification","message_timestamp":"2026-05-30T12:00:05Z","subscription_type":"stream.online"},"payload":{"event":{"id":"stream-1","broadcaster_user_id":"broadcaster-1","broadcaster_user_login":"somechannel","type":"live","started_at":"2026-05-30T12:00:00Z"}}}`
}

func streamOfflineMsg() string {
	return `{"metadata":{"message_id":"s-2","message_type":"notification","message_timestamp":"2026-05-30T12:05:00Z","subscription_type":"stream.offline"},"payload":{"event":{"broadcaster_user_id":"broadcaster-1","broadcaster_user_login":"somechannel"}}}`
}

func recvStreamEvent(t *testing.T, ch <-chan StreamEvent) StreamEvent {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream event")
		return StreamEvent{}
	}
}

func TestStreamOnlineNotification(t *testing.T) {
	_, url := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		writeFrame(ctx, conn, welcomeMsg("sess-stream"))
		writeFrame(ctx, conn, streamOnlineMsg())
		blockUntilClosed(ctx, conn)
	})

	events := make(chan StreamEvent, 1)
	c := New(Config{
		URL:           url,
		Dialer:        passthroughDialer,
		Logger:        discardLogger(),
		StreamHandler: func(_ context.Context, e StreamEvent) { events <- e },
	})
	runClient(t, c)

	evt := recvStreamEvent(t, events)
	assert.True(t, evt.Online)
	assert.Equal(t, "broadcaster-1", evt.BroadcasterUserID)
	assert.Equal(t, "somechannel", evt.BroadcasterUserLogin)
	assert.Equal(t, time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC), evt.StartedAt.UTC())
}

func TestStreamOfflineNotification(t *testing.T) {
	_, url := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		writeFrame(ctx, conn, welcomeMsg("sess-stream-off"))
		writeFrame(ctx, conn, streamOfflineMsg())
		blockUntilClosed(ctx, conn)
	})

	events := make(chan StreamEvent, 1)
	c := New(Config{
		URL:           url,
		Dialer:        passthroughDialer,
		Logger:        discardLogger(),
		StreamHandler: func(_ context.Context, e StreamEvent) { events <- e },
	})
	runClient(t, c)

	evt := recvStreamEvent(t, events)
	assert.False(t, evt.Online)
	assert.Equal(t, "somechannel", evt.BroadcasterUserLogin)
}

package eventsub

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func passthroughDialer(ctx context.Context, url string) (*websocket.Conn, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	return conn, err
}

// newEventSubServer spins up a fake EventSub WS endpoint. script drives each
// accepted connection; connNum is 1 for the first connection, incremented per
// accept so tests can vary behaviour across reconnects.
func newEventSubServer(t *testing.T, script func(ctx context.Context, conn *websocket.Conn, connNum int)) (*httptest.Server, string) {
	t.Helper()
	var accepted atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		num := int(accepted.Add(1))
		script(r.Context(), conn, num)
	}))
	t.Cleanup(srv.Close)
	return srv, wsURL(srv.URL)
}

func writeFrame(ctx context.Context, conn *websocket.Conn, payload string) {
	_ = conn.Write(ctx, websocket.MessageText, []byte(payload))
}

// blockUntilClosed keeps the server side alive so the client can read the
// frames already written, returning when the client closes or ctx is done.
func blockUntilClosed(ctx context.Context, conn *websocket.Conn) {
	_, _, _ = conn.Read(ctx)
}

func welcomeMsg(sessionID string) string {
	return fmt.Sprintf(`{"metadata":{"message_id":"w-1","message_type":"session_welcome","message_timestamp":"2026-05-30T12:00:00Z"},"payload":{"session":{"id":%q,"status":"connected","keepalive_timeout_seconds":10}}}`, sessionID)
}

func keepaliveMsg() string {
	return `{"metadata":{"message_id":"k-1","message_type":"session_keepalive","message_timestamp":"2026-05-30T12:00:01Z"},"payload":{}}`
}

func reconnectMsg(reconnectURL string) string {
	return fmt.Sprintf(`{"metadata":{"message_id":"r-1","message_type":"session_reconnect","message_timestamp":"2026-05-30T12:00:02Z"},"payload":{"session":{"id":"sess-1","status":"reconnecting","reconnect_url":%q,"keepalive_timeout_seconds":10}}}`, reconnectURL)
}

func redemptionMsg() string {
	return `{"metadata":{"message_id":"n-1","message_type":"notification","message_timestamp":"2026-05-30T12:00:03Z","subscription_type":"channel.channel_points_custom_reward_redemption.add"},"payload":{"event":{"id":"redemption-uuid-1","broadcaster_user_id":"broadcaster-1","broadcaster_user_login":"somechannel","user_id":"user-1","user_login":"viewer1","user_name":"Viewer1","user_input":"please play","status":"unfulfilled","reward":{"id":"reward-1","title":"Hydrate","cost":500},"redeemed_at":"2026-05-30T12:00:00Z"}}}`
}

func nonRedemptionMsg() string {
	return `{"metadata":{"message_id":"n-2","message_type":"notification","message_timestamp":"2026-05-30T12:00:04Z","subscription_type":"channel.follow"},"payload":{"event":{"user_id":"x"}}}`
}

func recvEvent(t *testing.T, ch <-chan RedemptionEvent) RedemptionEvent {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for redemption event")
		return RedemptionEvent{}
	}
}

func recvString(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session id")
		return ""
	}
}

func runClient(t *testing.T, c *Client) (context.Context, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	return ctx, cancel, done
}

func TestHappyPath(t *testing.T) {
	_, url := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		writeFrame(ctx, conn, welcomeMsg("sess-happy"))
		writeFrame(ctx, conn, redemptionMsg())
		blockUntilClosed(ctx, conn)
	})

	sessions := make(chan string, 1)
	events := make(chan RedemptionEvent, 1)
	c := New(Config{
		URL:    url,
		Dialer: passthroughDialer,
		Logger: discardLogger(),
		OnSession: func(_ context.Context, id string) error {
			sessions <- id
			return nil
		},
		Handler: func(_ context.Context, e RedemptionEvent) { events <- e },
	})
	runClient(t, c)

	assert.Equal(t, "sess-happy", recvString(t, sessions))

	evt := recvEvent(t, events)
	assert.Equal(t, "redemption-uuid-1", evt.ID)
	assert.Equal(t, "broadcaster-1", evt.BroadcasterUserID)
	assert.Equal(t, "somechannel", evt.BroadcasterUserLogin)
	assert.Equal(t, "user-1", evt.UserID)
	assert.Equal(t, "viewer1", evt.UserLogin)
	assert.Equal(t, "Viewer1", evt.UserName)
	assert.Equal(t, "please play", evt.UserInput)
	assert.Equal(t, "unfulfilled", evt.Status)
	assert.Equal(t, "reward-1", evt.RewardID)
	assert.Equal(t, "Hydrate", evt.RewardTitle)
	assert.Equal(t, 500, evt.RewardCost)
	assert.Equal(t, time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC), evt.RedeemedAt.UTC())
}

func TestKeepaliveIgnored(t *testing.T) {
	_, url := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		writeFrame(ctx, conn, welcomeMsg("sess-keepalive"))
		writeFrame(ctx, conn, keepaliveMsg())
		writeFrame(ctx, conn, redemptionMsg())
		blockUntilClosed(ctx, conn)
	})

	events := make(chan RedemptionEvent, 4)
	c := New(Config{
		URL:     url,
		Dialer:  passthroughDialer,
		Logger:  discardLogger(),
		Handler: func(_ context.Context, e RedemptionEvent) { events <- e },
	})
	runClient(t, c)

	evt := recvEvent(t, events)
	assert.Equal(t, "redemption-uuid-1", evt.ID)

	select {
	case e := <-events:
		t.Fatalf("unexpected extra event from keepalive: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNonRedemptionNotificationIgnored(t *testing.T) {
	_, url := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		writeFrame(ctx, conn, welcomeMsg("sess-other"))
		writeFrame(ctx, conn, nonRedemptionMsg())
		writeFrame(ctx, conn, redemptionMsg())
		blockUntilClosed(ctx, conn)
	})

	events := make(chan RedemptionEvent, 4)
	c := New(Config{
		URL:     url,
		Dialer:  passthroughDialer,
		Logger:  discardLogger(),
		Handler: func(_ context.Context, e RedemptionEvent) { events <- e },
	})
	runClient(t, c)

	evt := recvEvent(t, events)
	require.Equal(t, "redemption-uuid-1", evt.ID, "only the redemption notification should reach the handler")

	select {
	case e := <-events:
		t.Fatalf("non-redemption notification leaked to handler: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestOnSessionErrorTriggersReconnect(t *testing.T) {
	_, url := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, num int) {
		writeFrame(ctx, conn, welcomeMsg(fmt.Sprintf("sess-%d", num)))
		writeFrame(ctx, conn, redemptionMsg())
		blockUntilClosed(ctx, conn)
	})

	var calls atomic.Int64
	events := make(chan RedemptionEvent, 1)
	c := New(Config{
		URL:                 url,
		Dialer:              passthroughDialer,
		Logger:              discardLogger(),
		ReconnectMinBackoff: time.Millisecond,
		ReconnectMaxBackoff: time.Millisecond,
		OnSession: func(_ context.Context, _ string) error {
			if calls.Add(1) == 1 {
				return fmt.Errorf("first session rejected")
			}
			return nil
		},
		Handler: func(_ context.Context, e RedemptionEvent) { events <- e },
	})
	runClient(t, c)

	evt := recvEvent(t, events)
	assert.Equal(t, "redemption-uuid-1", evt.ID)
	assert.GreaterOrEqual(t, calls.Load(), int64(2), "OnSession must be retried after the first failure")
}

func TestSessionReconnectHandoff(t *testing.T) {
	_, secondURL := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		writeFrame(ctx, conn, welcomeMsg("sess-second"))
		writeFrame(ctx, conn, redemptionMsg())
		blockUntilClosed(ctx, conn)
	})

	_, firstURL := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		writeFrame(ctx, conn, welcomeMsg("sess-first"))
		writeFrame(ctx, conn, reconnectMsg(secondURL))
		blockUntilClosed(ctx, conn)
	})

	var sessionCalls atomic.Int64
	events := make(chan RedemptionEvent, 1)
	c := New(Config{
		URL:    firstURL,
		Dialer: passthroughDialer,
		Logger: discardLogger(),
		OnSession: func(_ context.Context, _ string) error {
			sessionCalls.Add(1)
			return nil
		},
		Handler: func(_ context.Context, e RedemptionEvent) { events <- e },
	})
	runClient(t, c)

	evt := recvEvent(t, events)
	assert.Equal(t, "redemption-uuid-1", evt.ID, "event from the reconnect target must be delivered")
	assert.Equal(t, int64(1), sessionCalls.Load(), "OnSession must NOT be re-invoked on a session_reconnect handoff")
}

func TestContextCancellationStopsRun(t *testing.T) {
	_, url := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		writeFrame(ctx, conn, welcomeMsg("sess-cancel"))
		blockUntilClosed(ctx, conn)
	})

	ready := make(chan struct{}, 1)
	c := New(Config{
		URL:    url,
		Dialer: passthroughDialer,
		Logger: discardLogger(),
		OnSession: func(_ context.Context, _ string) error {
			select {
			case ready <- struct{}{}:
			default:
			}
			return nil
		},
		Handler: func(context.Context, RedemptionEvent) {},
	})
	_, cancel, done := runClient(t, c)

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("client never established a session")
	}

	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}
}

func TestDroppedConnectionRedials(t *testing.T) {
	_, url := newEventSubServer(t, func(ctx context.Context, conn *websocket.Conn, num int) {
		writeFrame(ctx, conn, welcomeMsg(fmt.Sprintf("sess-%d", num)))
		if num == 1 {
			_ = conn.Close(websocket.StatusNormalClosure, "drop")
			return
		}
		writeFrame(ctx, conn, redemptionMsg())
		blockUntilClosed(ctx, conn)
	})

	var calls atomic.Int64
	events := make(chan RedemptionEvent, 1)
	c := New(Config{
		URL:                 url,
		Dialer:              passthroughDialer,
		Logger:              discardLogger(),
		ReconnectMinBackoff: time.Millisecond,
		ReconnectMaxBackoff: time.Millisecond,
		OnSession: func(_ context.Context, _ string) error {
			calls.Add(1)
			return nil
		},
		Handler: func(_ context.Context, e RedemptionEvent) { events <- e },
	})
	runClient(t, c)

	evt := recvEvent(t, events)
	assert.Equal(t, "redemption-uuid-1", evt.ID)
	assert.GreaterOrEqual(t, calls.Load(), int64(2), "a dropped connection must redial and re-run OnSession")
}

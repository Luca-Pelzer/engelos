package ws_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/engelswtf/engelos/internal/api/ws"
)

func newHubServer(t *testing.T) (*ws.Hub, *httptest.Server, context.CancelFunc) {
	t.Helper()
	hub := ws.NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	ts := httptest.NewServer(hub)
	t.Cleanup(ts.Close)
	t.Cleanup(cancel)
	return hub, ts, cancel
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func TestHub_ConnectAndEcho(t *testing.T) {
	t.Parallel()
	_, ts, _ := newHubServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(ts.URL), nil)
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "")

	payload := []byte("hello engelos")
	require.NoError(t, conn.Write(ctx, websocket.MessageText, payload))

	mt, data, err := conn.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, mt)
	assert.Equal(t, payload, data)
}

func TestHub_BroadcastFanOut(t *testing.T) {
	t.Parallel()
	hub, ts, _ := newHubServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const n = 3
	conns := make([]*websocket.Conn, n)
	for i := range conns {
		c, _, err := websocket.Dial(ctx, wsURL(ts.URL), nil)
		require.NoError(t, err)
		conns[i] = c
		defer c.Close(websocket.StatusNormalClosure, "")
	}

	require.Eventually(t, func() bool {
		return hub.ConnCount() == int64(n)
	}, 2*time.Second, 10*time.Millisecond)

	msg := []byte("broadcast-payload")
	hub.Broadcast(msg)

	for i, c := range conns {
		mt, data, err := c.Read(ctx)
		require.NoError(t, err, "conn %d", i)
		assert.Equal(t, websocket.MessageText, mt)
		assert.Equal(t, msg, data, "conn %d", i)
	}
}

func TestHub_DisconnectDecrementsCount(t *testing.T) {
	t.Parallel()
	hub, ts, _ := newHubServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(ts.URL), nil)
	require.NoError(t, err)

	require.Eventually(t, func() bool { return hub.ConnCount() == 1 },
		2*time.Second, 10*time.Millisecond)

	_ = conn.Close(websocket.StatusNormalClosure, "bye")

	require.Eventually(t, func() bool { return hub.ConnCount() == 0 },
		2*time.Second, 10*time.Millisecond)
}

func TestHub_HubShutdownClosesClients(t *testing.T) {
	t.Parallel()
	hub := ws.NewHub(nil)
	hubCtx, hubCancel := context.WithCancel(context.Background())
	go hub.Run(hubCtx)
	ts := httptest.NewServer(hub)
	defer ts.Close()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()

	conn, _, err := websocket.Dial(dialCtx, wsURL(ts.URL), nil)
	require.NoError(t, err)
	defer conn.Close(websocket.StatusInternalError, "")

	require.Eventually(t, func() bool { return hub.ConnCount() == 1 },
		2*time.Second, 10*time.Millisecond)

	hubCancel()

	require.Eventually(t, func() bool {
		readCtx, c := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer c()
		_, _, err := conn.Read(readCtx)
		return err != nil
	}, 2*time.Second, 50*time.Millisecond)
}

func TestHub_BroadcastBeforeAnyClients(t *testing.T) {
	t.Parallel()
	hub := ws.NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	hub.Broadcast([]byte("nobody-here"))
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, int64(0), hub.ConnCount())
}

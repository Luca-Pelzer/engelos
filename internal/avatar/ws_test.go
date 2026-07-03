package avatar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// wsTestRig bundles a hub, a real token store and an httptest server fronting
// the WSHandler, so tests dial it with a genuine WebSocket client.
type wsTestRig struct {
	hub    *Hub
	store  TokenStore
	server *httptest.Server
	token  string
}

func newWSTestRig(t *testing.T) *wsTestRig {
	t.Helper()
	hub := NewHub(nil)
	store := openTestStore(t)
	token, err := store.GetOrCreate(context.Background(), "tenant", "chan")
	require.NoError(t, err)

	h := NewWSHandler(hub, store, "tenant", nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &wsTestRig{hub: hub, store: store, server: srv, token: token}
}

func (r *wsTestRig) wsURL(query string) string {
	return "ws" + strings.TrimPrefix(r.server.URL, "http") + "/" + query
}

func dial(t *testing.T, url string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, url, nil)
}

func TestWSRejectsMissingToken(t *testing.T) {
	rig := newWSTestRig(t)
	conn, resp, err := dial(t, rig.wsURL("?channel=chan"))
	require.Error(t, err)
	if conn != nil {
		_ = conn.Close(websocket.StatusInternalError, "")
	}
	require.NotNil(t, resp)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestWSRejectsBadToken(t *testing.T) {
	rig := newWSTestRig(t)
	conn, resp, err := dial(t, rig.wsURL("?channel=chan&token=deadbeef"))
	require.Error(t, err)
	if conn != nil {
		_ = conn.Close(websocket.StatusInternalError, "")
	}
	require.NotNil(t, resp)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestWSRejectsMissingChannel(t *testing.T) {
	rig := newWSTestRig(t)
	conn, resp, err := dial(t, rig.wsURL("?token="+rig.token))
	require.Error(t, err)
	if conn != nil {
		_ = conn.Close(websocket.StatusInternalError, "")
	}
	require.NotNil(t, resp)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestWSDeliversBroadcastFrames(t *testing.T) {
	rig := newWSTestRig(t)
	conn, _, err := dial(t, rig.wsURL("?channel=chan&token="+rig.token))
	require.NoError(t, err)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Wait for the handler to register the subscription before broadcasting,
	// so the frame is not lost to a race between Accept and subscribe.
	require.Eventually(t, func() bool {
		return rig.hub.SubscriberCount() == 1
	}, time.Second, 5*time.Millisecond)

	rig.hub.Speak("chan", "id1", "hello overlay", "voiceX", "YXVkaW8=", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, typ)

	var m Message
	require.NoError(t, json.Unmarshal(data, &m))
	require.Equal(t, TypeSpeak, m.Type)
	require.Equal(t, "id1", m.ID)
	require.Equal(t, "hello overlay", m.Text)
	require.Equal(t, "YXVkaW8=", m.AudioB64)
}

func TestWSUnsubscribesOnDisconnect(t *testing.T) {
	rig := newWSTestRig(t)
	conn, _, err := dial(t, rig.wsURL("?channel=chan&token="+rig.token))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return rig.hub.SubscriberCount() == 1
	}, time.Second, 5*time.Millisecond)

	_ = conn.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool {
		return rig.hub.SubscriberCount() == 0
	}, 2*time.Second, 5*time.Millisecond, "hub must drop the subscriber on disconnect")
}

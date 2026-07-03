package avatar

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// WSHandler upgrades an overlay's HTTP request to a WebSocket and streams avatar
// directives for the requested channel. It is served (via the router) at
//
//	GET /api/v1/overlay/avatar/ws?channel={slug}&token={overlay_token}
//
// The endpoint is intentionally not session-gated — an OBS browser source has
// no login — so it authenticates with the per-channel overlay token instead: a
// missing, malformed or wrong token is rejected with 401 before any upgrade.
type WSHandler struct {
	hub        *Hub
	tokens     TokenStore
	tenantID   string
	logger     *slog.Logger
	acceptOpts *websocket.AcceptOptions
}

// NewWSHandler constructs a WSHandler. A nil logger falls back to
// [slog.Default]. Origin checking is disabled by default because the overlay
// runs inside OBS (which sends no usable Origin); the token is the access
// control, not the Origin header.
func NewWSHandler(hub *Hub, tokens TokenStore, tenantID string, logger *slog.Logger) *WSHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &WSHandler{
		hub:        hub,
		tokens:     tokens,
		tenantID:   strings.TrimSpace(tenantID),
		logger:     logger.With("component", "avatar.ws"),
		acceptOpts: &websocket.AcceptOptions{InsecureSkipVerify: true},
	}
}

func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil || h.tokens == nil {
		http.Error(w, "avatar overlay not configured", http.StatusNotImplemented)
		return
	}
	channel := normalizeChannel(r.URL.Query().Get("channel"))
	if channel == "" {
		http.Error(w, "channel is required", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ok, err := h.tokens.Verify(r.Context(), h.tenantID, channel, token)
	if err != nil {
		h.logger.Warn("avatar overlay token verify failed", "channel", channel, "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, h.acceptOpts)
	if err != nil {
		h.logger.Warn("avatar overlay ws accept failed", "err", err)
		return
	}
	conn.SetReadLimit(maxMessageBytes)

	sub := h.hub.subscribe(channel)
	defer h.hub.unsubscribe(channel, sub)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// The overlay is receive-only; the read loop exists solely to observe the
	// peer closing (or misbehaving) and to cancel the write loop when it does.
	go h.readLoop(ctx, cancel, conn)
	h.writeLoop(ctx, conn, sub)

	_ = conn.Close(websocket.StatusNormalClosure, "")
	h.logger.Debug("avatar overlay disconnected", "channel", channel, "subscriber_id", sub.id)
}

// readLoop drains inbound frames (which are ignored) until the peer or context
// ends, then cancels so the write loop stops.
func (h *WSHandler) readLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	defer cancel()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}

// writeLoop forwards queued directives to the overlay and pings periodically.
// It returns when the context ends, the subscriber's queue is closed (the hub
// dropped it), or a write fails — after which the caller closes the connection,
// which unblocks readLoop.
func (h *WSHandler) writeLoop(ctx context.Context, conn *websocket.Conn, sub *subscriber) {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.send:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

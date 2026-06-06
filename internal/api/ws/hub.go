// Package ws provides a minimal WebSocket hub that upgrades inbound HTTP
// connections, echoes received text/binary messages, and supports
// fan-out broadcasts with per-client backpressure.
package ws

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultSendQueueDepth = 64
	defaultWriteTimeout   = 5 * time.Second
	defaultPingInterval   = 30 * time.Second
	maxMessageBytes       = 1 << 20
)

// Hub fans broadcasts out to all connected WebSocket clients.
// A zero Hub is not usable; construct via NewHub.
type Hub struct {
	logger *slog.Logger

	broadcast chan []byte

	mu      sync.RWMutex
	clients map[*client]struct{}

	acceptOpts *websocket.AcceptOptions

	connCount atomic.Int64
}

type client struct {
	conn *websocket.Conn
	send chan []byte
	id   int64
}

// NewHub constructs a Hub. The optional AcceptOptions controls origin checks
// for the WebSocket handshake; pass nil to allow any origin (suitable for
// local-only daemons).
func NewHub(logger *slog.Logger, opts ...HubOption) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Hub{
		logger:    logger,
		broadcast: make(chan []byte, 256),
		clients:   make(map[*client]struct{}),
		acceptOpts: &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HubOption customizes a Hub at construction.
type HubOption func(*Hub)

// WithAcceptOptions overrides the default permissive AcceptOptions.
func WithAcceptOptions(o *websocket.AcceptOptions) HubOption {
	return func(h *Hub) { h.acceptOpts = o }
}

// Run processes broadcast fan-out until ctx is cancelled. Returns when ctx is
// done; safe to call once per Hub.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.closeAll(websocket.StatusGoingAway, "server shutting down")
			return
		case msg, ok := <-h.broadcast:
			if !ok {
				return
			}
			h.fanout(msg)
		}
	}
}

// Broadcast queues a payload for delivery to every connected client.
// Non-blocking: if the hub's broadcast queue is full, the message is dropped
// and a warning is logged.
func (h *Hub) Broadcast(payload []byte) {
	cp := make([]byte, len(payload))
	copy(cp, payload)
	select {
	case h.broadcast <- cp:
	default:
		h.logger.Warn("ws hub broadcast queue full, dropping message", "bytes", len(payload))
	}
}

// ConnCount returns the number of currently connected clients.
func (h *Hub) ConnCount() int64 { return h.connCount.Load() }

func (h *Hub) fanout(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			h.logger.Warn("ws client send queue full, dropping",
				"client_id", c.id)
		}
	}
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.connCount.Add(1)
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
	h.connCount.Add(-1)
}

func (h *Hub) closeAll(code websocket.StatusCode, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		_ = c.conn.Close(code, reason)
		delete(h.clients, c)
		close(c.send)
		h.connCount.Add(-1)
	}
}

var clientIDSeq atomic.Int64

// ServeHTTP upgrades the request to a WebSocket connection and runs the
// per-client read/write loops until either side disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, h.acceptOpts)
	if err != nil {
		h.logger.Warn("ws accept failed", "err", err)
		return
	}
	conn.SetReadLimit(maxMessageBytes)

	c := &client{
		conn: conn,
		send: make(chan []byte, defaultSendQueueDepth),
		id:   clientIDSeq.Add(1),
	}
	h.register(c)
	h.logger.Info("ws client connected", "client_id", c.id, "remote", r.RemoteAddr)

	connCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go h.writePump(connCtx, c)
	h.readPump(connCtx, c)

	h.unregister(c)
	_ = conn.Close(websocket.StatusNormalClosure, "")
	h.logger.Info("ws client disconnected", "client_id", c.id)
}

func (h *Hub) readPump(ctx context.Context, c *client) {
	for {
		mt, data, err := c.conn.Read(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				h.logger.Debug("ws read closed", "client_id", c.id, "err", err)
			}
			return
		}
		if mt != websocket.MessageText && mt != websocket.MessageBinary {
			continue
		}
		echo := make([]byte, len(data))
		copy(echo, data)
		select {
		case c.send <- echo:
		default:
			h.logger.Warn("ws echo dropped: send queue full", "client_id", c.id)
		}
	}
}

func (h *Hub) writePump(ctx context.Context, c *client) {
	pingTicker := time.NewTicker(defaultPingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, defaultWriteTimeout)
			err := c.conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				h.logger.Debug("ws write failed", "client_id", c.id, "err", err)
				return
			}
		case <-pingTicker.C:
			pingCtx, cancel := context.WithTimeout(ctx, defaultWriteTimeout)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				h.logger.Debug("ws ping failed", "client_id", c.id, "err", err)
				return
			}
		}
	}
}

package avatar

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// subscriberBuffer bounds each overlay's outbound queue. Directives are
	// small and infrequent; a modest buffer absorbs a burst while a full
	// buffer means the overlay cannot keep up and is dropped.
	subscriberBuffer = 16
	// pingInterval keeps overlay connections and intermediaries alive.
	pingInterval = 30 * time.Second
	// writeTimeout bounds a single frame or ping write.
	writeTimeout = 5 * time.Second
	// maxMessageBytes caps inbound frames from an overlay (which is
	// receive-only, so anything large is unexpected).
	maxMessageBytes = 1 << 20
)

// subscriber is one connected overlay. send is a bounded queue drained by the
// connection's write pump; the hub closes it exactly once when the subscriber
// is removed.
type subscriber struct {
	send chan []byte
	id   int64
}

var subIDSeq atomic.Int64

// Hub relays avatar directives to overlay subscribers grouped by channel. A
// zero Hub is not usable; construct one with [NewHub]. It is safe for
// concurrent use. Unlike a central-goroutine hub, fan-out is synchronous and
// non-blocking under a read lock, so there is no background goroutine to run.
type Hub struct {
	logger *slog.Logger

	mu       sync.RWMutex
	channels map[string]map[*subscriber]struct{}
}

// NewHub constructs an empty Hub. A nil logger falls back to [slog.Default].
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		logger:   logger.With("component", "avatar.hub"),
		channels: make(map[string]map[*subscriber]struct{}),
	}
}

// subscribe registers a new overlay for channel and returns its subscriber
// handle. The caller drains sub.send until it is closed.
func (h *Hub) subscribe(channel string) *subscriber {
	channel = normalizeChannel(channel)
	s := &subscriber{
		send: make(chan []byte, subscriberBuffer),
		id:   subIDSeq.Add(1),
	}
	h.mu.Lock()
	set := h.channels[channel]
	if set == nil {
		set = make(map[*subscriber]struct{})
		h.channels[channel] = set
	}
	set[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// unsubscribe removes s from channel and closes its send queue. It is
// idempotent: removing a subscriber that was already dropped (for example by a
// full-buffer eviction) is a no-op, so the send channel is closed exactly once.
func (h *Hub) unsubscribe(channel string, s *subscriber) {
	channel = normalizeChannel(channel)
	h.mu.Lock()
	if set, ok := h.channels[channel]; ok {
		if _, present := set[s]; present {
			delete(set, s)
			close(s.send)
			if len(set) == 0 {
				delete(h.channels, channel)
			}
		}
	}
	h.mu.Unlock()
}

// publish fans payload out to every subscriber of channel. Delivery is
// non-blocking: a subscriber whose buffer is full is dropped (and its queue
// closed) rather than blocking the broadcaster or growing memory without bound.
func (h *Hub) publish(channel string, payload []byte) {
	h.mu.RLock()
	set := h.channels[channel]
	var dead []*subscriber
	for s := range set {
		select {
		case s.send <- payload:
		default:
			dead = append(dead, s)
		}
	}
	h.mu.RUnlock()

	for _, s := range dead {
		h.logger.Warn("avatar overlay subscriber dropped: send buffer full",
			"channel", channel, "subscriber_id", s.id)
		h.unsubscribe(channel, s)
	}
}

// publishMessage marshals m and publishes it to channel. A marshal failure is
// logged and swallowed so a single bad directive never affects other overlays.
func (h *Hub) publishMessage(channel string, m Message) {
	channel = normalizeChannel(channel)
	if channel == "" {
		return
	}
	payload, err := json.Marshal(m)
	if err != nil {
		h.logger.Warn("avatar directive marshal failed", "type", m.Type, "err", err)
		return
	}
	h.publish(channel, payload)
}

// Speak broadcasts a speak directive: synthesized audio (base64 MP3) plus the
// spoken text and, optionally, the voice id and an opaque alignment passthrough.
// It satisfies the actions.AvatarHub contract.
func (h *Hub) Speak(channel, id, text, voice, audioB64 string, alignment json.RawMessage) {
	h.publishMessage(channel, Message{
		Type:      TypeSpeak,
		ID:        id,
		Text:      text,
		Voice:     voice,
		AudioB64:  audioB64,
		Alignment: alignment,
	})
}

// Expression broadcasts an expression directive. holdSeconds tells the overlay
// how long to hold before reverting to idle (0 = hold until changed). It
// satisfies the actions.AvatarHub contract.
func (h *Hub) Expression(channel, id, expression string, holdSeconds int) {
	h.publishMessage(channel, Message{
		Type:        TypeExpression,
		ID:          id,
		Expression:  expression,
		HoldSeconds: holdSeconds,
	})
}

// End broadcasts an explicit end marker for the speak directive with the given
// id. The v1 speak path relies on the overlay deriving end from audio
// completion; End exists for a future explicit stop/interrupt path.
func (h *Hub) End(channel, id string) {
	h.publishMessage(channel, Message{Type: TypeEnd, ID: id})
}

// SubscriberCount returns the total number of connected overlays across all
// channels. It backs the integration's connected probe.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, set := range h.channels {
		n += len(set)
	}
	return n
}

// channelSubscriberCount returns the number of overlays connected to channel.
func (h *Hub) channelSubscriberCount(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.channels[normalizeChannel(channel)])
}

package runtime

import (
	"encoding/json"
	"log/slog"
)

// RawBroadcaster is the contract satisfied by ws.Hub: a sink that accepts an
// opaque byte payload. WSBroadcaster wraps it so the dispatcher can emit
// typed envelopes without importing internal/api/ws.
type RawBroadcaster interface {
	Broadcast(payload []byte)
}

// WSBroadcaster adapts a ws.Hub-style sink to the Broadcaster interface used
// by the Dispatcher.
type WSBroadcaster struct {
	Sink   RawBroadcaster
	Logger *slog.Logger
}

// NewWSBroadcaster wraps sink so dispatcher events become JSON-encoded
// envelopes of the form {"type": "...", "data": <event>}.
func NewWSBroadcaster(sink RawBroadcaster, logger *slog.Logger) *WSBroadcaster {
	if logger == nil {
		logger = slog.Default()
	}
	return &WSBroadcaster{Sink: sink, Logger: logger}
}

// Broadcast marshals the envelope and forwards it.
func (b *WSBroadcaster) Broadcast(eventType string, payload any) {
	if b == nil || b.Sink == nil {
		return
	}
	envelope := struct {
		Type string `json:"type"`
		Data any    `json:"data"`
	}{Type: eventType, Data: payload}
	buf, err := json.Marshal(envelope)
	if err != nil {
		b.Logger.Warn("ws bridge marshal failed", "type", eventType, "err", err)
		return
	}
	b.Sink.Broadcast(buf)
}

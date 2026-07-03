package twitch

import (
	"context"
	"fmt"
	"time"

	"github.com/nicklaw5/helix/v2"

	"github.com/Luca-Pelzer/engelos/internal/adapters"
)

// SubscribeStreamOnline registers a stream.online EventSub subscription bound to
// the given WebSocket session for the channel, mirroring SubscribeRedemptions.
// stream.online is public: it needs only the broadcaster id and no extra scope.
// Returns [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) SubscribeStreamOnline(ctx context.Context, login, sessionID string) error {
	return a.subscribeStream(ctx, login, sessionID, "stream.online")
}

// SubscribeStreamOffline registers a stream.offline EventSub subscription bound
// to the given WebSocket session for the channel. Returns [ErrHelixUnavailable]
// in anonymous mode.
func (a *Adapter) SubscribeStreamOffline(ctx context.Context, login, sessionID string) error {
	return a.subscribeStream(ctx, login, sessionID, "stream.offline")
}

func (a *Adapter) subscribeStream(ctx context.Context, login, sessionID, subType string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return err
	}
	resp, err := hx.CreateEventSubSubscription(&helix.EventSubSubscription{
		Type:      subType,
		Version:   "1",
		Condition: helix.EventSubCondition{BroadcasterUserID: bid},
		Transport: helix.EventSubTransport{Method: "websocket", SessionID: sessionID},
	})
	if err != nil {
		return fmt.Errorf("twitch: subscribe %s: %w", subType, err)
	}
	return helixStatusError("subscribe "+subType, resp.StatusCode, resp.ErrorMessage)
}

// EmitStreamEvent normalizes a stream online/offline signal into a neutral
// adapters.Event and publishes it on the adapter's Events() channel, so the
// dispatcher routes it like any other platform event. It is the seam the
// EventSub stream handler (wired in main) calls; login is the broadcaster's
// Twitch login, which becomes the event Channel.
func (a *Adapter) EmitStreamEvent(online bool, login string, startedAt time.Time) {
	a.emit(adapters.Event{
		ID:         adapters.NewEventID(),
		Type:       streamEventType(online),
		Platform:   a.Name(),
		Channel:    normalizeLogin(login),
		OccurredAt: time.Now().UTC(),
		Stream:     &adapters.StreamEvent{IsLive: online, StartedAt: startedAt},
	})
}

func streamEventType(online bool) adapters.EventType {
	if online {
		return adapters.EventStreamOnline
	}
	return adapters.EventStreamOffline
}

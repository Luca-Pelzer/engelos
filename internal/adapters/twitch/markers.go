package twitch

import (
	"context"
	"fmt"

	"github.com/nicklaw5/helix/v2"
)

// MarkerView is a neutral, helix-free snapshot of a created stream marker.
// PositionSeconds is the marker's offset into the current VOD.
type MarkerView struct {
	ID              string
	PositionSeconds int
}

// CreateStreamMarker marks the current point in the channel's live stream with
// an optional description. Twitch rejects markers when the channel is offline
// (Helix 404), which surfaces here as an error. Requires the
// channel:manage:broadcast scope; returns [ErrHelixUnavailable] in anonymous
// mode.
func (a *Adapter) CreateStreamMarker(ctx context.Context, login, description string) (MarkerView, error) {
	if err := ctx.Err(); err != nil {
		return MarkerView{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return MarkerView{}, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return MarkerView{}, err
	}
	resp, err := hx.CreateStreamMarker(&helix.CreateStreamMarkerParams{
		UserID:      bid,
		Description: description,
	})
	if err != nil {
		return MarkerView{}, fmt.Errorf("twitch: create marker on %q: %w", login, err)
	}
	if err := helixStatusError("create marker", resp.StatusCode, resp.ErrorMessage); err != nil {
		return MarkerView{}, err
	}
	if len(resp.Data.CreateStreamMarkers) == 0 {
		return MarkerView{}, fmt.Errorf("twitch: create marker on %q: empty response", login)
	}
	m := resp.Data.CreateStreamMarkers[0]
	return MarkerView{ID: m.ID, PositionSeconds: m.PositionSeconds}, nil
}

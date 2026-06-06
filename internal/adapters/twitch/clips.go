package twitch

import (
	"context"
	"fmt"

	"github.com/nicklaw5/helix/v2"
)

// ClipView is a neutral, helix-free snapshot of a created clip returned to the
// caller so it never depends on the helix SDK. EditURL is Twitch's editor link
// for the freshly created clip; URL is the public watch URL once the clip has
// finished processing.
type ClipView struct {
	ID      string
	EditURL string
	URL     string
}

// CreateClip captures a clip from the channel's live stream with an optional
// duration in seconds (Twitch clamps to 5-60; pass 0 for the Twitch default).
// Twitch creates clips asynchronously: this returns the new clip id and edit
// URL immediately, and the public URL is resolved later via [Adapter.GetClip].
//
// Requires the clips:edit scope on an authenticated broadcaster token. Returns
// [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) CreateClip(ctx context.Context, login string, durationSeconds float64) (ClipView, error) {
	if err := ctx.Err(); err != nil {
		return ClipView{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return ClipView{}, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return ClipView{}, err
	}
	resp, err := hx.CreateClip(&helix.CreateClipParams{
		BroadcasterID: bid,
		Duration:      float32(durationSeconds),
	})
	if err != nil {
		return ClipView{}, fmt.Errorf("twitch: create clip on %q: %w", login, err)
	}
	if err := helixStatusError("create clip", resp.StatusCode, resp.ErrorMessage); err != nil {
		return ClipView{}, err
	}
	if len(resp.Data.ClipEditURLs) == 0 {
		return ClipView{}, fmt.Errorf("twitch: create clip on %q: empty response", login)
	}
	c := resp.Data.ClipEditURLs[0]
	return ClipView{ID: c.ID, EditURL: c.EditURL}, nil
}

// GetClip resolves a clip id to its public watch URL once Twitch has finished
// processing it. An empty URL with a nil error means the clip is not ready yet
// (the caller should retry); Twitch recommends polling for up to 15s after
// creation. Returns [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) GetClip(ctx context.Context, clipID string) (ClipView, error) {
	if err := ctx.Err(); err != nil {
		return ClipView{}, err
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return ClipView{}, err
	}
	resp, err := hx.GetClips(&helix.ClipsParams{IDs: []string{clipID}})
	if err != nil {
		return ClipView{}, fmt.Errorf("twitch: get clip %q: %w", clipID, err)
	}
	if err := helixStatusError("get clip", resp.StatusCode, resp.ErrorMessage); err != nil {
		return ClipView{}, err
	}
	if len(resp.Data.Clips) == 0 {
		return ClipView{ID: clipID}, nil
	}
	c := resp.Data.Clips[0]
	return ClipView{ID: c.ID, URL: c.URL}, nil
}

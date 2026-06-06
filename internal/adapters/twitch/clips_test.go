package twitch

import (
	"context"
	"errors"
	"testing"

	"github.com/nicklaw5/helix/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateClip_MapsParamsAndResponse(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	resp := &helix.CreateClipResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.ClipEditURLs = []helix.ClipEditURL{{ID: "clip-1", EditURL: "https://clips.twitch.tv/edit/clip-1"}}
	hx.createClipResp = resp
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.CreateClip(context.Background(), "#Broadcaster", 30)
	require.NoError(t, err)
	assert.Equal(t, "clip-1", view.ID)
	assert.Equal(t, "https://clips.twitch.tv/edit/clip-1", view.EditURL)

	require.NotNil(t, hx.lastCreateClip)
	assert.Equal(t, "987", hx.lastCreateClip.BroadcasterID)
	assert.Equal(t, float32(30), hx.lastCreateClip.Duration)
}

func TestCreateClip_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreateClip(context.Background(), "broadcaster", 30)
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

func TestCreateClip_EmptyResponseErrors(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.createClipResp = &helix.CreateClipResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreateClip(context.Background(), "broadcaster", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
}

func TestCreateClip_HelixError(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.createClipErr = errors.New("boom")
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreateClip(context.Background(), "broadcaster", 0)
	require.Error(t, err)
}

func TestGetClip_ReturnsURL(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	resp := &helix.ClipsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.Clips = []helix.Clip{{ID: "clip-1", URL: "https://clips.twitch.tv/clip-1"}}
	hx.getClipsResp = resp
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.GetClip(context.Background(), "clip-1")
	require.NoError(t, err)
	assert.Equal(t, "clip-1", view.ID)
	assert.Equal(t, "https://clips.twitch.tv/clip-1", view.URL)

	require.NotNil(t, hx.lastGetClips)
	require.Len(t, hx.lastGetClips.IDs, 1)
	assert.Equal(t, "clip-1", hx.lastGetClips.IDs[0])
}

func TestGetClip_NotReadyReturnsEmptyURL(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.getClipsResp = &helix.ClipsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.GetClip(context.Background(), "clip-1")
	require.NoError(t, err)
	assert.Equal(t, "clip-1", view.ID)
	assert.Equal(t, "", view.URL)
}

func TestGetClip_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.GetClip(context.Background(), "clip-1")
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

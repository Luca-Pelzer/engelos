package twitch

import (
	"context"
	"testing"

	"github.com/nicklaw5/helix/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateStreamMarker_MapsParamsAndResponse(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	resp := &helix.CreateStreamMarkerResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.CreateStreamMarkers = []helix.CreateStreamMarker{{ID: "marker-1", PositionSeconds: 42}}
	hx.createMarkerResp = resp
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.CreateStreamMarker(context.Background(), "#Broadcaster", "big play")
	require.NoError(t, err)
	assert.Equal(t, "marker-1", view.ID)
	assert.Equal(t, 42, view.PositionSeconds)

	require.NotNil(t, hx.lastCreateMarker)
	assert.Equal(t, "987", hx.lastCreateMarker.UserID)
	assert.Equal(t, "big play", hx.lastCreateMarker.Description)
}

func TestCreateStreamMarker_OfflineStreamErrors(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	// Twitch 404s stream markers when the channel is offline.
	hx.createMarkerResp = &helix.CreateStreamMarkerResponse{
		ResponseCommon: helix.ResponseCommon{StatusCode: 404, ErrorMessage: "Not Found"},
	}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreateStreamMarker(context.Background(), "#Broadcaster", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create marker")
}

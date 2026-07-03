package twitch

import (
	"context"
	"testing"

	"github.com/nicklaw5/helix/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePoll_MapsParamsAndResponse(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	resp := &helix.PollsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.Polls = []helix.Poll{{ID: "poll-1", Title: "Best game?"}}
	hx.createPollResp = resp
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.CreatePoll(context.Background(), "#Broadcaster", "Best game?", []string{"Elden Ring", "DS3"}, 90)
	require.NoError(t, err)
	assert.Equal(t, "poll-1", view.ID)
	assert.Equal(t, "Best game?", view.Title)

	require.NotNil(t, hx.lastCreatePoll)
	assert.Equal(t, "987", hx.lastCreatePoll.BroadcasterID)
	assert.Equal(t, "Best game?", hx.lastCreatePoll.Title)
	assert.Equal(t, 90, hx.lastCreatePoll.Duration)
	require.Len(t, hx.lastCreatePoll.Choices, 2)
	assert.Equal(t, "Elden Ring", hx.lastCreatePoll.Choices[0].Title)
	assert.Equal(t, "DS3", hx.lastCreatePoll.Choices[1].Title)
}

func TestCreatePoll_HelixErrorSurfaces(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.createPollResp = &helix.PollsResponse{
		ResponseCommon: helix.ResponseCommon{StatusCode: 400, ErrorMessage: "Bad Request"},
	}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreatePoll(context.Background(), "#Broadcaster", "x", []string{"A", "B"}, 60)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create poll")
}

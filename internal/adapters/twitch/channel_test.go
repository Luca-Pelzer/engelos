package twitch

import (
	"context"
	"testing"

	"github.com/nicklaw5/helix/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetStreamTitle_MapsParams(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.SetStreamTitle(context.Background(), "#Broadcaster", "  new title  ")
	require.NoError(t, err)

	require.NotNil(t, hx.lastEditChannel)
	assert.Equal(t, "987", hx.lastEditChannel.BroadcasterID)
	assert.Equal(t, "new title", hx.lastEditChannel.Title)
	assert.Empty(t, hx.lastEditChannel.GameID)
}

func TestSetStreamTitle_EmptyTitleErrors(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.SetStreamTitle(context.Background(), "broadcaster", "   ")
	require.Error(t, err)
	assert.Nil(t, hx.lastEditChannel)
}

func TestSetStreamTitle_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	err := a.SetStreamTitle(context.Background(), "broadcaster", "title")
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

func TestSetCategory_ExactMatchSetsGameID(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	resp := &helix.SearchCategoriesResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.Categories = []helix.Category{
		{ID: "111", Name: "Just Chatting Adjacent"},
		{ID: "509658", Name: "Just Chatting"},
	}
	hx.searchCatResp = resp
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.SetCategory(context.Background(), "broadcaster", "just chatting")
	require.NoError(t, err)
	assert.Equal(t, "509658", view.ID)
	assert.Equal(t, "Just Chatting", view.Name)

	require.NotNil(t, hx.lastSearchCat)
	assert.Equal(t, "just chatting", hx.lastSearchCat.Query)
	require.NotNil(t, hx.lastEditChannel)
	assert.Equal(t, "987", hx.lastEditChannel.BroadcasterID)
	assert.Equal(t, "509658", hx.lastEditChannel.GameID)
	assert.Empty(t, hx.lastEditChannel.Title)
}

func TestSetCategory_NoExactMatchErrors(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	resp := &helix.SearchCategoriesResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	resp.Data.Categories = []helix.Category{{ID: "111", Name: "Just Chatting Adjacent"}}
	hx.searchCatResp = resp
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.SetCategory(context.Background(), "broadcaster", "Just Chatting")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no category exactly named")
	assert.Nil(t, hx.lastEditChannel)
}

func TestSetCategory_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.SetCategory(context.Background(), "broadcaster", "Just Chatting")
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

package actions

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingChannel struct {
	mu           sync.Mutex
	clip         TwitchClip
	clipErr      error
	lastClipDur  float64
	marker       TwitchMarker
	markerErr    error
	lastMarkDesc string
	poll         TwitchPoll
	pollErr      error
	lastPoll     pollCall
	titles       []sentMessage
	categories   []sentMessage
	resolvedName string
	catErr       error
	fulfilled    []redemptionCall
	canceled     []redemptionCall
}

type redemptionCall struct{ channel, rewardID, redemptionID string }

type pollCall struct {
	channel  string
	title    string
	choices  []string
	duration int
}

func (c *recordingChannel) CreateClip(channel string, durationSeconds float64) (TwitchClip, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastClipDur = durationSeconds
	if c.clipErr != nil {
		return TwitchClip{}, c.clipErr
	}
	return c.clip, nil
}

func (c *recordingChannel) CreateMarker(channel, description string) (TwitchMarker, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastMarkDesc = description
	if c.markerErr != nil {
		return TwitchMarker{}, c.markerErr
	}
	return c.marker, nil
}

func (c *recordingChannel) CreatePoll(channel, title string, choices []string, durationSeconds int) (TwitchPoll, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastPoll = pollCall{channel: channel, title: title, choices: choices, duration: durationSeconds}
	if c.pollErr != nil {
		return TwitchPoll{}, c.pollErr
	}
	return c.poll, nil
}

func (c *recordingChannel) SetTitle(channel, title string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.titles = append(c.titles, sentMessage{Channel: channel, Text: title})
	return nil
}

func (c *recordingChannel) SetCategory(channel, name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.catErr != nil {
		return "", c.catErr
	}
	c.categories = append(c.categories, sentMessage{Channel: channel, Text: name})
	return c.resolvedName, nil
}

func (c *recordingChannel) FulfillRedemption(channel, rewardID, redemptionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fulfilled = append(c.fulfilled, redemptionCall{channel, rewardID, redemptionID})
	return nil
}

func (c *recordingChannel) CancelRedemption(channel, rewardID, redemptionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.canceled = append(c.canceled, redemptionCall{channel, rewardID, redemptionID})
	return nil
}

func TestTwitchChannelActions_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{TwitchChannel: &recordingChannel{}}))
	for _, id := range []string{
		"twitch:create-clip", "twitch:create-marker", "twitch:create-poll",
		"twitch:set-title", "twitch:set-category", "twitch:redemption",
	} {
		_, ok := reg.Action(id)
		assert.True(t, ok, id)
	}
	_, gone := reg.Action("twitch:update-redemption")
	assert.False(t, gone, "old redemption id must not remain registered")
}

func TestTwitchCreateClip_Outputs(t *testing.T) {
	ch := &recordingChannel{clip: TwitchClip{ID: "c1", EditURL: "edit", URL: "url"}}
	act := twitchCreateClipAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"duration_seconds": 30.0}))
	require.NoError(t, err)
	assert.Equal(t, "c1", res.Outputs["clip_id"])
	assert.Equal(t, "edit", res.Outputs["edit_url"])
	assert.Equal(t, "url", res.Outputs["url"])
	assert.Equal(t, 30.0, ch.lastClipDur)
}

func TestTwitchCreateClip_ErrorSurfaces(t *testing.T) {
	act := twitchCreateClipAction{ch: &recordingChannel{clipErr: errors.New("offline")}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.Error(t, err)
}

func TestTwitchSetTitle_Calls(t *testing.T) {
	ch := &recordingChannel{}
	act := twitchSetTitleAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"title": "New Title"}))
	require.NoError(t, err)
	require.Len(t, ch.titles, 1)
	assert.Equal(t, sentMessage{"chan", "New Title"}, ch.titles[0])
}

func TestTwitchSetTitle_RequiresTitle(t *testing.T) {
	act := twitchSetTitleAction{ch: &recordingChannel{}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"title": " "}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "title is required")
}

func TestTwitchSetCategory_OutputsResolvedName(t *testing.T) {
	ch := &recordingChannel{resolvedName: "Just Chatting"}
	act := twitchSetCategoryAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"category": "just chatting"}))
	require.NoError(t, err)
	assert.Equal(t, "Just Chatting", res.Outputs["category"])
	require.Len(t, ch.categories, 1)
	assert.Equal(t, "just chatting", ch.categories[0].Text)
}

func TestTwitchSetCategory_ErrorSurfaces(t *testing.T) {
	act := twitchSetCategoryAction{ch: &recordingChannel{catErr: errors.New("no match")}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"category": "Nope"}))
	require.Error(t, err)
}

func TestTwitchRedemption_DefaultStatusFulfills(t *testing.T) {
	ch := &recordingChannel{}
	act := twitchRedemptionAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"reward_id": "r1", "redemption_id": "d1"}))
	require.NoError(t, err)
	require.Len(t, ch.fulfilled, 1)
	assert.Equal(t, redemptionCall{"chan", "r1", "d1"}, ch.fulfilled[0])
	assert.Empty(t, ch.canceled)
}

func TestTwitchRedemption_CancelStatus(t *testing.T) {
	ch := &recordingChannel{}
	act := twitchRedemptionAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"status": "canceled", "reward_id": "r1", "redemption_id": "d1"}))
	require.NoError(t, err)
	require.Len(t, ch.canceled, 1)
	assert.Equal(t, redemptionCall{"chan", "r1", "d1"}, ch.canceled[0])
	assert.Empty(t, ch.fulfilled)
}

func TestTwitchRedemption_DefaultsIDsFromTriggerData(t *testing.T) {
	ch := &recordingChannel{}
	act := twitchRedemptionAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{
		Channel: "chan",
		Data:    map[string]any{"reward_id": "rd", "redemption_id": "dd"},
	})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.NoError(t, err)
	require.Len(t, ch.fulfilled, 1)
	assert.Equal(t, redemptionCall{"chan", "rd", "dd"}, ch.fulfilled[0])
}

func TestTwitchRedemption_MissingIDsErrors(t *testing.T) {
	act := twitchRedemptionAction{ch: &recordingChannel{}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"reward_id": "r1"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestTwitchRedemption_BadStatusErrors(t *testing.T) {
	act := twitchRedemptionAction{ch: &recordingChannel{}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"status": "explode", "reward_id": "r1", "redemption_id": "d1"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status must be")
}

func TestTwitchChannel_NilControllerErrors(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	act, _ := reg.Action("twitch:set-title")
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"title": "x"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestTwitchCreateMarker_Outputs(t *testing.T) {
	ch := &recordingChannel{marker: TwitchMarker{ID: "m1", PositionSeconds: 42}}
	act := twitchCreateMarkerAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{"description": "big play"}))
	require.NoError(t, err)
	assert.Equal(t, "m1", res.Outputs["marker_id"])
	assert.Equal(t, 42, res.Outputs["position_seconds"])
	assert.Equal(t, "big play", ch.lastMarkDesc)
}

func TestTwitchCreateMarker_TruncatesDescription(t *testing.T) {
	ch := &recordingChannel{}
	act := twitchCreateMarkerAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"description": strings.Repeat("x", 200)}))
	require.NoError(t, err)
	assert.Len(t, []rune(ch.lastMarkDesc), markerDescriptionMax, "description truncated to Helix's 140-char cap")
}

func TestTwitchCreateMarker_AdapterErrorSurfaces(t *testing.T) {
	ch := &recordingChannel{markerErr: errors.New("stream not live")}
	act := twitchCreateMarkerAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream not live")
}

func TestTwitchCreatePoll_Outputs(t *testing.T) {
	ch := &recordingChannel{poll: TwitchPoll{ID: "p1", Title: "Best game?"}}
	act := twitchCreatePollAction{ch: ch}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	res, err := act.Execute(ec, mustJSON(t, map[string]any{
		"title":   "Best game?",
		"choices": []string{"A", "B", "C"},
	}))
	require.NoError(t, err)
	assert.Equal(t, "p1", res.Outputs["poll_id"])
	assert.Equal(t, "Best game?", res.Outputs["title"])
	assert.Equal(t, []string{"A", "B", "C"}, ch.lastPoll.choices)
	assert.Equal(t, pollDurationDefaul, ch.lastPoll.duration, "duration defaults to 60")
}

func TestTwitchCreatePoll_Validation(t *testing.T) {
	act := twitchCreatePollAction{ch: &recordingChannel{}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"empty title", map[string]any{"choices": []string{"A", "B"}}, "title is required"},
		{"title too long", map[string]any{"title": strings.Repeat("t", 61), "choices": []string{"A", "B"}}, "title exceeds"},
		{"one choice", map[string]any{"title": "x", "choices": []string{"A"}}, "need 2-5 choices"},
		{"six choices", map[string]any{"title": "x", "choices": []string{"A", "B", "C", "D", "E", "F"}}, "need 2-5 choices"},
		{"choice too long", map[string]any{"title": "x", "choices": []string{"A", strings.Repeat("c", 26)}}, "exceeds 25"},
		{"duration too low", map[string]any{"title": "x", "choices": []string{"A", "B"}, "duration_seconds": 10}, "must be 15-1800"},
		{"duration too high", map[string]any{"title": "x", "choices": []string{"A", "B"}, "duration_seconds": 2000}, "must be 15-1800"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := act.Execute(ec, mustJSON(t, c.cfg))
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

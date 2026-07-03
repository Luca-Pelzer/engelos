package actions

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingDiscord struct {
	mu      sync.Mutex
	posts   []sentMessage
	postErr error
}

func (r *recordingDiscord) PostMessage(channelID, text string) error {
	if r.postErr != nil {
		return r.postErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.posts = append(r.posts, sentMessage{Channel: channelID, Text: text})
	return nil
}

func TestDiscordPost_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{Discord: &recordingDiscord{}}))
	_, ok := reg.Action("discord:post")
	assert.True(t, ok)
}

func TestDiscordPost_SendsToChannel(t *testing.T) {
	dc := &recordingDiscord{}
	act := discordPostAction{discord: dc}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"channel_id": "123", "text": "gg"}))
	require.NoError(t, err)
	require.Len(t, dc.posts, 1)
	assert.Equal(t, "123", dc.posts[0].Channel)
	assert.Equal(t, "gg", dc.posts[0].Text)
}

func TestDiscordPost_RequiresChannelID(t *testing.T) {
	act := discordPostAction{discord: &recordingDiscord{}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"channel_id": " ", "text": "gg"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel_id is required")
}

func TestDiscordPost_EmptyTextNoOp(t *testing.T) {
	dc := &recordingDiscord{}
	act := discordPostAction{discord: dc}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"channel_id": "123", "text": ""}))
	require.NoError(t, err)
	assert.Empty(t, dc.posts)
}

func TestDiscordPost_NilPosterErrors(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	act, _ := reg.Action("discord:post")
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"channel_id": "123", "text": "hi"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestDiscordPost_PosterErrorSurfaces(t *testing.T) {
	act := discordPostAction{discord: &recordingDiscord{postErr: errors.New("403")}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"channel_id": "123", "text": "hi"}))
	require.Error(t, err)
}

func TestDiscordReply_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterDiscordReplyNode(reg, &recordingDiscord{}))
	_, ok := reg.Action("discord:reply")
	assert.True(t, ok)
}

func TestDiscordReply_ExplicitChannel(t *testing.T) {
	dc := &recordingDiscord{}
	act := discordReplyAction{discord: dc}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"channel_id": "999", "text": "hello"}))
	require.NoError(t, err)
	require.Len(t, dc.posts, 1)
	assert.Equal(t, "999", dc.posts[0].Channel)
	assert.Equal(t, "hello", dc.posts[0].Text)
}

func TestDiscordReply_DefaultsToOriginatingChannel(t *testing.T) {
	dc := &recordingDiscord{}
	act := discordReplyAction{discord: dc}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"),
		Trigger{Data: map[string]any{"discord.channel_id": "orig-chan"}})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"text": "hi there"}))
	require.NoError(t, err)
	require.Len(t, dc.posts, 1)
	assert.Equal(t, "orig-chan", dc.posts[0].Channel, "defaults to the triggering Discord channel")
}

func TestDiscordReply_NoChannelErrors(t *testing.T) {
	act := discordReplyAction{discord: &recordingDiscord{}}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"text": "hi"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no channel_id and no originating Discord channel")
}

func TestDiscordReply_EmptyTextNoOp(t *testing.T) {
	dc := &recordingDiscord{}
	act := discordReplyAction{discord: dc}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"),
		Trigger{Data: map[string]any{"discord.channel_id": "orig"}})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"text": ""}))
	require.NoError(t, err)
	assert.Empty(t, dc.posts)
}

func TestDiscordReply_RateLimitedPerChannel(t *testing.T) {
	dc := &recordingDiscord{}
	// Frozen clock + burst 5: the 6th reply into the same channel is dropped.
	frozen := time.Unix(1_000_000, 0)
	act := discordReplyAction{discord: dc, limiter: newReplyRateLimiter(1, 5, func() time.Time { return frozen })}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"),
		Trigger{Data: map[string]any{"discord.channel_id": "c1"}})

	for i := 0; i < 6; i++ {
		res, err := act.Execute(ec, mustJSON(t, map[string]any{"text": "spam"}))
		require.NoError(t, err)
		if i == 5 {
			require.NotNil(t, res)
			assert.Equal(t, true, res.Outputs["rate_limited"])
		}
	}
	assert.Len(t, dc.posts, 5, "at most 5 replies per channel within the window")
}

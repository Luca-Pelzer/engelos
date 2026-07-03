package actions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type spokenDirective struct {
	channel, id, text, voice, audioB64 string
	alignment                          json.RawMessage
}

type exprDirective struct {
	channel, id, expression string
	hold                    int
}

type fakeAvatarHub struct {
	mu     sync.Mutex
	speaks []spokenDirective
	exprs  []exprDirective
}

func (f *fakeAvatarHub) Speak(channel, id, text, voice, audioB64 string, alignment json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.speaks = append(f.speaks, spokenDirective{channel, id, text, voice, audioB64, alignment})
}

func (f *fakeAvatarHub) Expression(channel, id, expression string, holdSeconds int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exprs = append(f.exprs, exprDirective{channel, id, expression, holdSeconds})
}

type fakeAvatarSynth struct {
	audio                         []byte
	err                           error
	gotChannel, gotVoice, gotText string
}

func (f *fakeAvatarSynth) SynthesizeText(_ context.Context, channel, voice, text string) ([]byte, error) {
	f.gotChannel, f.gotVoice, f.gotText = channel, voice, text
	return f.audio, f.err
}

func avatarEC() *ExecutionContext {
	return &ExecutionContext{Ctx: context.Background(), Channel: "chan", Trigger: Trigger{Channel: "chan"}}
}

func TestAvatarSpeakSynthesizesAndBroadcasts(t *testing.T) {
	hub := &fakeAvatarHub{}
	synth := &fakeAvatarSynth{audio: []byte("mp3bytes")}
	node := avatarSpeakAction{hub: hub, synth: synth, limiter: newAvatarSpeakLimiter(avatarSpeakConcurrency)}

	res, err := node.Execute(avatarEC(), json.RawMessage(`{"text":"hello world","voice":"voiceX"}`))
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, res.Outputs["avatar_speak_id"])

	require.Equal(t, "hello world", synth.gotText)
	require.Equal(t, "voiceX", synth.gotVoice)
	require.Equal(t, "chan", synth.gotChannel)

	require.Len(t, hub.speaks, 1)
	require.Equal(t, "hello world", hub.speaks[0].text)
	require.Equal(t, "voiceX", hub.speaks[0].voice)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("mp3bytes")), hub.speaks[0].audioB64)
	require.Equal(t, res.Outputs["avatar_speak_id"], hub.speaks[0].id)
}

func TestAvatarSpeakExpressionDuring(t *testing.T) {
	hub := &fakeAvatarHub{}
	synth := &fakeAvatarSynth{audio: []byte("x")}
	node := avatarSpeakAction{hub: hub, synth: synth, limiter: newAvatarSpeakLimiter(3)}

	_, err := node.Execute(avatarEC(), json.RawMessage(`{"text":"hi","expression_during":"talking"}`))
	require.NoError(t, err)
	require.Len(t, hub.exprs, 1)
	require.Equal(t, "talking", hub.exprs[0].expression)
	require.Equal(t, 0, hub.exprs[0].hold)
	require.Len(t, hub.speaks, 1)
}

func TestAvatarSpeakInvalidExpressionDuring(t *testing.T) {
	hub := &fakeAvatarHub{}
	synth := &fakeAvatarSynth{audio: []byte("x")}
	node := avatarSpeakAction{hub: hub, synth: synth, limiter: newAvatarSpeakLimiter(3)}

	_, err := node.Execute(avatarEC(), json.RawMessage(`{"text":"hi","expression_during":"bogus"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid expression_during")
	require.Empty(t, hub.speaks)
}

func TestAvatarSpeakNoTTSConfigured(t *testing.T) {
	hub := &fakeAvatarHub{}
	node := avatarSpeakAction{hub: hub, synth: nil, limiter: newAvatarSpeakLimiter(3)}

	_, err := node.Execute(avatarEC(), json.RawMessage(`{"text":"hi"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "tts not configured")
	require.Empty(t, hub.speaks)
}

func TestAvatarSpeakSynthErrorFailsClosed(t *testing.T) {
	hub := &fakeAvatarHub{}
	synth := &fakeAvatarSynth{err: errors.New("boom")}
	node := avatarSpeakAction{hub: hub, synth: synth, limiter: newAvatarSpeakLimiter(3)}

	_, err := node.Execute(avatarEC(), json.RawMessage(`{"text":"hi"}`))
	require.Error(t, err)
	require.Empty(t, hub.speaks, "no broadcast when synthesis fails")
}

func TestAvatarSpeakEmptyTextIsNoop(t *testing.T) {
	hub := &fakeAvatarHub{}
	synth := &fakeAvatarSynth{audio: []byte("x")}
	node := avatarSpeakAction{hub: hub, synth: synth, limiter: newAvatarSpeakLimiter(3)}

	res, err := node.Execute(avatarEC(), json.RawMessage(`{"text":"   "}`))
	require.NoError(t, err)
	require.Nil(t, res)
	require.Empty(t, hub.speaks)
}

func TestAvatarSpeakRateLimited(t *testing.T) {
	hub := &fakeAvatarHub{}
	synth := &fakeAvatarSynth{audio: []byte("x")}
	limiter := newAvatarSpeakLimiter(1)
	node := avatarSpeakAction{hub: hub, synth: synth, limiter: limiter}

	// Occupy the single slot for "chan" so the node's own acquire fails.
	require.True(t, limiter.acquire("chan"))
	defer limiter.release("chan")

	_, err := node.Execute(avatarEC(), json.RawMessage(`{"text":"hi"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limited")
	require.Empty(t, hub.speaks)
}

func TestAvatarSpeakLimiterReleasesSlot(t *testing.T) {
	hub := &fakeAvatarHub{}
	synth := &fakeAvatarSynth{audio: []byte("x")}
	node := avatarSpeakAction{hub: hub, synth: synth, limiter: newAvatarSpeakLimiter(1)}

	// Two sequential calls both succeed: the slot is released after each.
	_, err := node.Execute(avatarEC(), json.RawMessage(`{"text":"one"}`))
	require.NoError(t, err)
	_, err = node.Execute(avatarEC(), json.RawMessage(`{"text":"two"}`))
	require.NoError(t, err)
	require.Len(t, hub.speaks, 2)
}

func TestAvatarExpressionValidatesEnum(t *testing.T) {
	hub := &fakeAvatarHub{}
	node := avatarExpressionAction{hub: hub}

	for _, expr := range []string{"idle", "talking", "happy", "thinking", "confused", "hype", "error"} {
		_, err := node.Execute(avatarEC(), json.RawMessage(`{"expression":"`+expr+`"}`))
		require.NoError(t, err, expr)
	}
	require.Len(t, hub.exprs, 7)

	_, err := node.Execute(avatarEC(), json.RawMessage(`{"expression":"BOGUS"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid expression")

	_, err = node.Execute(avatarEC(), json.RawMessage(`{"expression":""}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestAvatarExpressionCaseInsensitiveAndHold(t *testing.T) {
	hub := &fakeAvatarHub{}
	node := avatarExpressionAction{hub: hub}

	res, err := node.Execute(avatarEC(), json.RawMessage(`{"expression":"HyPe","hold_seconds":5}`))
	require.NoError(t, err)
	require.NotEmpty(t, res.Outputs["avatar_expression_id"])
	require.Len(t, hub.exprs, 1)
	require.Equal(t, "hype", hub.exprs[0].expression)
	require.Equal(t, 5, hub.exprs[0].hold)
}

func TestAvatarExpressionNilHub(t *testing.T) {
	node := avatarExpressionAction{hub: nil}
	_, err := node.Execute(avatarEC(), json.RawMessage(`{"expression":"idle"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

func TestRegisterAvatarNodesPopulatesCatalog(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterAvatarNodes(reg, &fakeAvatarHub{}, &fakeAvatarSynth{}))

	_, ok := reg.Action("avatar:speak")
	require.True(t, ok)
	_, ok = reg.Action("avatar:expression")
	require.True(t, ok)

	ids := make([]string, 0)
	for _, d := range reg.Actions() {
		ids = append(ids, d.ID)
	}
	joined := strings.Join(ids, ",")
	require.Contains(t, joined, "avatar:speak")
	require.Contains(t, joined, "avatar:expression")
}

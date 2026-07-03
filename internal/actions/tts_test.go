package actions

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingTTS struct {
	mu     sync.Mutex
	spoken []sentMessage
}

func (r *recordingTTS) Speak(channel, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spoken = append(r.spoken, sentMessage{Channel: channel, Text: text})
}

func TestTTSSpeak_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterTTSNodes(reg, &recordingTTS{}))
	_, ok := reg.Action("tts:speak")
	assert.True(t, ok)
}

func TestTTSSpeak_EnqueuesText(t *testing.T) {
	tts := &recordingTTS{}
	act := ttsSpeakAction{tts: tts}
	ec := newExecutionContext(context.Background(), sampleRule("mychan", "t"), Trigger{Channel: "trigchan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"text": "hello world"}))
	require.NoError(t, err)
	require.Len(t, tts.spoken, 1)
	assert.Equal(t, "mychan", tts.spoken[0].Channel)
	assert.Equal(t, "hello world", tts.spoken[0].Text)
}

func TestTTSSpeak_EmptyTextNoOp(t *testing.T) {
	tts := &recordingTTS{}
	act := ttsSpeakAction{tts: tts}
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"text": "  "}))
	require.NoError(t, err)
	assert.Empty(t, tts.spoken)
}

func TestTTSSpeak_NilServiceErrors(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterTTSNodes(reg, nil))
	act, _ := reg.Action("tts:speak")
	ec := newExecutionContext(context.Background(), sampleRule("ch", "t"), Trigger{})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"text": "hi"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestTTSSpeak_ChannelOverride(t *testing.T) {
	tts := &recordingTTS{}
	act := ttsSpeakAction{tts: tts}
	ec := newExecutionContext(context.Background(), sampleRule("mychan", "t"), Trigger{Channel: "trigchan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"text": "hi", "channel": "otherchan"}))
	require.NoError(t, err)
	require.Len(t, tts.spoken, 1)
	assert.Equal(t, "otherchan", tts.spoken[0].Channel)
	assert.Equal(t, "hi", tts.spoken[0].Text)
}

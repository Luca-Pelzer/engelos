package actions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStreamState struct {
	live map[string]bool
}

func (f fakeStreamState) IsLive(channel string) bool { return f.live[channel] }

func TestStreamStateCondition_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{StreamState: fakeStreamState{}}))
	_, ok := reg.Condition("cond:stream-state")
	assert.True(t, ok)
}

func TestStreamStateCondition_LiveAndOffline(t *testing.T) {
	provider := fakeStreamState{live: map[string]bool{"chan": true}}
	c := streamStateCondition{provider: provider}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"state": "live"}))
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = c.Evaluate(ec, mustJSON(t, map[string]any{"state": "offline"}))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestStreamStateCondition_OfflineChannel(t *testing.T) {
	provider := fakeStreamState{live: map[string]bool{}}
	c := streamStateCondition{provider: provider}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"state": "offline"}))
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = c.Evaluate(ec, mustJSON(t, map[string]any{"state": "live"}))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestStreamStateCondition_NilProviderFailsClosed(t *testing.T) {
	c := streamStateCondition{provider: nil}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	ok, err := c.Evaluate(ec, mustJSON(t, map[string]any{"state": "live"}))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestStreamStateCondition_BadState(t *testing.T) {
	c := streamStateCondition{provider: fakeStreamState{}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := c.Evaluate(ec, mustJSON(t, map[string]any{"state": "streaming"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live or offline")
}

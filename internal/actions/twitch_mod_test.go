package actions

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingMod struct {
	mu       sync.Mutex
	deletes  []modDelete
	timeouts []modTimeout
	bans     []modBan
	err      error
}

type modDelete struct{ channel, messageID string }
type modTimeout struct {
	channel, userID, reason string
	seconds                 int
}
type modBan struct{ channel, userID, reason string }

func (m *recordingMod) DeleteMessage(channel, messageID string) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletes = append(m.deletes, modDelete{channel, messageID})
	return nil
}

func (m *recordingMod) Timeout(channel, userID, reason string, seconds int) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timeouts = append(m.timeouts, modTimeout{channel, userID, reason, seconds})
	return nil
}

func (m *recordingMod) Ban(channel, userID, reason string) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bans = append(m.bans, modBan{channel, userID, reason})
	return nil
}

func TestTwitchModActions_Registered(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{TwitchMod: &recordingMod{}}))
	for _, id := range []string{"twitch:delete-message", "twitch:timeout", "twitch:ban"} {
		_, ok := reg.Action(id)
		assert.True(t, ok, id)
	}
}

func TestTwitchDeleteMessage_DefaultsToTriggerMessage(t *testing.T) {
	mod := &recordingMod{}
	act := twitchDeleteMessageAction{mod: mod}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"),
		Trigger{Channel: "chan", MessageID: "msg-1"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.NoError(t, err)
	require.Len(t, mod.deletes, 1)
	assert.Equal(t, modDelete{"chan", "msg-1"}, mod.deletes[0])
}

func TestTwitchDeleteMessage_ExplicitMessageID(t *testing.T) {
	mod := &recordingMod{}
	act := twitchDeleteMessageAction{mod: mod}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"),
		Trigger{Channel: "chan", MessageID: "msg-1"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"message_id": "other"}))
	require.NoError(t, err)
	require.Len(t, mod.deletes, 1)
	assert.Equal(t, "other", mod.deletes[0].messageID)
}

func TestTwitchDeleteMessage_NoMessageIDErrors(t *testing.T) {
	act := twitchDeleteMessageAction{mod: &recordingMod{}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no message id")
}

func TestTwitchDeleteMessage_NilModErrors(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, RegisterBuiltins(reg, Services{}))
	act, _ := reg.Action("twitch:delete-message")
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"),
		Trigger{Channel: "chan", MessageID: "m"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestTwitchTimeout_DefaultsUserAndDuration(t *testing.T) {
	mod := &recordingMod{}
	act := twitchTimeoutAction{mod: mod}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"),
		Trigger{Channel: "chan", UserID: "u-9"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"reason": "spam"}))
	require.NoError(t, err)
	require.Len(t, mod.timeouts, 1)
	assert.Equal(t, modTimeout{"chan", "u-9", "spam", defaultTimeoutSeconds}, mod.timeouts[0])
}

func TestTwitchTimeout_ExplicitUserAndDuration(t *testing.T) {
	mod := &recordingMod{}
	act := twitchTimeoutAction{mod: mod}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"),
		Trigger{Channel: "chan", UserID: "u-9"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"user_id": "u-42", "duration_seconds": 30}))
	require.NoError(t, err)
	require.Len(t, mod.timeouts, 1)
	assert.Equal(t, "u-42", mod.timeouts[0].userID)
	assert.Equal(t, 30, mod.timeouts[0].seconds)
}

func TestTwitchTimeout_NoUserErrors(t *testing.T) {
	act := twitchTimeoutAction{mod: &recordingMod{}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"), Trigger{Channel: "chan"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no user id")
}

func TestTwitchBan_DefaultsUser(t *testing.T) {
	mod := &recordingMod{}
	act := twitchBanAction{mod: mod}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"),
		Trigger{Channel: "chan", UserID: "u-9"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{"reason": "bye"}))
	require.NoError(t, err)
	require.Len(t, mod.bans, 1)
	assert.Equal(t, modBan{"chan", "u-9", "bye"}, mod.bans[0])
}

func TestTwitchBan_ModErrorSurfaces(t *testing.T) {
	act := twitchBanAction{mod: &recordingMod{err: errors.New("401")}}
	ec := newExecutionContext(context.Background(), sampleRule("chan", "t"),
		Trigger{Channel: "chan", UserID: "u-9"})

	_, err := act.Execute(ec, mustJSON(t, map[string]any{}))
	require.Error(t, err)
}

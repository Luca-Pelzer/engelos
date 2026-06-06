package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type addTimerCall struct {
	channel         string
	name            string
	message         string
	intervalSeconds int
	minChatLines    int
	createdBy       string
}

type fakeTimerStore struct {
	addCalls    []addTimerCall
	removeCalls [][2]string
	listResult  []commands.TimerInfo
	addErr      error
	removeErr   error
	listErr     error
}

func (f *fakeTimerStore) AddTimer(_ context.Context, channel, name, message string, intervalSeconds, minChatLines int, createdBy string) error {
	f.addCalls = append(f.addCalls, addTimerCall{channel, name, message, intervalSeconds, minChatLines, createdBy})
	return f.addErr
}

func (f *fakeTimerStore) RemoveTimer(_ context.Context, channel, name string) error {
	f.removeCalls = append(f.removeCalls, [2]string{channel, name})
	return f.removeErr
}

func (f *fakeTimerStore) ListTimers(_ context.Context, _ string) ([]commands.TimerInfo, error) {
	return f.listResult, f.listErr
}

func TestAddTimer_ParsesAndCallsStore(t *testing.T) {
	store := &fakeTimerStore{}
	cmd := commands.NewAddTimerCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(), modMsg("!addtimer rules 600 Follow the rules!"),
		[]string{"rules", "600", "Follow", "the", "rules!"})

	require.Len(t, store.addCalls, 1)
	c := store.addCalls[0]
	assert.Equal(t, testChannel, c.channel)
	assert.Equal(t, "rules", c.name)
	assert.Equal(t, 600, c.intervalSeconds)
	assert.Equal(t, "Follow the rules!", c.message)
	assert.Equal(t, 0, c.minChatLines)
	assert.Equal(t, testViewer, c.createdBy)
	assert.Contains(t, reply.Text, "added timer 'rules'")
	assert.Contains(t, reply.Text, "600s")
}

func TestAddTimer_StripsBangFromName(t *testing.T) {
	store := &fakeTimerStore{}
	cmd := commands.NewAddTimerCommand(store)
	cmd.Handler(context.Background(), modMsg("!addtimer !Rules 600 hi"),
		[]string{"!Rules", "600", "hi"})
	require.Len(t, store.addCalls, 1)
	assert.Equal(t, "rules", store.addCalls[0].name)
}

func TestAddTimer_TooFewArgs(t *testing.T) {
	store := &fakeTimerStore{}
	cmd := commands.NewAddTimerCommand(store)
	for _, args := range [][]string{{}, {"rules"}, {"rules", "600"}} {
		reply := cmd.Handler(context.Background(), modMsg("!addtimer"), args)
		assert.Contains(t, reply.Text, "usage:")
	}
	assert.Empty(t, store.addCalls)
}

func TestAddTimer_NonNumericInterval(t *testing.T) {
	store := &fakeTimerStore{}
	cmd := commands.NewAddTimerCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!addtimer rules abc hi"),
		[]string{"rules", "abc", "hi"})
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.addCalls)
}

func TestAddTimer_StoreError(t *testing.T) {
	store := &fakeTimerStore{addErr: errors.New("boom")}
	cmd := commands.NewAddTimerCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!addtimer rules 600 hi"),
		[]string{"rules", "600", "hi"})
	assert.Contains(t, reply.Text, "couldn't add timer 'rules'")
}

func TestAddTimer_NilStore(t *testing.T) {
	cmd := commands.NewAddTimerCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!addtimer rules 600 hi"),
		[]string{"rules", "600", "hi"})
	assert.Contains(t, reply.Text, "timers are unavailable")
}

func TestDeleteTimer_CallsStore(t *testing.T) {
	store := &fakeTimerStore{}
	cmd := commands.NewDeleteTimerCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(), modMsg("!deltimer rules"), []string{"rules"})
	require.Len(t, store.removeCalls, 1)
	assert.Equal(t, [2]string{testChannel, "rules"}, store.removeCalls[0])
	assert.Contains(t, reply.Text, "deleted timer 'rules'")
}

func TestDeleteTimer_NoArg(t *testing.T) {
	store := &fakeTimerStore{}
	cmd := commands.NewDeleteTimerCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!deltimer"), nil)
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.removeCalls)
}

func TestDeleteTimer_NilStore(t *testing.T) {
	cmd := commands.NewDeleteTimerCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!deltimer rules"), []string{"rules"})
	assert.Contains(t, reply.Text, "timers are unavailable")
}

func TestListTimers_RendersList(t *testing.T) {
	store := &fakeTimerStore{listResult: []commands.TimerInfo{
		{Name: "rules", IntervalSeconds: 600, Enabled: true},
		{Name: "discord", IntervalSeconds: 1800, Enabled: true},
	}}
	cmd := commands.NewListTimersCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(), modMsg("!timers"), nil)
	assert.Contains(t, reply.Text, "rules (600s)")
	assert.Contains(t, reply.Text, "discord (1800s)")
}

func TestListTimers_Empty(t *testing.T) {
	store := &fakeTimerStore{}
	cmd := commands.NewListTimersCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!timers"), nil)
	assert.Contains(t, reply.Text, "no timers set")
}

func TestListTimers_NilStore(t *testing.T) {
	cmd := commands.NewListTimersCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!timers"), nil)
	assert.Contains(t, reply.Text, "timers are unavailable")
}

package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type addCounterCall struct {
	name  string
	delta int64
}

type setCounterCall struct {
	name  string
	value int64
}

type fakeCounterStore struct {
	value     int64
	valueOK   bool
	addReturn int64
	addErr    error
	setReturn int64
	setErr    error
	addCalls  []addCounterCall
	setCalls  []setCounterCall
}

func (f *fakeCounterStore) Value(_ context.Context, _, _ string) (int64, bool) {
	return f.value, f.valueOK
}

func (f *fakeCounterStore) Add(_ context.Context, _, name string, delta int64) (int64, error) {
	f.addCalls = append(f.addCalls, addCounterCall{name, delta})
	return f.addReturn, f.addErr
}

func (f *fakeCounterStore) Set(_ context.Context, _, name string, value int64) (int64, error) {
	f.setCalls = append(f.setCalls, setCounterCall{name, value})
	return f.setReturn, f.setErr
}

func TestCounter_Name(t *testing.T) {
	cmd := commands.NewCounterCommand(&fakeCounterStore{})
	assert.Equal(t, "counter", cmd.Name)
	assert.Equal(t, commands.NewCounterAddCommand(nil).Name, "counter+")
	assert.Equal(t, commands.NewCounterSubCommand(nil).Name, "counter-")
	assert.Equal(t, commands.NewSetCounterCommand(nil).Name, "setcounter")
	assert.Equal(t, commands.NewResetCounterCommand(nil).Name, "resetcounter")
}

func TestCounter_ShowsValue(t *testing.T) {
	store := &fakeCounterStore{value: 42, valueOK: true}
	cmd := commands.NewCounterCommand(store)
	assert.Equal(t, commands.RoleEveryone, cmd.MinRole)

	reply := cmd.Handler(context.Background(), msgText("!counter deaths"), []string{"deaths"})
	assert.Contains(t, reply.Text, "deaths: 42")
	assert.NotContains(t, reply.Text, "\n")
}

func TestCounter_Unknown(t *testing.T) {
	store := &fakeCounterStore{valueOK: false}
	cmd := commands.NewCounterCommand(store)
	reply := cmd.Handler(context.Background(), msgText("!counter deaths"), []string{"deaths"})
	assert.Contains(t, reply.Text, "no counter 'deaths' yet")
}

func TestCounter_MissingName(t *testing.T) {
	store := &fakeCounterStore{}
	cmd := commands.NewCounterCommand(store)
	reply := cmd.Handler(context.Background(), msgText("!counter"), nil)
	assert.Contains(t, reply.Text, "usage:")
}

func TestCounter_NilStore(t *testing.T) {
	cmd := commands.NewCounterCommand(nil)
	reply := cmd.Handler(context.Background(), msgText("!counter deaths"), []string{"deaths"})
	assert.Contains(t, reply.Text, "counters unavailable")
}

func TestCounterAdd_DefaultAndAmount(t *testing.T) {
	store := &fakeCounterStore{addReturn: 43}
	cmd := commands.NewCounterAddCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(), modMsg("!counter+ deaths"), []string{"deaths"})
	require.Len(t, store.addCalls, 1)
	assert.Equal(t, addCounterCall{"deaths", 1}, store.addCalls[0])
	assert.Contains(t, reply.Text, "deaths: 43")

	store.addReturn = 48
	reply = cmd.Handler(context.Background(), modMsg("!counter+ deaths 5"), []string{"deaths", "5"})
	require.Len(t, store.addCalls, 2)
	assert.Equal(t, addCounterCall{"deaths", 5}, store.addCalls[1])
	assert.Contains(t, reply.Text, "deaths: 48")
}

func TestCounterAdd_BadAmountUsage(t *testing.T) {
	store := &fakeCounterStore{}
	cmd := commands.NewCounterAddCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!counter+ deaths abc"), []string{"deaths", "abc"})
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.addCalls)
}

func TestCounterAdd_NilStore(t *testing.T) {
	cmd := commands.NewCounterAddCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!counter+ deaths"), []string{"deaths"})
	assert.Contains(t, reply.Text, "counters unavailable")
}

func TestCounterSub_DefaultAndAmount(t *testing.T) {
	store := &fakeCounterStore{addReturn: 41}
	cmd := commands.NewCounterSubCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(), modMsg("!counter- deaths"), []string{"deaths"})
	require.Len(t, store.addCalls, 1)
	assert.Equal(t, addCounterCall{"deaths", -1}, store.addCalls[0])
	assert.Contains(t, reply.Text, "deaths: 41")

	store.addReturn = 39
	reply = cmd.Handler(context.Background(), modMsg("!counter- deaths 3"), []string{"deaths", "3"})
	require.Len(t, store.addCalls, 2)
	assert.Equal(t, addCounterCall{"deaths", -3}, store.addCalls[1])
	assert.Contains(t, reply.Text, "deaths: 39")
}

func TestCounterSub_NilStore(t *testing.T) {
	cmd := commands.NewCounterSubCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!counter- deaths"), []string{"deaths"})
	assert.Contains(t, reply.Text, "counters unavailable")
}

func TestSetCounter_CallsStore(t *testing.T) {
	store := &fakeCounterStore{setReturn: 100}
	cmd := commands.NewSetCounterCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(), modMsg("!setcounter deaths 100"), []string{"deaths", "100"})
	require.Len(t, store.setCalls, 1)
	assert.Equal(t, setCounterCall{"deaths", 100}, store.setCalls[0])
	assert.Contains(t, reply.Text, "deaths set to 100")
	assert.NotContains(t, reply.Text, "\n")
}

func TestSetCounter_BadValueUsage(t *testing.T) {
	store := &fakeCounterStore{}
	cmd := commands.NewSetCounterCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!setcounter deaths abc"), []string{"deaths", "abc"})
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.setCalls)
}

func TestSetCounter_MissingValueUsage(t *testing.T) {
	store := &fakeCounterStore{}
	cmd := commands.NewSetCounterCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!setcounter deaths"), []string{"deaths"})
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.setCalls)
}

func TestSetCounter_NilStore(t *testing.T) {
	cmd := commands.NewSetCounterCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!setcounter deaths 1"), []string{"deaths", "1"})
	assert.Contains(t, reply.Text, "counters unavailable")
}

func TestResetCounter_SetsZero(t *testing.T) {
	store := &fakeCounterStore{setReturn: 0}
	cmd := commands.NewResetCounterCommand(store)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)

	reply := cmd.Handler(context.Background(), modMsg("!resetcounter deaths"), []string{"deaths"})
	require.Len(t, store.setCalls, 1)
	assert.Equal(t, setCounterCall{"deaths", 0}, store.setCalls[0])
	assert.Contains(t, reply.Text, "deaths reset to 0")
}

func TestResetCounter_MissingNameUsage(t *testing.T) {
	store := &fakeCounterStore{}
	cmd := commands.NewResetCounterCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!resetcounter"), nil)
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.setCalls)
}

func TestResetCounter_NilStore(t *testing.T) {
	cmd := commands.NewResetCounterCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!resetcounter deaths"), []string{"deaths"})
	assert.Contains(t, reply.Text, "counters unavailable")
}

func TestCounter_RegistersWithPlusMinusNames(t *testing.T) {
	store := &fakeCounterStore{addReturn: 1}
	e := commands.New(commands.Config{Logger: silentLogger()})
	require.NoError(t, e.Register(commands.NewCounterAddCommand(store)))
	require.NoError(t, e.Register(commands.NewCounterSubCommand(store)))

	msg := modMsg("!counter+ deaths")
	reply, ok := e.Handle(context.Background(), msg)
	assert.True(t, ok)
	assert.Contains(t, reply.Text, "deaths: 1")
}

func TestSetCounter_StoreErrorFriendlyReply(t *testing.T) {
	store := &fakeCounterStore{setErr: errors.New("boom")}
	cmd := commands.NewSetCounterCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!setcounter deaths 1"), []string{"deaths", "1"})
	assert.Contains(t, reply.Text, "couldn't set counter")
	assert.NotContains(t, reply.Text, "\n")
}

package commands_test

import (
	"context"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCoHostStore struct {
	enabled   bool
	name      string
	enaCalls  []setEconomyCall
	nameCalls []string
	personas  []string
}

func (f *fakeCoHostStore) CoHostStatus(_ context.Context, _ string) (bool, string) {
	name := f.name
	if name == "" {
		name = "bot"
	}
	return f.enabled, name
}

func (f *fakeCoHostStore) SetCoHostEnabled(_ context.Context, channel string, enabled bool) error {
	f.enaCalls = append(f.enaCalls, setEconomyCall{channel, enabled})
	return nil
}

func (f *fakeCoHostStore) SetCoHostName(_ context.Context, _, name string) error {
	f.nameCalls = append(f.nameCalls, name)
	return nil
}

func (f *fakeCoHostStore) SetCoHostPersona(_ context.Context, _, persona string) error {
	f.personas = append(f.personas, persona)
	return nil
}

func TestCoHostToggle_NameAndRole(t *testing.T) {
	cmd := commands.NewCoHostToggleCommand(&fakeCoHostStore{})
	assert.Equal(t, "cohost", cmd.Name)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)
}

func TestCoHostToggle_Status(t *testing.T) {
	store := &fakeCoHostStore{enabled: true, name: "engel"}
	cmd := commands.NewCoHostToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!cohost"), nil)
	assert.Contains(t, reply.Text, "ON")
	assert.Contains(t, reply.Text, "engel")
}

func TestCoHostToggle_OnOff(t *testing.T) {
	store := &fakeCoHostStore{}
	cmd := commands.NewCoHostToggleCommand(store)
	cmd.Handler(context.Background(), modMsg("!cohost on"), []string{"on"})
	cmd.Handler(context.Background(), modMsg("!cohost off"), []string{"off"})
	require.Len(t, store.enaCalls, 2)
	assert.True(t, store.enaCalls[0].enabled)
	assert.False(t, store.enaCalls[1].enabled)
}

func TestCoHostToggle_SetName(t *testing.T) {
	store := &fakeCoHostStore{}
	cmd := commands.NewCoHostToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!cohost name engel"), []string{"name", "engel"})
	require.Len(t, store.nameCalls, 1)
	assert.Equal(t, "engel", store.nameCalls[0])
	assert.Contains(t, reply.Text, "engel")
}

func TestCoHostToggle_SetPersona(t *testing.T) {
	store := &fakeCoHostStore{}
	cmd := commands.NewCoHostToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!cohost persona a witty gremlin"), []string{"persona", "a", "witty", "gremlin"})
	require.Len(t, store.personas, 1)
	assert.Equal(t, "a witty gremlin", store.personas[0])
	assert.Contains(t, reply.Text, "persona")
}

func TestCoHostToggle_NilStore(t *testing.T) {
	cmd := commands.NewCoHostToggleCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!cohost"), nil)
	assert.Contains(t, reply.Text, "unavailable")
}

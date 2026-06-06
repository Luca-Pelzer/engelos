package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type setEconomyCall struct {
	channel string
	enabled bool
}

type fakeFeatureToggleStore struct {
	enabled  bool
	setErr   error
	setCalls []setEconomyCall
}

func (f *fakeFeatureToggleStore) EconomyEnabled(_ context.Context, _ string) bool {
	return f.enabled
}

func (f *fakeFeatureToggleStore) SetEconomy(_ context.Context, channel string, enabled bool) error {
	f.setCalls = append(f.setCalls, setEconomyCall{channel, enabled})
	return f.setErr
}

func TestEconomyToggle_NameAndRole(t *testing.T) {
	cmd := commands.NewEconomyToggleCommand(&fakeFeatureToggleStore{})
	assert.Equal(t, "economy", cmd.Name)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)
}

func TestEconomyToggle_StatusOn(t *testing.T) {
	store := &fakeFeatureToggleStore{enabled: true}
	cmd := commands.NewEconomyToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!economy"), nil)
	assert.Contains(t, reply.Text, "ON")
	assert.Empty(t, store.setCalls)
}

func TestEconomyToggle_StatusOff(t *testing.T) {
	store := &fakeFeatureToggleStore{enabled: false}
	cmd := commands.NewEconomyToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!economy status"), []string{"status"})
	assert.Contains(t, reply.Text, "OFF")
	assert.Empty(t, store.setCalls)
}

func TestEconomyToggle_TurnOff(t *testing.T) {
	store := &fakeFeatureToggleStore{enabled: true}
	cmd := commands.NewEconomyToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!economy off"), []string{"off"})
	require.Len(t, store.setCalls, 1)
	assert.Equal(t, setEconomyCall{"chan-A", false}, store.setCalls[0])
	assert.Contains(t, reply.Text, "OFF")
}

func TestEconomyToggle_TurnOn(t *testing.T) {
	store := &fakeFeatureToggleStore{enabled: false}
	cmd := commands.NewEconomyToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!economy on"), []string{"on"})
	require.Len(t, store.setCalls, 1)
	assert.Equal(t, setEconomyCall{"chan-A", true}, store.setCalls[0])
	assert.Contains(t, reply.Text, "ON")
}

func TestEconomyToggle_SetError(t *testing.T) {
	store := &fakeFeatureToggleStore{enabled: true, setErr: errors.New("boom")}
	cmd := commands.NewEconomyToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!economy off"), []string{"off"})
	assert.Contains(t, reply.Text, "couldn't update")
}

func TestEconomyToggle_BadArg(t *testing.T) {
	store := &fakeFeatureToggleStore{enabled: true}
	cmd := commands.NewEconomyToggleCommand(store)
	reply := cmd.Handler(context.Background(), modMsg("!economy wat"), []string{"wat"})
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, store.setCalls)
}

func TestEconomyToggle_NilStore(t *testing.T) {
	cmd := commands.NewEconomyToggleCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!economy"), nil)
	assert.Contains(t, reply.Text, "unavailable")
}

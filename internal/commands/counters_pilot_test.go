package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/commands"
)

// buildCounterPilotRouter mirrors cmd/engelos.buildCommandRouter's counters
// gating: the counter commands register only when the plugin's store is non-nil.
func buildCounterPilotRouter(t *testing.T, store commands.CounterStore) *commands.Engine {
	t.Helper()
	eng := commands.New(commands.Config{Logger: silentLogger()})
	if store != nil {
		require.NoError(t, eng.Register(commands.NewCounterCommand(store)))
		require.NoError(t, eng.Register(commands.NewCounterAddCommand(store)))
		require.NoError(t, eng.Register(commands.NewCounterSubCommand(store)))
		require.NoError(t, eng.Register(commands.NewSetCounterCommand(store)))
		require.NoError(t, eng.Register(commands.NewResetCounterCommand(store)))
	}
	return eng
}

func TestCountersPilot_EnabledRegistersCommands(t *testing.T) {
	store := &fakeCounterStore{value: 7, valueOK: true}
	eng := buildCounterPilotRouter(t, store)

	reply, handled := eng.Handle(context.Background(), msgText("!counter deaths"))
	assert.True(t, handled, "counters enabled: !counter is registered and handled")
	assert.Contains(t, reply.Text, "deaths: 7")
}

func TestCountersPilot_DisabledUnregistersCommands(t *testing.T) {
	eng := buildCounterPilotRouter(t, nil) // plugin disabled => nil store => no registration

	for _, cmd := range []string{"!counter deaths", "!counter+ deaths", "!counter- deaths", "!setcounter deaths 5", "!resetcounter deaths"} {
		_, handled := eng.Handle(context.Background(), modMsg(cmd))
		assert.False(t, handled,
			"counters disabled: %q must be unregistered so the router reports it unhandled", cmd)
	}
}

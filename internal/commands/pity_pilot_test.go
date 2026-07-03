package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/commands"
)

// buildPityPilotRouter mirrors cmd/engelos.buildCommandRouter's pity gating: the
// !pity command registers only when the pity system is present. A nil querier
// (plugin disabled) leaves it unregistered so the router reports it unhandled.
func buildPityPilotRouter(t *testing.T, q commands.PityQuerier) *commands.Engine {
	t.Helper()
	eng := commands.New(commands.Config{Logger: silentLogger()})
	if q != nil {
		require.NoError(t, eng.Register(commands.NewPityCommand(testTenant, q)))
	}
	return eng
}

func TestPityPilot_EnabledRegistersCommand(t *testing.T) {
	eng := buildPityPilotRouter(t, &fakePity{status: commands.PityStatus{Points: 12, EffectiveChance: 0.1}})

	_, handled := eng.Handle(context.Background(), msgText("!pity"))
	assert.True(t, handled, "pity enabled: !pity is registered and handled")
}

func TestPityPilot_DisabledUnregistersCommand(t *testing.T) {
	eng := buildPityPilotRouter(t, nil) // plugin disabled => no querier => no registration

	_, handled := eng.Handle(context.Background(), msgText("!pity"))
	assert.False(t, handled, "pity disabled: !pity must be unregistered (router reports unhandled)")
}

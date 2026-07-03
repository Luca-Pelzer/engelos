package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/commands"
)

// buildStreakPilotRouter mirrors cmd/engelos.buildCommandRouter's streak gating:
// the !streak command registers only when the streak system is present. A nil
// querier (plugin disabled) leaves it unregistered so the router reports it
// unhandled.
func buildStreakPilotRouter(t *testing.T, q commands.StreakQuerier) *commands.Engine {
	t.Helper()
	eng := commands.New(commands.Config{Logger: silentLogger()})
	if q != nil {
		require.NoError(t, eng.Register(commands.NewStreakCommand(testTenant, q)))
	}
	return eng
}

func TestStreakPilot_EnabledRegistersCommand(t *testing.T) {
	eng := buildStreakPilotRouter(t, &fakeStreak{status: commands.StreakStatus{DaysCurrent: 3}})

	_, handled := eng.Handle(context.Background(), msgText("!streak"))
	assert.True(t, handled, "streak enabled: !streak is registered and handled")
}

func TestStreakPilot_DisabledUnregistersCommand(t *testing.T) {
	eng := buildStreakPilotRouter(t, nil) // plugin disabled => no querier => no registration

	_, handled := eng.Handle(context.Background(), msgText("!streak"))
	assert.False(t, handled, "streak disabled: !streak must be unregistered (router reports unhandled)")
}

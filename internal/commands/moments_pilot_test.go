package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/commands"
)

// buildMomentsPilotRouter mirrors cmd/engelos.buildCommandRouter's moments
// gating: !moment/!here register only when the controller is non-nil (the
// plugin is enabled). A nil controller (disabled) leaves them unregistered.
func buildMomentsPilotRouter(t *testing.T, ctrl commands.MomentController) *commands.Engine {
	t.Helper()
	eng := commands.New(commands.Config{Logger: silentLogger()})
	if ctrl != nil {
		require.NoError(t, eng.Register(commands.NewMomentCommand(ctrl)))
		require.NoError(t, eng.Register(commands.NewHereCommand(ctrl)))
	}
	return eng
}

func TestMomentsPilot_EnabledRegistersCommands(t *testing.T) {
	eng := buildMomentsPilotRouter(t, &fakeMomentController{})

	_, handled := eng.Handle(context.Background(), modMsg("!moment GG"))
	assert.True(t, handled, "moments enabled: !moment is registered and handled")
}

func TestMomentsPilot_DisabledUnregistersCommands(t *testing.T) {
	eng := buildMomentsPilotRouter(t, nil) // plugin disabled => nil controller => no registration

	for _, cmd := range []string{"!moment GG", "!here"} {
		_, handled := eng.Handle(context.Background(), modMsg(cmd))
		assert.False(t, handled,
			"moments disabled: %q must be unregistered so the router reports it unhandled", cmd)
	}
}

package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/commands"
)

// pilotLoyaltyProvider is a minimal commands.LoyaltyProvider for the pilot test.
type pilotLoyaltyProvider struct{ bal int64 }

func (p pilotLoyaltyProvider) Balance(context.Context, string, string) (int64, commands.LoyaltyError) {
	return p.bal, commands.LoyaltyOK
}
func (p pilotLoyaltyProvider) Transfer(context.Context, string, string, string, int64) (commands.LoyaltyError, string) {
	return commands.LoyaltyOK, ""
}
func (p pilotLoyaltyProvider) Top(context.Context, string, int) []commands.LoyaltyEntry { return nil }

// buildLoyaltyPilotRouter mirrors cmd/engelos.buildCommandRouter's loyalty
// gating: the loyalty commands register only when the provider is non-nil (the
// plugin is enabled). A nil provider (disabled) leaves them unregistered.
func buildLoyaltyPilotRouter(t *testing.T, provider commands.LoyaltyProvider) *commands.Engine {
	t.Helper()
	eng := commands.New(commands.Config{Logger: silentLogger()})
	if provider != nil {
		require.NoError(t, eng.Register(commands.NewPointsCommand(provider)))
		require.NoError(t, eng.Register(commands.NewGiveCommand(provider)))
		require.NoError(t, eng.Register(commands.NewPointsLeaderboardCommand(provider)))
	}
	return eng
}

func TestLoyaltyPilot_EnabledRegistersCommands(t *testing.T) {
	eng := buildLoyaltyPilotRouter(t, pilotLoyaltyProvider{bal: 500})

	_, handled := eng.Handle(context.Background(), msgText("!points"))
	assert.True(t, handled, "loyalty enabled: !points is registered and handled")
}

func TestLoyaltyPilot_DisabledUnregistersCommands(t *testing.T) {
	eng := buildLoyaltyPilotRouter(t, nil) // plugin disabled => nil provider => no registration

	for _, cmd := range []string{"!points", "!give @alice 100"} {
		_, handled := eng.Handle(context.Background(), msgText(cmd))
		assert.False(t, handled,
			"loyalty disabled: %q must be unregistered so the router reports it unhandled", cmd)
	}
}

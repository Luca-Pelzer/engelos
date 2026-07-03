package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/commands"
)

// buildQuotePilotRouter mirrors cmd/engelos.buildCommandRouter's quotes gating:
// the quote commands register only when the plugin's store is non-nil (enabled).
// It returns the command engine so the test can route through the same path the
// daemon uses, proving the restart-based boundary at the command-router layer.
func buildQuotePilotRouter(t *testing.T, store commands.QuoteStore) *commands.Engine {
	t.Helper()
	eng := commands.New(commands.Config{Logger: silentLogger()})
	if store != nil {
		require.NoError(t, eng.Register(commands.NewAddQuoteCommand(store)))
		require.NoError(t, eng.Register(commands.NewQuoteCommand(store)))
		require.NoError(t, eng.Register(commands.NewDeleteQuoteCommand(store)))
	}
	return eng
}

func TestQuotesPilot_EnabledRegistersCommands(t *testing.T) {
	store := &fakeQuoteStore{randomOK: true, randomView: commands.QuoteView{Number: 1, Text: "a wild quote"}}
	eng := buildQuotePilotRouter(t, store)

	reply, handled := eng.Handle(context.Background(), msgText("!quote"))
	assert.True(t, handled, "quotes enabled: !quote is registered and handled")
	assert.Contains(t, reply.Text, "a wild quote")
}

func TestQuotesPilot_DisabledUnregistersCommands(t *testing.T) {
	eng := buildQuotePilotRouter(t, nil) // plugin disabled => nil store => no registration

	for _, cmd := range []string{"!quote", "!quote 1", "!addquote hello", "!delquote 1"} {
		_, handled := eng.Handle(context.Background(), modMsg(cmd))
		assert.False(t, handled,
			"quotes disabled: %q must be unregistered so the router reports it unhandled", cmd)
	}
}

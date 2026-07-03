package builtins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/integrations"
)

func TestBuiltins_Avatar_ManifestAndNodes(t *testing.T) {
	integ := Avatar(nil, nil, func(context.Context, string) bool { return true })

	m := integ.Manifest()
	assert.Equal(t, "avatar", m.ID)
	assert.Equal(t, "Avatar", m.Name)
	assert.Equal(t, integrations.AuthNone, m.AuthKind)
	assert.Equal(t, "/integrations", m.SetupHref)

	// The two avatar nodes are exposed ONLY via the integration, never as
	// builtins — mirroring the tts/obs/ai/discord contract.
	actionsReg := actions.NewRegistry()
	require.NoError(t, actions.RegisterBuiltins(actionsReg, actions.Services{}))
	assert.False(t, hasAction(actionsReg, "avatar:speak"), "avatar:speak must not be a builtin")
	assert.False(t, hasAction(actionsReg, "avatar:expression"), "avatar:expression must not be a builtin")

	require.NoError(t, integ.RegisterNodes(actionsReg))
	assert.True(t, hasAction(actionsReg, "avatar:speak"), "avatar:speak exposed via the integration")
	assert.True(t, hasAction(actionsReg, "avatar:expression"), "avatar:expression exposed via the integration")
}

func TestBuiltins_Avatar_ConnectedProbe(t *testing.T) {
	on := Avatar(nil, nil, func(context.Context, string) bool { return true }).(integrations.ConnectionProber)
	assert.True(t, on.Connected(context.Background(), "local"))

	off := Avatar(nil, nil, nil).(integrations.ConnectionProber)
	assert.False(t, off.Connected(context.Background(), "local"))
}

package builtins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/integrations"
)

// integrationNodeIDs are the action nodes the three first-party integrations
// contribute; none of them may be a builtin.
var integrationNodeIDs = []string{
	"tts:speak",
	"obs:switch-scene", "obs:set-source-visibility",
	"ai:generate", "ai:classify",
}

func hasAction(reg *actions.Registry, id string) bool {
	for _, def := range reg.Actions() {
		if def.ID == id {
			return true
		}
	}
	return false
}

func newAllThree() *integrations.Registry {
	reg := integrations.NewRegistry()
	_ = reg.Register(ElevenLabs(nil, nil))
	_ = reg.Register(OBS(nil, nil))
	_ = reg.Register(AIBackend(nil, nil))
	return reg
}

func TestBuiltins_NodesAutoExposedOnlyViaIntegrations(t *testing.T) {
	actionsReg := actions.NewRegistry()
	require.NoError(t, actions.RegisterBuiltins(actionsReg, actions.Services{}))

	// The five integration nodes are NOT builtins.
	for _, id := range integrationNodeIDs {
		assert.False(t, hasAction(actionsReg, id), "%q must not be a builtin", id)
	}

	// Registering the three integrations exposes exactly those five nodes.
	require.NoError(t, newAllThree().RegisterAllNodes(actionsReg))
	for _, id := range integrationNodeIDs {
		assert.True(t, hasAction(actionsReg, id), "%q must be exposed after RegisterAllNodes", id)
	}
}

func TestBuiltins_WithoutIntegrationsNodesAbsent(t *testing.T) {
	actionsReg := actions.NewRegistry()
	require.NoError(t, actions.RegisterBuiltins(actionsReg, actions.Services{}))
	// An empty integrations registry contributes nothing.
	require.NoError(t, integrations.NewRegistry().RegisterAllNodes(actionsReg))
	for _, id := range integrationNodeIDs {
		assert.False(t, hasAction(actionsReg, id), "%q must be absent without its integration", id)
	}
}

func TestBuiltins_Manifests(t *testing.T) {
	el := ElevenLabs(nil, nil).Manifest()
	assert.Equal(t, "elevenlabs", el.ID)
	assert.Equal(t, integrations.AuthAPIKey, el.AuthKind)
	assert.Equal(t, "/tts", el.SetupHref)

	obs := OBS(nil, nil).Manifest()
	assert.Equal(t, "obs", obs.ID)
	assert.Equal(t, integrations.AuthNone, obs.AuthKind)

	ai := AIBackend(nil, nil).Manifest()
	assert.Equal(t, "ai-backend", ai.ID)
	assert.Equal(t, integrations.AuthAPIKey, ai.AuthKind)
	assert.Equal(t, "/ai-mod/settings", ai.SetupHref)
}

func TestBuiltins_ConnectedProbe(t *testing.T) {
	connected := ElevenLabs(nil, func(context.Context, string) bool { return true })
	prober, ok := connected.(integrations.ConnectionProber)
	require.True(t, ok, "integration must implement ConnectionProber")
	assert.True(t, prober.Connected(context.Background(), "local"))

	// A nil probe reports not connected.
	off := AIBackend(nil, nil).(integrations.ConnectionProber)
	assert.False(t, off.Connected(context.Background(), "local"))
}

func TestBuiltins_CredentialSpecs(t *testing.T) {
	el := ElevenLabs(nil, nil).(integrations.CredentialProvider)
	require.Len(t, el.CredentialSpec().Fields, 1)
	assert.Equal(t, "api_key", el.CredentialSpec().Fields[0].Key)

	// OBS is config-only (AuthNone) and exposes no credential spec.
	_, ok := OBS(nil, nil).(integrations.CredentialProvider)
	assert.False(t, ok, "obs must not be a credential provider")
}

// recordingPoster is a test DiscordPoster the discord integration wires into
// discord:reply.
type recordingPoster struct{ sent int }

func (r *recordingPoster) PostMessage(string, string) error { r.sent++; return nil }

func TestBuiltins_Discord_ManifestAndNode(t *testing.T) {
	poster := &recordingPoster{}
	integ := Discord(poster, func(context.Context, string) bool { return true })

	m := integ.Manifest()
	assert.Equal(t, "discord", m.ID)
	assert.Equal(t, "Discord", m.Name)
	assert.Equal(t, integrations.AuthNone, m.AuthKind)

	// RegisterNodes contributes discord:reply into the actions catalog.
	actionsReg := actions.NewRegistry()
	require.NoError(t, actions.RegisterBuiltins(actionsReg, actions.Services{}))
	assert.False(t, hasAction(actionsReg, "discord:reply"), "discord:reply is not a builtin")
	require.NoError(t, integ.RegisterNodes(actionsReg))
	assert.True(t, hasAction(actionsReg, "discord:reply"), "discord:reply exposed via the integration")

	// Connected probe reflects the gateway state.
	prober := integ.(integrations.ConnectionProber)
	assert.True(t, prober.Connected(context.Background(), "local"))
	off := Discord(poster, nil).(integrations.ConnectionProber)
	assert.False(t, off.Connected(context.Background(), "local"))
}

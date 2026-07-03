package integrations

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
)

// fixtureAction is a test-only action node an integration contributes.
type fixtureAction struct{ id string }

func (a fixtureAction) Definition() actions.PluginDefinition {
	return actions.PluginDefinition{ID: a.id, Name: "Fixture node", Description: "test node"}
}

func (fixtureAction) Execute(*actions.ExecutionContext, json.RawMessage) (*actions.ActionResult, error) {
	return nil, nil
}

// fixtureIntegration registers exactly one action node.
type fixtureIntegration struct {
	id     string
	nodeID string
}

func (f fixtureIntegration) Manifest() Manifest {
	return Manifest{ID: f.id, Name: "Fixture " + f.id, Description: "a test integration", AuthKind: AuthAPIKey}
}

func (f fixtureIntegration) RegisterNodes(reg *actions.Registry) error {
	return reg.RegisterAction(fixtureAction{id: f.nodeID})
}

func hasAction(reg *actions.Registry, id string) bool {
	for _, def := range reg.Actions() {
		if def.ID == id {
			return true
		}
	}
	return false
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(fixtureIntegration{id: "elevenlabs", nodeID: "elevenlabs:speak"}))
	got, ok := r.Get("elevenlabs")
	require.True(t, ok)
	assert.Equal(t, "elevenlabs", got.Manifest().ID)

	_, ok = r.Get("missing")
	assert.False(t, ok)
}

func TestRegistry_DuplicateIDRejected(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(fixtureIntegration{id: "obs", nodeID: "obs:x"}))
	err := r.Register(fixtureIntegration{id: "obs", nodeID: "obs:y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistry_EmptyIDRejected(t *testing.T) {
	r := NewRegistry()
	err := r.Register(fixtureIntegration{id: "  ", nodeID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty id")
}

func TestRegistry_ListStableOrder(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(fixtureIntegration{id: "zeta", nodeID: "zeta:x"}))
	require.NoError(t, r.Register(fixtureIntegration{id: "alpha", nodeID: "alpha:x"}))
	require.NoError(t, r.Register(fixtureIntegration{id: "mike", nodeID: "mike:x"}))
	list := r.List()
	require.Len(t, list, 3)
	assert.Equal(t, "alpha", list[0].ID)
	assert.Equal(t, "mike", list[1].ID)
	assert.Equal(t, "zeta", list[2].ID)
}

func TestRegisterAllNodes_AutoExposesInActionsCatalog(t *testing.T) {
	intReg := NewRegistry()
	require.NoError(t, intReg.Register(fixtureIntegration{id: "elevenlabs", nodeID: "elevenlabs:speak"}))

	actionsReg := actions.NewRegistry()
	// Before wiring: the integration's node is absent from the actions catalog.
	assert.False(t, hasAction(actionsReg, "elevenlabs:speak"))

	require.NoError(t, intReg.RegisterAllNodes(actionsReg))

	// After wiring: it appears in the catalog with no internal/actions changes.
	assert.True(t, hasAction(actionsReg, "elevenlabs:speak"))
}

func TestRegisterAllNodes_UnregisteredIntegrationAbsent(t *testing.T) {
	intReg := NewRegistry() // nothing registered
	actionsReg := actions.NewRegistry()
	require.NoError(t, intReg.RegisterAllNodes(actionsReg))
	assert.False(t, hasAction(actionsReg, "elevenlabs:speak"))
}

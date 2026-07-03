package sdkbridge_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/api/handlers"
	"github.com/Luca-Pelzer/engelos/internal/integrations"
	"github.com/Luca-Pelzer/engelos/internal/plugins"
	"github.com/Luca-Pelzer/engelos/internal/sdkbridge"
	"github.com/Luca-Pelzer/engelos/pkg/sdk"
	"github.com/Luca-Pelzer/engelos/pkg/sdk/examples/greeter"
)

func hasAction(reg *actions.Registry, id string) bool {
	for _, d := range reg.Actions() {
		if d.ID == id {
			return true
		}
	}
	return false
}

// TestRegisterSDKAction_CatalogAndExecute proves a bridged SDK action appears in
// the actions catalog and runs through the engine's ActionType surface, writing
// its outputs. The greeter reads its config (which the engine pre-substitutes).
func TestRegisterSDKAction_CatalogAndExecute(t *testing.T) {
	reg := actions.NewRegistry()
	require.NoError(t, sdkbridge.RegisterSDKAction(reg, greeter.HelloAction()))

	assert.True(t, hasAction(reg, "greeter:hello"), "node appears in the catalog")

	act, ok := reg.Action("greeter:hello")
	require.True(t, ok)
	res, err := act.Execute(&actions.ExecutionContext{}, json.RawMessage(`{"name":"World"}`))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "Hello, World!", res.Outputs["text"])
}

// echoAction is a test-only SDK action exercising the full ExecContext surface:
// it reads a trigger-data value and writes an output.
type echoAction struct{}

func (echoAction) Definition() sdk.Definition {
	return sdk.Definition{ID: "test:echo", Name: "Echo", Description: "test"}
}
func (echoAction) Execute(ec sdk.ExecContext, _ json.RawMessage) (*sdk.Result, error) {
	v, _ := ec.Data("who")
	ec.SetOutput("seen", v)
	return &sdk.Result{Outputs: map[string]any{"channel": ec.Channel(), "user": ec.Username()}}, nil
}

func TestRegisterSDKAction_ExecContextSurface(t *testing.T) {
	reg := actions.NewRegistry()
	require.NoError(t, sdkbridge.RegisterSDKAction(reg, echoAction{}))
	act, ok := reg.Action("test:echo")
	require.True(t, ok)

	ec := &actions.ExecutionContext{
		Channel: "streamer1",
		Trigger: actions.Trigger{Username: "bob", Data: map[string]any{"who": "alice"}},
	}
	res, err := act.Execute(ec, nil)
	require.NoError(t, err)
	assert.Equal(t, "streamer1", res.Outputs["channel"])
	assert.Equal(t, "bob", res.Outputs["user"])
	got, ok := ec.Output("seen")
	require.True(t, ok, "SetOutput wrote through the bridge into the real context")
	assert.Equal(t, "alice", got)
}

func TestRegisterSDKPlugin_AppearsInPluginsAPI(t *testing.T) {
	reg := plugins.NewRegistry()
	require.NoError(t, sdkbridge.RegisterSDKPlugin(reg, greeter.Plugin()))

	h := handlers.NewPlugins(reg, nil, map[string]bool{"greeter": false}, "local", nil)
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var views []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &views))
	require.Len(t, views, 1)
	assert.Equal(t, "greeter", views[0]["id"])
	assert.Equal(t, "experimental", views[0]["tier"])
	assert.Equal(t, false, views[0]["default_enabled"])
}

// credIntegration is a test-only SDK integration that contributes one node and a
// credential spec, exercising the integration + CredentialProvider bridge.
type credIntegration struct{}

func (credIntegration) Manifest() sdk.IntegrationManifest {
	return sdk.IntegrationManifest{ID: "acme", Name: "Acme", Description: "test", AuthKind: sdk.AuthAPIKey}
}
func (credIntegration) Nodes() sdk.Nodes {
	return sdk.Nodes{Actions: []sdk.Action{greeter.HelloAction()}}
}
func (credIntegration) CredentialSpec() sdk.CredentialSpec {
	return sdk.CredentialSpec{Fields: []sdk.CredentialField{{Key: "api_key", Label: "API key"}}}
}

func TestRegisterSDKIntegration_NodesAndCreds(t *testing.T) {
	intReg := integrations.NewRegistry()
	require.NoError(t, sdkbridge.RegisterSDKIntegration(intReg, credIntegration{}))

	actReg := actions.NewRegistry()
	require.NoError(t, intReg.RegisterAllNodes(actReg))
	assert.True(t, hasAction(actReg, "greeter:hello"), "integration node reaches the catalog")

	got, ok := intReg.Get("acme")
	require.True(t, ok)
	cp, ok := got.(integrations.CredentialProvider)
	require.True(t, ok, "an SDK integration with creds bridges to a CredentialProvider")
	assert.Equal(t, "api_key", cp.CredentialSpec().Fields[0].Key)
}

func TestSDKIntegration_NoCredsWhenAbsent(t *testing.T) {
	_, ok := sdkbridge.SDKIntegration(plainIntegration{}).(integrations.CredentialProvider)
	assert.False(t, ok, "an SDK integration without creds does not expose a CredentialProvider")
}

// plainIntegration is a test-only SDK integration with no credential spec.
type plainIntegration struct{}

func (plainIntegration) Manifest() sdk.IntegrationManifest {
	return sdk.IntegrationManifest{ID: "plain", Name: "Plain", AuthKind: sdk.AuthNone}
}
func (plainIntegration) Nodes() sdk.Nodes { return sdk.Nodes{} }

// Package sdkbridge adapts public pkg/sdk implementations into the daemon's
// private registries (internal/actions, internal/integrations, internal/plugins).
// It is the one place that imports both the SDK and internal packages, keeping
// pkg/sdk free of any internal/ dependency: a third-party extension compiles
// against pkg/sdk only, and the daemon wires it in through the RegisterSDK*
// helpers here.
package sdkbridge

import (
	"encoding/json"

	"github.com/Luca-Pelzer/engelos/internal/actions"
	"github.com/Luca-Pelzer/engelos/internal/integrations"
	"github.com/Luca-Pelzer/engelos/internal/plugins"
	"github.com/Luca-Pelzer/engelos/pkg/sdk"
)

// execContext adapts *actions.ExecutionContext to the narrow sdk.ExecContext.
type execContext struct{ ec *actions.ExecutionContext }

func (a execContext) Data(key string) (any, bool) {
	v, ok := a.ec.Trigger.Data[key]
	return v, ok
}
func (a execContext) Output(key string) (any, bool)   { return a.ec.Output(key) }
func (a execContext) SetOutput(key string, value any) { a.ec.SetOutput(key, value) }
func (a execContext) Channel() string                 { return a.ec.Channel }
func (a execContext) Username() string                { return a.ec.Trigger.Username }

func toDefinition(d sdk.Definition) actions.PluginDefinition {
	return actions.PluginDefinition{ID: d.ID, Name: d.Name, Description: d.Description}
}

// sdkAction adapts an sdk.Action to actions.ActionType.
type sdkAction struct{ impl sdk.Action }

func (a sdkAction) Definition() actions.PluginDefinition { return toDefinition(a.impl.Definition()) }

func (a sdkAction) Execute(ec *actions.ExecutionContext, config json.RawMessage) (*actions.ActionResult, error) {
	res, err := a.impl.Execute(execContext{ec: ec}, config)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return &actions.ActionResult{Outputs: res.Outputs, Stop: res.Stop}, nil
}

// sdkCondition adapts an sdk.Condition to actions.ConditionType.
type sdkCondition struct{ impl sdk.Condition }

func (c sdkCondition) Definition() actions.PluginDefinition { return toDefinition(c.impl.Definition()) }

func (c sdkCondition) Evaluate(ec *actions.ExecutionContext, config json.RawMessage) (bool, error) {
	return c.impl.Evaluate(execContext{ec: ec}, config)
}

// RegisterSDKAction registers an SDK action node into the actions catalog.
func RegisterSDKAction(reg *actions.Registry, impl sdk.Action) error {
	return reg.RegisterAction(sdkAction{impl: impl})
}

// RegisterSDKCondition registers an SDK condition node into the actions catalog.
func RegisterSDKCondition(reg *actions.Registry, impl sdk.Condition) error {
	return reg.RegisterCondition(sdkCondition{impl: impl})
}

// sdkIntegration adapts an sdk.Integration to integrations.Integration.
type sdkIntegration struct{ impl sdk.Integration }

func (i sdkIntegration) Manifest() integrations.Manifest {
	m := i.impl.Manifest()
	return integrations.Manifest{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Icon:        m.Icon,
		AuthKind:    integrations.AuthKind(m.AuthKind),
		SetupHref:   m.SetupHref,
	}
}

func (i sdkIntegration) RegisterNodes(reg *actions.Registry) error {
	nodes := i.impl.Nodes()
	for _, a := range nodes.Actions {
		if err := RegisterSDKAction(reg, a); err != nil {
			return err
		}
	}
	for _, c := range nodes.Conditions {
		if err := RegisterSDKCondition(reg, c); err != nil {
			return err
		}
	}
	return nil
}

// sdkIntegrationWithCreds additionally exposes the integration's credential
// spec, so the integrations handler renders its setup form. It is used only when
// the SDK implementation also satisfies sdk.CredentialProvider.
type sdkIntegrationWithCreds struct {
	sdkIntegration
	cp sdk.CredentialProvider
}

func (i sdkIntegrationWithCreds) CredentialSpec() integrations.CredentialSpec {
	s := i.cp.CredentialSpec()
	fields := make([]integrations.CredentialField, len(s.Fields))
	for idx, f := range s.Fields {
		fields[idx] = integrations.CredentialField{Key: f.Key, Label: f.Label, Hint: f.Hint}
	}
	return integrations.CredentialSpec{Fields: fields}
}

// SDKIntegration wraps an sdk.Integration as an integrations.Integration,
// exposing its credential spec only when the implementation provides one.
func SDKIntegration(impl sdk.Integration) integrations.Integration {
	base := sdkIntegration{impl: impl}
	if cp, ok := impl.(sdk.CredentialProvider); ok {
		return sdkIntegrationWithCreds{sdkIntegration: base, cp: cp}
	}
	return base
}

// RegisterSDKIntegration registers an SDK integration into the integrations
// registry. Its nodes reach the actions catalog on the registry's RegisterAllNodes.
func RegisterSDKIntegration(reg *integrations.Registry, impl sdk.Integration) error {
	return reg.Register(SDKIntegration(impl))
}

// sdkPlugin adapts an sdk.Plugin to plugins.Plugin.
type sdkPlugin struct{ impl sdk.Plugin }

func (p sdkPlugin) Manifest() plugins.Manifest {
	m := p.impl.Manifest()
	return plugins.Manifest{
		ID:             m.ID,
		Name:           m.Name,
		Description:    m.Description,
		Tier:           plugins.Tier(m.Tier),
		SettingsHref:   m.SettingsHref,
		DefaultEnabled: m.DefaultEnabled,
	}
}

// SDKPlugin wraps an sdk.Plugin as a plugins.Plugin.
func SDKPlugin(impl sdk.Plugin) plugins.Plugin { return sdkPlugin{impl: impl} }

// RegisterSDKPlugin registers an SDK plugin manifest into the plugins registry.
func RegisterSDKPlugin(reg *plugins.Registry, impl sdk.Plugin) error {
	return reg.Register(SDKPlugin(impl))
}

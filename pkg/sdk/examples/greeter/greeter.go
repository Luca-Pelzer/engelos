// SPDX-License-Identifier: Apache-2.0

// Package greeter is a complete, self-contained example engelOS extension. It
// contributes one workflow action node ("greeter:hello") and a toggleable
// plugin manifest, using only the public pkg/sdk — no internal/ imports. The
// daemon wires it in through internal/sdkbridge.
package greeter

import (
	"encoding/json"
	"fmt"

	"github.com/Luca-Pelzer/engelos/pkg/sdk"
)

// helloConfig is the JSON config the greeter:hello node accepts. The engine
// substitutes $(...) variables in these string values before Execute runs, so
// Name may be a literal or e.g. "$(username)".
type helloConfig struct {
	Name      string `json:"name"`
	OutputKey string `json:"output_key"`
}

// helloAction renders "Hello, {name}!" into an output key (default "text").
type helloAction struct{}

func (helloAction) Definition() sdk.Definition {
	return sdk.Definition{
		ID:          "greeter:hello",
		Name:        "Greeter / hello",
		Description: `Renders "Hello, {name}!" into output_key (default text) from the name config.`,
	}
}

func (helloAction) Execute(_ sdk.ExecContext, config json.RawMessage) (*sdk.Result, error) {
	var c helloConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, fmt.Errorf("greeter:hello: invalid config: %w", err)
		}
	}
	name := c.Name
	if name == "" {
		name = "there"
	}
	key := c.OutputKey
	if key == "" {
		key = "text"
	}
	return &sdk.Result{Outputs: map[string]any{key: fmt.Sprintf("Hello, %s!", name)}}, nil
}

// HelloAction returns the greeter's single action node.
func HelloAction() sdk.Action { return helloAction{} }

// greeterPlugin is the toggleable dashboard entry for the example. It ships
// default-off so an operator opts in, and carries no stores or routes: it exists
// to surface the greeter:hello node under a named, tierable plugin.
type greeterPlugin struct{}

func (greeterPlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		ID:             "greeter",
		Name:           "Greeter (SDK example)",
		Description:    "Example SDK extension contributing the greeter:hello workflow action node.",
		Tier:           sdk.TierExperimental,
		DefaultEnabled: false,
	}
}

// Plugin returns the greeter plugin manifest.
func Plugin() sdk.Plugin { return greeterPlugin{} }

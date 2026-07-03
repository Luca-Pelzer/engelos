<!-- SPDX-License-Identifier: Apache-2.0 -->

# engelOS SDK (`pkg/sdk`)

The public Go SDK for building **compiled-in** engelOS extensions: workflow
action/condition nodes, integrations, and toggleable plugins.

- **License:** Apache-2.0 (see [`LICENSE`](./LICENSE)) — not AGPL-3.0 like the
  core daemon, so you can build extensions without an AGPL compliance burden.
- **Dependency direction:** `pkg/sdk` imports **only the standard library** and
  never anything under `internal/`. The daemon adapts your implementation into
  its private registries through `internal/sdkbridge`. This is enforced by a
  test (`TestSDK_NoInternalImports`).

## Write an action node

An action implements [`sdk.Action`](./action.go). It decodes its own config
(the engine substitutes `$(...)` variables in string values before `Execute`)
and returns outputs later nodes and `$(...)` variables can read.

```go
package greeter

import (
	"encoding/json"
	"fmt"

	"github.com/Luca-Pelzer/engelos/pkg/sdk"
)

type helloAction struct{}

func (helloAction) Definition() sdk.Definition {
	return sdk.Definition{
		ID:          "greeter:hello",
		Name:        "Greeter / hello",
		Description: `Renders "Hello, {name}!" into output_key (default text).`,
	}
}

func (helloAction) Execute(_ sdk.ExecContext, config json.RawMessage) (*sdk.Result, error) {
	var c struct {
		Name      string `json:"name"`
		OutputKey string `json:"output_key"`
	}
	if len(config) > 0 {
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, fmt.Errorf("greeter:hello: invalid config: %w", err)
		}
	}
	if c.Name == "" {
		c.Name = "there"
	}
	if c.OutputKey == "" {
		c.OutputKey = "text"
	}
	return &sdk.Result{Outputs: map[string]any{c.OutputKey: fmt.Sprintf("Hello, %s!", c.Name)}}, nil
}

// HelloAction returns the node for registration.
func HelloAction() sdk.Action { return helloAction{} }
```

## Add a plugin manifest

A [`sdk.Plugin`](./plugin.go) is a dashboard-toggleable feature. Third-party
plugins typically ship `DefaultEnabled: false` so operators opt in.

```go
type greeterPlugin struct{}

func (greeterPlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		ID:             "greeter",
		Name:           "Greeter (SDK example)",
		Description:    "Example SDK extension contributing the greeter:hello action node.",
		Tier:           sdk.TierExperimental,
		DefaultEnabled: false,
	}
}

// Plugin returns the manifest for registration.
func Plugin() sdk.Plugin { return greeterPlugin{} }
```

## Register it in the daemon

Wire your extension in `cmd/engelos` through `internal/sdkbridge`, alongside the
built-in registrations:

```go
// Plugin manifest -> appears in GET /api/v1/plugins (default-off).
sdkbridge.RegisterSDKPlugin(pluginRegistry, greeter.Plugin())

// Action node -> appears in the actions catalog and runs in rules.
sdkbridge.RegisterSDKAction(actionsRegistry, greeter.HelloAction())
```

The bridge also exposes `RegisterSDKCondition` and `RegisterSDKIntegration`
(the latter bundles nodes with an [`sdk.IntegrationManifest`](./integration.go)
and an optional [`sdk.CredentialProvider`](./integration.go)).

## Full example

The complete, self-contained extension used above lives in
[`examples/greeter`](./examples/greeter) and imports only `pkg/sdk`.

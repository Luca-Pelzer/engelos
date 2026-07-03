// SPDX-License-Identifier: Apache-2.0

package sdk

// Tier classifies a plugin by how close it sits to the product core, so the
// dashboard can group and caveat them. It is descriptive metadata only; it does
// not affect gating.
type Tier string

const (
	// TierLegacy is a pre-plugin feature migrated behind the plugin boundary.
	TierLegacy Tier = "legacy"
	// TierTemplate is a packaged workflow/template offering.
	TierTemplate Tier = "template"
	// TierExperimental is an opt-in, possibly-unstable feature.
	TierExperimental Tier = "experimental"
	// TierCoreAdjacent is close to core but still independently toggleable.
	TierCoreAdjacent Tier = "core-adjacent"
)

// PluginManifest is the static, dashboard-facing description a plugin exposes.
// ID is the stable slug used as the registry key and the persisted toggle key.
// DefaultEnabled is the state applied when no explicit toggle is stored; a
// third-party plugin typically ships DefaultEnabled=false so operators opt in.
type PluginManifest struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Tier           Tier   `json:"tier"`
	SettingsHref   string `json:"settings_href,omitempty"`
	DefaultEnabled bool   `json:"default_enabled"`
}

// Plugin is a self-describing, toggleable feature. Manifest returns its static
// metadata including its default enablement. Enablement is restart-based: the
// daemon reads the resolved state at startup and wires the plugin's surfaces
// accordingly, so the interface needs no runtime lifecycle hook.
type Plugin interface {
	Manifest() PluginManifest
}

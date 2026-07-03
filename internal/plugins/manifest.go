// Package plugins is the manifest + lifecycle boundary through which a legacy
// or optional feature (quotes, counters, loyalty, ...) declares itself once and
// becomes a toggleable, compiled-in Go plugin. It formalises what the codebase
// already does ad-hoc with nil-guarded stores and the /api/v1/capabilities map:
// a feature is "on" when its store is opened and its routes/commands are wired.
//
// Lifecycle model: RESTART-BASED. Chat commands register only at startup
// (commands.Engine has no Unregister) and chi routes mount at router-build time,
// so unmounting either at runtime is unsafe. This mirrors the existing
// songrequests precedent (RES-19), where an env flag gates store-opening and a
// disabled feature simply 404s its routes and never registers its commands. A
// toggle therefore PERSISTS IMMEDIATELY but TAKES EFFECT ON NEXT RESTART. The
// API reports both states so the dashboard can show "restart required":
//
//   - enabled (current): what is actually mounted in THIS process, captured at
//     startup from the persisted state and passed to the handler as a snapshot.
//   - desired (persisted): the state a PUT writes, applied on the next restart.
//
// Default state comes from each plugin's Manifest.DefaultEnabled, preserving
// today's behaviour exactly: quotes defaults on (its store is always opened
// today), a songrequests-style feature defaults off.
package plugins

// Tier classifies a plugin by how close it sits to the product core, so the
// dashboard can group and caveat them. It is descriptive metadata only; it does
// not affect gating.
type Tier string

const (
	// TierLegacy is a pre-plugin feature migrated behind this boundary.
	TierLegacy Tier = "legacy"
	// TierTemplate is a packaged workflow/template offering.
	TierTemplate Tier = "template"
	// TierExperimental is an opt-in, possibly-unstable feature.
	TierExperimental Tier = "experimental"
	// TierCoreAdjacent is close to core but still independently toggleable.
	TierCoreAdjacent Tier = "core-adjacent"
)

// Manifest is the static, dashboard-facing description a plugin exposes. ID is
// the stable slug used as the registry key and the persisted state key.
// DefaultEnabled is the state applied when no explicit toggle is stored, and
// must reproduce the feature's pre-plugin default exactly.
type Manifest struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Tier           Tier   `json:"tier"`
	SettingsHref   string `json:"settings_href,omitempty"`
	DefaultEnabled bool   `json:"default_enabled"`
}

// Plugin is a self-describing, toggleable feature. Manifest returns its static
// metadata including its default enablement; the actual store-opening and
// route/command wiring lives in cmd/engelos and the router, gated on the
// resolved enabled state, because the restart-based model needs no runtime hook.
type Plugin interface {
	Manifest() Manifest
}

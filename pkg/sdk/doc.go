// SPDX-License-Identifier: Apache-2.0

// Package sdk is the public Go SDK for building compiled-in engelOS extensions:
// workflow action/condition nodes, integrations, and toggleable plugins.
//
// It is licensed under Apache-2.0 (NOT AGPL-3.0 like the core daemon), so any
// company, tool vendor, or independent developer can build extensions against
// engelOS without an AGPL compliance burden. See pkg/sdk/LICENSE.
//
// # Dependency direction
//
// This package depends ONLY on the standard library. It never imports anything
// under internal/, so an extension author compiles against a stable, minimal
// surface. The core daemon adapts SDK implementations into its private
// registries through the internal/sdkbridge package (the SDK interfaces + a thin
// adapter layer in internal/, rather than moving core types out of internal/).
// This keeps the diff small and every existing internal test unchanged: the SDK
// mirrors the internal concepts and the bridge converts between them.
//
// # Building an extension
//
// Implement [Action] (and/or [Condition]) for workflow nodes, and optionally
// [Plugin] for a dashboard-toggleable feature or [Integration] to bundle nodes
// with a credential manifest. Register them from the daemon with the
// internal/sdkbridge RegisterSDK* helpers. See examples/greeter for a complete,
// self-contained extension that imports only this package.
package sdk

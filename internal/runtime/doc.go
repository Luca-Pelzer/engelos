// Package runtime wires platform adapters to feature subsystems. It runs a
// Dispatcher goroutine that consumes adapters.Event values from every
// connected platform and forwards them to pity points, the WebSocket hub,
// and (in later phases) other features.
//
// The runtime owns no business logic; it is a thin fan-in router. Adapter
// adapters and feature implementations live in their own packages and are
// passed in via the Dispatcher's Config.
package runtime

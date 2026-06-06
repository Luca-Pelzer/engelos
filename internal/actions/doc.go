// Package actions is the engelOS Action-Engine: user-defined automation rules
// of the shape Trigger -> Conditions -> Actions, the core product surface that
// makes engelOS a competitor to Streamer.bot and Firebot.
//
// A [Rule] binds one trigger (an incoming platform event, a chat command, a
// timer, ...) to an ordered list of conditions that gate execution and an
// ordered list of actions to run when the conditions pass. Both conditions and
// actions are extensible plugins registered in a string-keyed [Registry]; each
// plugin owns an opaque json.RawMessage config blob it decodes itself. This is
// the same decoupling Firebot uses (effect id + per-effect model) and is the
// foundation a future visual node-editor and addon-marketplace build on: the
// editor draws and edits the very same data model the engine executes.
//
// # Layers
//
// The package is split so the logic never couples to a presentation layer:
//
//   - model.go    the persisted data model ([Rule], [ConditionList], [ActionList])
//   - context.go  the runtime [ExecutionContext] handed to every plugin
//   - registry.go the plugin contracts ([ConditionType], [ActionType]) + [Registry]
//   - vars.go     safe $(variable) substitution into action configs
//   - engine.go   the [Engine]: rule matching, a bounded worker pool, the executor
//   - store.go    SQLite persistence (mirrors internal/counters conventions)
//   - builtins.go the first-party condition and action plugins
//
// # Decoupling
//
// This package imports nothing under engelos/internal. The runtime dispatcher
// talks to a narrow interface; main wires a thin adapter and injects side-effect
// dependencies (a chat sender) through [Services]. Keeping the dependency
// one-way avoids import cycles and lets the engine evolve independently.
package actions

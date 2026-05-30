// Package customcommands persists streamer-defined chat commands -
// the "!addcom !hello Welcome $user!" feature familiar from Nightbot
// and StreamElements.
//
// The store is SQLite-backed (pure-Go via modernc.org/sqlite). Commands
// are scoped to (tenant, channel); name is unique inside that scope and
// stored lower-cased with the prefix stripped, so callers may pass
// either "!hello" or "hello". Responses are stored RAW: placeholders
// like $user, $channel and $args are expanded by the commands engine
// at send time (see [github.com/Luca-Pelzer/engelos/internal/commands.ExpandVariables]),
// NOT by this package.
//
// # Decoupling
//
// This package does NOT import internal/commands. The commands engine
// consumes a narrow Resolver and a narrow CustomCommandStore interface
// (both declared in internal/commands); main.go wires a thin adapter
// over [Store]. Keeping the dependency one-way avoids an import cycle
// and lets the persistence layer evolve independently of the engine.
package customcommands

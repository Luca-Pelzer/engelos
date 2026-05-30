// Package rewards persists streamer-defined loyalty rewards - the
// "!reward add coffee 500 A hot coffee" feature familiar from channel
// loyalty systems. Viewers redeem these with loyalty points via
// !redeem, and !rewards lists what is available.
//
// The store is SQLite-backed (pure-Go via modernc.org/sqlite). Rewards
// are scoped to (tenant, channel); name is unique inside that scope and
// stored lower-cased and trimmed, so callers may pass "Coffee" or
// "  coffee  " interchangeably. Cost is in loyalty points (a positive
// integer) and description is optional human-readable text shown in the
// !rewards listing.
//
// # Decoupling
//
// This package stands alone: it imports no other engelos/internal
// package. It stores ONLY the reward definitions - deducting loyalty
// points, checking balances and recording redemptions are the concern
// of the commands layer, which composes this [Store] with the loyalty
// ledger. main.go wires the store and the !reward/!rewards/!redeem
// commands on top of it. Keeping persistence one-way and dependency-free
// lets it evolve independently of the command engine.
package rewards

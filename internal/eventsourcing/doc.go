// Package eventsourcing implements the immutable, append-only event log that
// sits at the heart of engelOS.
//
// # Model
//
// Every interesting fact that happens in a streaming community - a chat
// message, a subscription, a moderation action, an integration callback - is
// recorded as an Event. Events are immutable: once written, they are never
// updated or deleted. Read-models (user profiles, loyalty ledgers, analytics,
// yearly Wrapped-cards) are derived by replaying events. Replays also enable
// time-travel debugging and AI-training datasets.
//
// # Identity & ordering
//
// Each Event has a ULID identifier ([github.com/oklog/ulid/v2]) which is both
// globally unique and lexicographically sortable by time. The lexicographic
// order of IDs matches the order in which events were generated, which makes
// cursor-based pagination (AfterID) trivial.
//
// # Multi-tenancy
//
// Every Event carries a TenantID. Reads MUST always filter by tenant; the
// SQLite implementation enforces this at query time and via indexes. There is
// no API to read across tenants - this property keeps engelOS commercial-ready
// from day 1 without any later schema refactor.
//
// # Schema evolution
//
// Each Event has a Version field on its payload schema. New consumers must
// tolerate older versions; producers MUST bump Version when the payload shape
// changes incompatibly. The Payload itself is a [encoding/json.RawMessage] -
// the event log is intentionally agnostic of the concrete payload Go type.
//
// # Causation
//
// CorrelationID groups events that belong to the same logical request or
// flow. CausationID points at the immediate predecessor event that triggered
// this one. Both are optional ULIDs.
//
// # Storage
//
// The default implementation is a pure-Go SQLite store
// (modernc.org/sqlite, no CGO). It uses WAL mode, foreign keys, and per-tenant
// composite indexes for time-range and type-filtered queries.
package eventsourcing

-- engelOS counters package: named integer counters per channel.
--
-- One row per (tenant, channel, name) counter — the classic "death
-- counter" feature (e.g. "!deaths" for a Souls-game streamer). The name
-- is the trigger argument WITHOUT a prefix, lower-cased. value may be
-- negative; there is no floor (callers decide).
--
-- The UNIQUE (tenant_id, channel, name) constraint is REQUIRED: it is the
-- conflict target for the atomic "INSERT ... ON CONFLICT DO UPDATE" upsert
-- used by Add/Set, so concurrent increments cannot lose an update.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS counters (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    channel     TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    value       INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, channel, name)
);

CREATE INDEX IF NOT EXISTS idx_counters_channel
    ON counters (tenant_id, channel);

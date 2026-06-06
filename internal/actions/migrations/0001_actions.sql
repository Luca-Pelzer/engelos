-- engelOS actions package: the Action-Engine's persisted automation rules.
--
-- One row per (tenant, channel, name) rule. A rule binds a trigger to an
-- ordered condition list and action list, both stored as JSON blobs the engine
-- and its plugins decode. trigger_filter is a trigger-kind-specific JSON
-- predicate the engine uses to decide whether a fired trigger matches.
--
-- The JSON columns keep the schema stable as new condition/action plugins ship:
-- a new plugin type means new config shapes inside the blobs, never a new
-- column. schema_version stamps the row so a future loader can migrate blobs
-- whose meaning changed under a plugin type id.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS rules (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    channel        TEXT NOT NULL,
    name           TEXT NOT NULL,
    enabled        INTEGER NOT NULL DEFAULT 1,
    trigger_kind   TEXT NOT NULL,
    trigger_filter TEXT NOT NULL DEFAULT '',
    conditions     TEXT NOT NULL DEFAULT '{}',
    actions        TEXT NOT NULL DEFAULT '{}',
    schema_version INTEGER NOT NULL DEFAULT 1,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_rules_tenant_channel_name
    ON rules(tenant_id, channel, name);

CREATE INDEX IF NOT EXISTS idx_rules_tenant_channel_enabled
    ON rules(tenant_id, channel, enabled);

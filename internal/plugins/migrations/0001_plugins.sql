-- engelOS plugins package: per-tenant plugin enable/disable state.
--
-- One row per (tenant, plugin_id) EXPLICIT desired state. A plugin with no
-- row falls back to its Manifest.DefaultEnabled, so today's per-feature
-- defaults are preserved exactly until an operator toggles the plugin.
--
-- The lifecycle is restart-based: this table holds the DESIRED state a PUT
-- writes; the running process keeps whatever it mounted at startup until the
-- next restart re-reads this table.
--
-- The UNIQUE (tenant_id, plugin_id) constraint is REQUIRED: it is the
-- conflict target for the atomic "INSERT ... ON CONFLICT DO UPDATE" upsert
-- used by Set, so concurrent writers cannot leave a duplicate row.
--
-- updated_at is stored as Unix seconds (UTC). enabled is 0/1.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS plugin_state (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    plugin_id   TEXT    NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, plugin_id)
);

CREATE INDEX IF NOT EXISTS idx_plugin_state_tenant
    ON plugin_state (tenant_id);

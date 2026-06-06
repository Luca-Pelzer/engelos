-- engelOS customcommands package: streamer-defined chat commands.
--
-- One row per (tenant, channel, name) triggered text command. The
-- response is stored RAW — placeholders like $user/$channel/$args are
-- expanded by the commands engine at send time, not here.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS custom_commands (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    channel     TEXT NOT NULL,
    name        TEXT NOT NULL,
    response    TEXT NOT NULL,
    min_role    TEXT NOT NULL DEFAULT 'everyone',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_commands_tenant_channel_name
    ON custom_commands(tenant_id, channel, name);

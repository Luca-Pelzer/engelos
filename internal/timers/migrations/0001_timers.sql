-- engelOS timers package: periodic auto-announcements ("timers").
--
-- One row per (tenant, channel, name) timer. The scheduler posts message
-- to chat every interval_ns nanoseconds, optionally gated behind
-- min_chat_lines of chat activity since the last post so the bot never
-- talks to an empty room.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS timers (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    channel        TEXT NOT NULL,
    name           TEXT NOT NULL,
    message        TEXT NOT NULL,
    interval_ns    INTEGER NOT NULL,
    min_chat_lines INTEGER NOT NULL DEFAULT 0,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_timers_tenant_channel_name
    ON timers(tenant_id, channel, name);

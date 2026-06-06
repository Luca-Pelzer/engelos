-- engelOS wrapped package: live-accumulating per-viewer statistics for
-- the "Stream Wrapped" (Spotify-Wrapped-style) recap feature.
--
-- One row per (tenant, channel, viewer, period). Period buckets the
-- counters so both monthly ("YYYY-MM") and all-time ("all") Wrapped cards
-- can be produced from the same table. The dispatcher calls Increment*
-- as chat events happen; a report builder later reads the aggregates.
--
-- The UNIQUE (tenant_id, channel, viewer_id, period) constraint is
-- REQUIRED: it is the conflict target for the atomic "INSERT ... ON
-- CONFLICT DO UPDATE" upsert used by every Increment* method, so
-- concurrent writers cannot leave a duplicate row.
--
-- All timestamps (first_seen, last_seen, updated_at) are stored as Unix
-- nanoseconds (UTC). Counters default to 0 and only ever increase.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS wrapped_stats (
    id            TEXT    PRIMARY KEY,
    tenant_id     TEXT    NOT NULL,
    channel       TEXT    NOT NULL,
    viewer_id     TEXT    NOT NULL,
    username      TEXT    NOT NULL DEFAULT '',
    period        TEXT    NOT NULL,
    messages      INTEGER NOT NULL DEFAULT 0,
    subs_given    INTEGER NOT NULL DEFAULT 0,
    subs_total    INTEGER NOT NULL DEFAULT 0,
    raids_started INTEGER NOT NULL DEFAULT 0,
    first_seen    INTEGER NOT NULL,
    last_seen     INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    UNIQUE (tenant_id, channel, viewer_id, period)
);

-- Conflict target for the Increment* upserts.
CREATE UNIQUE INDEX IF NOT EXISTS idx_wrapped_stats_tenant_channel_viewer_period
    ON wrapped_stats (tenant_id, channel, viewer_id, period);

-- Covers TopChatters: filter by (tenant, channel, period) then order by
-- messages DESC.
CREATE INDEX IF NOT EXISTS idx_wrapped_stats_top_chatters
    ON wrapped_stats (tenant_id, channel, period, messages);

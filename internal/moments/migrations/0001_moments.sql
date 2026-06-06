-- engelOS moments package: BeReal-style "Moment Alerts".
--
-- A moderator opens a time-boxed Moment for a channel; viewers react with
-- !here within the window and earn an "I Was There" participant record.
-- When the Moment is ended it is assigned a rarity tier derived from the
-- number of participants.
--
-- At most ONE row with status='open' may exist per (tenant_id, channel);
-- this invariant is enforced in Go under a mutex, not by the schema, so
-- that a clean ErrActiveExists can be returned to callers.
--
-- Times are stored as Unix nanoseconds (INTEGER). closed_at is 0 while the
-- moment is still open.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS moments (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    channel     TEXT NOT NULL,
    title       TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open',
    rarity      TEXT NOT NULL DEFAULT '',
    opened_by   TEXT NOT NULL DEFAULT '',
    opened_at   INTEGER NOT NULL,
    closes_at   INTEGER NOT NULL,
    closed_at   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_moments_tenant_channel_status
    ON moments(tenant_id, channel, status);

CREATE INDEX IF NOT EXISTS idx_moments_tenant_channel_closed_at
    ON moments(tenant_id, channel, closed_at);

CREATE TABLE IF NOT EXISTS moment_participants (
    id          TEXT PRIMARY KEY,
    moment_id   TEXT NOT NULL,
    viewer_id   TEXT NOT NULL,
    username    TEXT NOT NULL,
    joined_at   INTEGER NOT NULL,
    UNIQUE(moment_id, viewer_id)
);

CREATE INDEX IF NOT EXISTS idx_moment_participants_moment
    ON moment_participants(moment_id);

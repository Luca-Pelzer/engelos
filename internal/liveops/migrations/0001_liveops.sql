-- engelOS liveops package: scheduled Live-Ops events per channel.
--
-- One row per scheduled event, scoped to (tenant, channel) — the "what's
-- next?" feature (e.g. "Double Points Weekend" or "Season 3 starts"). The
-- visible "number" is a per-(tenant, channel) 1-based sequence shown to
-- users (e.g. "!delevent 3"); it is NOT the primary key. The primary key
-- is a ULID "id". Deletes leave gaps in the number sequence on purpose so
-- a given number always refers to the same event.
--
-- starts_at is the UnixNano start time; ends_at is a nullable UnixNano end
-- time (NULL = no defined end — an instantaneous milestone like "Season 3
-- starts"). An event is "active" when starts_at <= now AND ends_at IS NOT
-- NULL AND ends_at >= now.
--
-- The UNIQUE (tenant_id, channel, number) constraint is REQUIRED: it backs
-- the per-channel 1-based numbering so two concurrent Adds (serialised by a
-- process mutex computing MAX(number)+1) can never share a number.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS events (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    channel     TEXT    NOT NULL,
    number      INTEGER NOT NULL,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    starts_at   INTEGER NOT NULL,
    ends_at     INTEGER,
    created_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, channel, number)
);

CREATE INDEX IF NOT EXISTS idx_events_channel_starts
    ON events (tenant_id, channel, starts_at);

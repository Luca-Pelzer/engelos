-- engelOS quotes package: memorable chat lines saved as numbered quotes.
--
-- One row per saved quote, scoped to (tenant, channel). The visible
-- "number" is a per-(tenant, channel) 1-based sequence shown to users
-- (e.g. "!quote 3"); it is NOT the primary key. The primary key is a
-- ULID "id". Deletes leave gaps in the number sequence on purpose so a
-- given number always refers to the same quote.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS quotes (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    channel     TEXT    NOT NULL,
    number      INTEGER NOT NULL,
    text        TEXT    NOT NULL,
    created_by  TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_quotes_channel_number
    ON quotes (tenant_id, channel, number);

CREATE INDEX IF NOT EXISTS idx_quotes_channel
    ON quotes (tenant_id, channel);

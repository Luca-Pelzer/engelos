-- engelOS songrequests package: per-channel song-request configuration.
--
-- One row per (tenant, channel) describing how song requests behave for a
-- channel: which music provider is active ("spotify", "youtube", or empty
-- when disabled), the Spotify playlist requests are queued into, the
-- maximum allowed track duration (seconds; 0 means no limit) and whether
-- the feature is enabled at all.
--
-- The UNIQUE (tenant_id, channel) constraint is REQUIRED: it is the
-- conflict target for the atomic "INSERT ... ON CONFLICT DO UPDATE" upsert
-- used by Set, so concurrent writers cannot leave a duplicate row.
--
-- updated_at is stored as Unix nanoseconds (UTC). enabled is 0/1.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS song_request_config (
    id                  TEXT    PRIMARY KEY,
    tenant_id           TEXT    NOT NULL,
    channel             TEXT    NOT NULL,
    provider            TEXT    NOT NULL DEFAULT '',
    spotify_playlist_id TEXT    NOT NULL DEFAULT '',
    max_duration_sec    INTEGER NOT NULL DEFAULT 0,
    enabled             INTEGER NOT NULL DEFAULT 0,
    updated_at          INTEGER NOT NULL,
    UNIQUE (tenant_id, channel)
);

CREATE INDEX IF NOT EXISTS idx_song_request_config_tenant_channel
    ON song_request_config (tenant_id, channel);

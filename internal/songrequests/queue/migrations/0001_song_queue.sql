-- engelOS songrequests/queue package: bot-managed per-channel song queue.
--
-- One row per queued/playing/played song request scoped to a
-- (tenant, channel). The bot owns the queue and a browser-source player
-- pulls the currently-playing item; FIFO order is enforced by a
-- monotonic `position` assigned per channel at enqueue time.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS song_queue (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    channel      TEXT NOT NULL,
    video_id     TEXT NOT NULL,
    title        TEXT NOT NULL,
    artist       TEXT NOT NULL DEFAULT '',
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    requested_by TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'queued',
    position     INTEGER NOT NULL,
    created_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_song_queue_tenant_channel_status_position
    ON song_queue(tenant_id, channel, status, position);

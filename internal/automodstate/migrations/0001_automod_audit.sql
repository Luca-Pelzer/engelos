-- engelOS automodstate package: the AutoMod audit log.
--
-- One row per enforcement action AutoMod takes (or, in dry-run/shadow mode,
-- would have taken). It is the durable memory streamers consult to review or
-- reverse a moderation decision: the full message text, which filter fired, the
-- matched substring, and the punishment applied are all captured at decision
-- time so nothing has to be looked up after the fact.
--
-- created_at is stored as Unix seconds (INTEGER, UTC); dry_run is a 0/1 flag.
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS automod_audit (
    id            TEXT    PRIMARY KEY,
    tenant_id     TEXT    NOT NULL,
    channel       TEXT    NOT NULL,
    user_id       TEXT    NOT NULL DEFAULT '',
    username      TEXT    NOT NULL DEFAULT '',
    message_id    TEXT    NOT NULL DEFAULT '',
    message_text  TEXT    NOT NULL DEFAULT '',
    filter_name   TEXT    NOT NULL,
    reason        TEXT    NOT NULL DEFAULT '',
    matched_text  TEXT    NOT NULL DEFAULT '',
    action        TEXT    NOT NULL,
    duration_sec  INTEGER NOT NULL DEFAULT 0,
    dry_run       INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
);

-- Recent-actions feed for a channel (List): newest first by (tenant, channel).
CREATE INDEX IF NOT EXISTS idx_automod_audit_channel
    ON automod_audit (tenant_id, channel, created_at DESC);

-- Per-user offense history (ListByUser): newest first by (tenant, channel, user).
CREATE INDEX IF NOT EXISTS idx_automod_audit_user
    ON automod_audit (tenant_id, channel, username, created_at DESC);

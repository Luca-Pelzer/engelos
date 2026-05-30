-- engelOS rewards package: streamer-defined loyalty rewards.
--
-- One row per (tenant, channel, name) redeemable item. A reward has a
-- points cost and an optional description; viewers spend loyalty points
-- to redeem it via !redeem. This package stores ONLY the reward
-- definitions — the commands layer handles spending points.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS rewards (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    channel     TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    cost        INTEGER NOT NULL,
    created_by  TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, channel, name)
);

CREATE INDEX IF NOT EXISTS idx_rewards_channel
    ON rewards (tenant_id, channel, cost);

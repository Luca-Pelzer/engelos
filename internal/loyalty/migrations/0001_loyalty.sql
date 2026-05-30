-- engelOS loyalty package: spendable points economy per viewer.
--
-- One row per (tenant, channel, viewer_id) account — a viewer's loyalty
-- standing in a channel. Points are earned by chatting (Earn) and spent
-- on games/rewards (Spend) or gifted to other viewers (Transfer). balance
-- is never negative; the floor is enforced both in Go and by the
-- Spend/Transfer transactions which refuse to overdraw.
--
-- The UNIQUE (tenant_id, channel, viewer_id) constraint is REQUIRED: it is
-- the conflict target for the atomic "INSERT ... ON CONFLICT DO UPDATE"
-- upsert used by Earn, so concurrent earns cannot lose an increment.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS loyalty_accounts (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    channel     TEXT    NOT NULL,
    viewer_id   TEXT    NOT NULL,
    username    TEXT    NOT NULL DEFAULT '',
    balance     INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, channel, viewer_id)
);

-- Leaderboard reads order by balance DESC within a (tenant, channel); the
-- composite index keeps "top N richest viewers" lookups index-only.
CREATE INDEX IF NOT EXISTS idx_loyalty_channel_balance
    ON loyalty_accounts (tenant_id, channel, balance DESC);

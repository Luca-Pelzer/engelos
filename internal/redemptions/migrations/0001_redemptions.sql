-- engelOS redemptions package: Channel-Points reward to action bindings.
--
-- One row per (tenant, channel, reward_id) binding — the Firebot-style
-- "when reward X is redeemed, do Y" table. reward_id is a Twitch
-- Custom-Reward UUID and is the trigger key the (future) executor looks up
-- via GetByReward to decide which action_type to run. reward_title is a
-- cached human label for dashboard display only (may be empty). The
-- primary key is a ULID "id".
--
-- enabled and auto_fulfill are booleans stored as INTEGER 0/1: a disabled
-- binding persists but is ignored by the executor, and auto_fulfill tells
-- the executor whether to mark the redemption FULFILLED/CANCELED on the
-- Twitch side.
--
-- The UNIQUE (tenant_id, channel, reward_id) constraint is REQUIRED: it is
-- the conflict target making exactly one binding per reward, so concurrent
-- Creates (serialised by a process mutex doing check-then-insert) collapse
-- to a single winner and the executor always resolves a redemption to one
-- action.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS redemption_bindings (
    id           TEXT    PRIMARY KEY,
    tenant_id    TEXT    NOT NULL,
    channel      TEXT    NOT NULL,
    reward_id    TEXT    NOT NULL,
    reward_title TEXT    NOT NULL DEFAULT '',
    action_type  TEXT    NOT NULL,
    action_param TEXT    NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    auto_fulfill INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (tenant_id, channel, reward_id)
);

CREATE INDEX IF NOT EXISTS idx_redemption_bindings_channel
    ON redemption_bindings (tenant_id, channel);

-- engelOS featureflags package: per-channel feature on/off toggles.
--
-- One row per (tenant, channel, feature) EXPLICIT override — e.g. the
-- "economy" mini-game enabled for a particular channel. The feature key
-- is lower-cased and constrained to [a-z0-9_]+ so it can be matched
-- without quoting. Only explicit overrides are stored; an absent row means
-- "no override" and the caller falls back to its own default.
--
-- The UNIQUE (tenant_id, channel, feature) constraint is REQUIRED: it is
-- the conflict target for the atomic "INSERT ... ON CONFLICT DO UPDATE"
-- upsert used by Set, so concurrent writers cannot leave a duplicate row.
--
-- updated_at is stored as Unix seconds (UTC). enabled is 0/1.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS feature_flags (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    channel     TEXT    NOT NULL,
    feature     TEXT    NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, channel, feature)
);

CREATE INDEX IF NOT EXISTS idx_feature_flags_channel
    ON feature_flags (tenant_id, channel);

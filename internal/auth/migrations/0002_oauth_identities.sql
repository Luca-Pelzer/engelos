-- engelOS auth package: OAuth identity storage.
--
-- One row per linked external identity (Twitch, Discord, ...).
-- Tokens are stored ENCRYPTED at rest via internal/secrets.Box (AES-256-GCM).
-- Purpose distinguishes user-facing SSO links from the single bot account
-- that drives outbound feed adapters per (tenant, provider).

CREATE TABLE IF NOT EXISTS oauth_identities (
    id                TEXT PRIMARY KEY,            -- ULID
    tenant_id         TEXT NOT NULL,
    user_id           TEXT NOT NULL,               -- FK users.id
    provider          TEXT NOT NULL,               -- "twitch" | "discord"
    provider_user_id  TEXT NOT NULL,               -- external account id
    provider_login    TEXT NOT NULL DEFAULT '',    -- external username/login (display)
    purpose           TEXT NOT NULL DEFAULT 'user',-- "user" (SSO) | "bot" (feeds adapter)
    access_token_enc  BLOB NOT NULL,               -- secrets.Box-encrypted access token
    refresh_token_enc BLOB,                        -- nullable, encrypted refresh token
    scopes            TEXT NOT NULL DEFAULT '',    -- space-separated granted scopes
    expires_at        INTEGER,                     -- nullable UnixNano of token expiry
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_provider_user
    ON oauth_identities(provider, provider_user_id);

CREATE INDEX IF NOT EXISTS idx_oauth_user
    ON oauth_identities(tenant_id, user_id);

CREATE INDEX IF NOT EXISTS idx_oauth_purpose
    ON oauth_identities(tenant_id, provider, purpose);

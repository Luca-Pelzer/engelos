-- engelOS workspaces package: managed channels, memberships and invitations.
--
-- A workspace is one managed channel (slug = lowercase channel login, the key
-- the feature stores already use). A membership links an auth user to a
-- workspace with a role (owner|mod). An invitation is a pending grant addressed
-- to a Twitch login, auto-accepted when that login next authenticates.
--
-- Multi-tenant envelope: tenant_id stays "default" for the OSS single binary;
-- workspaces live within a tenant so a future hosted build can reuse the schema.

CREATE TABLE IF NOT EXISTS workspaces (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL,
    slug              TEXT NOT NULL,
    twitch_channel_id TEXT NOT NULL DEFAULT '',
    twitch_login      TEXT NOT NULL DEFAULT '',
    display_name      TEXT NOT NULL DEFAULT '',
    owner_user_id     TEXT NOT NULL,
    auto_verify_mods  INTEGER NOT NULL DEFAULT 1,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_tenant_slug
    ON workspaces(tenant_id, slug);

CREATE TABLE IF NOT EXISTS memberships (
    workspace_id TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    role         TEXT NOT NULL,
    source       TEXT NOT NULL DEFAULT 'invite',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_memberships_user
    ON memberships(user_id);

CREATE TABLE IF NOT EXISTS invitations (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    twitch_login TEXT NOT NULL,
    role         TEXT NOT NULL,
    token        TEXT NOT NULL,
    invited_by   TEXT NOT NULL,
    accepted_at  INTEGER,
    expires_at   INTEGER,
    created_at   INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_invitations_token
    ON invitations(token);

CREATE INDEX IF NOT EXISTS idx_invitations_login_pending
    ON invitations(twitch_login, accepted_at);

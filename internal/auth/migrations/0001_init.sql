-- engelOS auth package: initial schema.
--
-- Multi-tenant by default: every row is keyed by (tenant_id, ...).
-- Self-hosted deployments use tenant_id = 'local'.

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    email           TEXT NOT NULL,
    username        TEXT NOT NULL,
    password_hash   BLOB NOT NULL,
    role            TEXT NOT NULL,
    totp_secret     BLOB,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    last_login_at   INTEGER NOT NULL DEFAULT 0,
    disabled        INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_tenant_email
    ON users(tenant_id, email);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_tenant_username
    ON users(tenant_id, username);

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,
    created_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    last_used_at    INTEGER NOT NULL,
    user_agent      TEXT NOT NULL DEFAULT '',
    remote_ip       TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id    ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS api_keys (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    key_hash        TEXT NOT NULL UNIQUE,
    prefix          TEXT NOT NULL DEFAULT '',
    scopes          TEXT NOT NULL,
    ip_whitelist    TEXT NOT NULL DEFAULT '',
    rate_limit      INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    created_by      TEXT NOT NULL,
    last_used_at    INTEGER NOT NULL DEFAULT 0,
    expires_at      INTEGER,
    revoked_at      INTEGER,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant   ON api_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);

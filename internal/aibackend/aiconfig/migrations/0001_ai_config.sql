-- 0001_ai_config.sql
--
-- Single-row-per-tenant AI backend configuration.
--
-- Exactly one row per tenant_id (the PRIMARY KEY, targeted by the upsert in
-- store.go via ON CONFLICT). Timestamps are stored as INTEGER UnixNano (UTC) to
-- keep the schema dependency-free and portable across SQLite builds. The
-- api_key_ciphertext column holds the provider API key already encrypted by the
-- caller (secrets.Box); the store is crypto-agnostic and never sees or produces
-- the plaintext.
CREATE TABLE IF NOT EXISTS ai_config (
	tenant_id            TEXT    PRIMARY KEY,
	provider             TEXT    NOT NULL DEFAULT '',
	base_url             TEXT    NOT NULL DEFAULT '',
	model                TEXT    NOT NULL DEFAULT '',
	api_key_ciphertext   BLOB,
	updated_at           INTEGER NOT NULL
);

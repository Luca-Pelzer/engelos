-- 0001_tts.sql
--
-- Per-(tenant, channel) text-to-speech configuration.
--
-- One row per (tenant_id, channel). Booleans are stored as INTEGER (0/1) and
-- timestamps as INTEGER UnixNano (UTC) to keep the schema dependency-free and
-- portable across SQLite builds. The api_key_ciphertext column holds the
-- ElevenLabs key already encrypted by the caller (secrets.Box); the store is
-- crypto-agnostic and never sees the plaintext. The UNIQUE(tenant_id, channel)
-- constraint is what the upsert in store.go targets via ON CONFLICT.
CREATE TABLE IF NOT EXISTS tts_config (
	id                   TEXT    PRIMARY KEY,
	tenant_id            TEXT    NOT NULL,
	channel              TEXT    NOT NULL,
	enabled              INTEGER NOT NULL DEFAULT 0,
	voice_id             TEXT    NOT NULL DEFAULT '',
	model                TEXT    NOT NULL DEFAULT 'eleven_flash_v2_5',
	api_key_ciphertext   BLOB,
	updated_at           INTEGER NOT NULL,
	UNIQUE (tenant_id, channel)
);

CREATE INDEX IF NOT EXISTS idx_tts_config_tenant_channel
	ON tts_config (tenant_id, channel);

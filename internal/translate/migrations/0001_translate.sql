-- 0001_translate.sql
--
-- Per-(tenant, channel) chat-translation configuration.
--
-- One row per (tenant_id, channel). Booleans are stored as INTEGER (0/1) and
-- timestamps as INTEGER UnixNano (UTC) to keep the schema dependency-free and
-- portable across SQLite builds. The UNIQUE(tenant_id, channel) constraint is
-- what the upsert in store.go targets via ON CONFLICT.
CREATE TABLE IF NOT EXISTS translate_config (
	id              TEXT    PRIMARY KEY,
	tenant_id       TEXT    NOT NULL,
	channel         TEXT    NOT NULL,
	enabled         INTEGER NOT NULL DEFAULT 0,
	target_lang     TEXT    NOT NULL DEFAULT 'en',
	output_mode     TEXT    NOT NULL DEFAULT 'chat',
	min_word_count  INTEGER NOT NULL DEFAULT 0,
	updated_at      INTEGER NOT NULL,
	UNIQUE (tenant_id, channel)
);

-- Lookups and listings are always scoped by tenant, usually by channel too.
CREATE INDEX IF NOT EXISTS idx_translate_config_tenant_channel
	ON translate_config (tenant_id, channel);

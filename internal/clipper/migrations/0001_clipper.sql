-- 0001_clipper.sql
--
-- Per-(tenant, channel) auto-clipper tuning.
--
-- One row per (tenant_id, channel). Booleans are stored as INTEGER (0/1) and
-- timestamps as INTEGER UnixNano (UTC) to keep the schema dependency-free and
-- portable across SQLite builds. The UNIQUE(tenant_id, channel) constraint is
-- what the upsert in store.go targets via ON CONFLICT. Numeric tuning fields
-- store 0 to mean "inherit the running default", matching Settings.ApplyTo.
CREATE TABLE IF NOT EXISTS clipper_config (
	id                  TEXT    PRIMARY KEY,
	tenant_id           TEXT    NOT NULL,
	channel             TEXT    NOT NULL,
	enabled             INTEGER NOT NULL DEFAULT 0,
	keyword_threshold   INTEGER NOT NULL DEFAULT 0,
	emote_threshold     INTEGER NOT NULL DEFAULT 0,
	copypasta_threshold INTEGER NOT NULL DEFAULT 0,
	min_messages        INTEGER NOT NULL DEFAULT 0,
	spike_factor        REAL    NOT NULL DEFAULT 0,
	composite_threshold REAL    NOT NULL DEFAULT 0,
	cooldown_seconds    INTEGER NOT NULL DEFAULT 0,
	updated_at          INTEGER NOT NULL,
	UNIQUE (tenant_id, channel)
);

-- Lookups and listings are always scoped by tenant, usually by channel too.
CREATE INDEX IF NOT EXISTS idx_clipper_config_tenant_channel
	ON clipper_config (tenant_id, channel);

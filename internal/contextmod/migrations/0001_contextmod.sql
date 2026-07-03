-- contextmod_config holds the per-(tenant, channel) AI context-moderation
-- settings.
--
-- Exactly one row exists per (tenant_id, channel) pair (enforced by the UNIQUE
-- constraint), written via an upsert. enabled defaults to 0 because AI
-- escalation spends the streamer's Claude subscription, so it is strictly
-- opt-in. rules is a plain-language description of the channel's moderation
-- policy, fed verbatim into the classifier's system prompt.
CREATE TABLE IF NOT EXISTS contextmod_config (
	id         TEXT    PRIMARY KEY,
	tenant_id  TEXT    NOT NULL,
	channel    TEXT    NOT NULL,
	enabled    INTEGER NOT NULL DEFAULT 0,
	rules      TEXT    NOT NULL DEFAULT '',
	updated_at INTEGER NOT NULL,
	UNIQUE (tenant_id, channel)
);

CREATE INDEX IF NOT EXISTS idx_contextmod_config_tenant_channel
	ON contextmod_config (tenant_id, channel);

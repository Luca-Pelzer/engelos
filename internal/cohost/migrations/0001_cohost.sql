-- cohost_config holds the per-(tenant, channel) AI co-host settings.
--
-- Exactly one row exists per (tenant_id, channel) pair (enforced by the UNIQUE
-- constraint), written via an upsert. enabled defaults to 0 because the co-host
-- spends the streamer's Claude subscription, so it is strictly opt-in. bot_name
-- is how viewers address the bot; persona is a short style instruction folded
-- into the system prompt; max_reply_len caps the characters posted back to chat.
CREATE TABLE IF NOT EXISTS cohost_config (
	id            TEXT    PRIMARY KEY,
	tenant_id     TEXT    NOT NULL,
	channel       TEXT    NOT NULL,
	enabled       INTEGER NOT NULL DEFAULT 0,
	bot_name      TEXT    NOT NULL DEFAULT 'bot',
	persona       TEXT    NOT NULL DEFAULT 'a friendly, concise stream co-host',
	max_reply_len INTEGER NOT NULL DEFAULT 280,
	updated_at    INTEGER NOT NULL,
	UNIQUE (tenant_id, channel)
);

CREATE INDEX IF NOT EXISTS idx_cohost_config_tenant_channel
	ON cohost_config (tenant_id, channel);

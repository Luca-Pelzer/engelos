-- Per-channel overlay bearer token for the avatar WebSocket relay. Exactly one
-- row per (tenant, channel); the token authenticates an OBS overlay page and is
-- treated like other per-channel config (stored as opaque text).
CREATE TABLE IF NOT EXISTS avatar_overlay_token (
	id          TEXT    PRIMARY KEY,
	tenant_id   TEXT    NOT NULL,
	channel     TEXT    NOT NULL,
	token       TEXT    NOT NULL,
	updated_at  INTEGER NOT NULL,
	UNIQUE (tenant_id, channel)
);

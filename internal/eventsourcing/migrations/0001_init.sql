CREATE TABLE IF NOT EXISTS events (
    id              TEXT PRIMARY KEY COLLATE BINARY,
    type            TEXT NOT NULL,
    tenant_id       TEXT NOT NULL,
    occurred_at_ns  INTEGER NOT NULL,
    payload         BLOB NOT NULL,
    version         INTEGER NOT NULL,
    correlation_id  TEXT,
    causation_id    TEXT
) STRICT;

CREATE INDEX IF NOT EXISTS idx_events_tenant_time
    ON events (tenant_id, occurred_at_ns, id);

CREATE INDEX IF NOT EXISTS idx_events_tenant_type_time
    ON events (tenant_id, type, occurred_at_ns, id);

CREATE INDEX IF NOT EXISTS idx_events_correlation
    ON events (tenant_id, correlation_id)
    WHERE correlation_id IS NOT NULL;

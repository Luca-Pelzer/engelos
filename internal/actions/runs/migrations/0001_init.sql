CREATE TABLE IF NOT EXISTS runs (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    channel         TEXT NOT NULL,
    rule_name       TEXT NOT NULL,
    trigger_kind    TEXT NOT NULL,
    trigger_summary TEXT NOT NULL DEFAULT '',
    started_at      INTEGER NOT NULL,
    finished_at     INTEGER NOT NULL,
    status          TEXT NOT NULL,
    node_count      INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_runs_rule ON runs (tenant_id, channel, rule_name, id);
CREATE INDEX IF NOT EXISTS idx_runs_scope ON runs (tenant_id, channel, id);

CREATE TABLE IF NOT EXISTS run_nodes (
    run_id         TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    seq            INTEGER NOT NULL,
    node_kind      TEXT NOT NULL,
    type_id        TEXT NOT NULL,
    status         TEXT NOT NULL,
    duration_ms    INTEGER NOT NULL DEFAULT 0,
    error          TEXT NOT NULL DEFAULT '',
    output_summary TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (run_id, seq)
);

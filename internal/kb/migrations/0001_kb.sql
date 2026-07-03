-- engelOS kb package: per-channel streamer knowledge base with FTS5 search.
--
-- kb_entries is the base table (one row per entry). kb_fts is an
-- external-content FTS5 index over (title, content): it stores only the
-- inverted index and reads the text back from kb_entries via the implicit
-- rowid (content_rowid='rowid'). Three triggers keep the index in sync on
-- INSERT/UPDATE/DELETE, using the FTS5 'delete' command so re-indexes are
-- exact. bm25() ranking with a title weight boost powers Search.
--
-- Multi-tenant by default: self-hosted deployments use tenant_id = 'local'.
-- Timestamps are Unix nanoseconds (UTC). enabled is 0/1.

CREATE TABLE IF NOT EXISTS kb_entries (
    id          TEXT    PRIMARY KEY,
    tenant_id   TEXT    NOT NULL,
    channel     TEXT    NOT NULL,
    category    TEXT    NOT NULL,
    title       TEXT    NOT NULL,
    content     TEXT    NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_kb_channel
    ON kb_entries (tenant_id, channel);

CREATE VIRTUAL TABLE IF NOT EXISTS kb_fts USING fts5(
    title,
    content,
    content='kb_entries',
    content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS kb_entries_ai AFTER INSERT ON kb_entries BEGIN
    INSERT INTO kb_fts(rowid, title, content) VALUES (new.rowid, new.title, new.content);
END;

CREATE TRIGGER IF NOT EXISTS kb_entries_ad AFTER DELETE ON kb_entries BEGIN
    INSERT INTO kb_fts(kb_fts, rowid, title, content) VALUES ('delete', old.rowid, old.title, old.content);
END;

CREATE TRIGGER IF NOT EXISTS kb_entries_au AFTER UPDATE ON kb_entries BEGIN
    INSERT INTO kb_fts(kb_fts, rowid, title, content) VALUES ('delete', old.rowid, old.title, old.content);
    INSERT INTO kb_fts(rowid, title, content) VALUES (new.rowid, new.title, new.content);
END;

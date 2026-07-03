CREATE TABLE IF NOT EXISTS integration_credentials (
    tenant_id      TEXT NOT NULL,
    integration_id TEXT NOT NULL,
    cred_key       TEXT NOT NULL,
    ciphertext     BLOB NOT NULL,
    updated_at     INTEGER NOT NULL,
    PRIMARY KEY (tenant_id, integration_id, cred_key)
);

CREATE INDEX IF NOT EXISTS idx_intcreds_scope ON integration_credentials (tenant_id, integration_id);

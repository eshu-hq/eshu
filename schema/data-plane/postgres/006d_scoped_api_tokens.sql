CREATE TABLE IF NOT EXISTS scoped_api_tokens (
    token_hash TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    subject_id_hash TEXT NOT NULL,
    subject_class TEXT NOT NULL,
    status TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    last_used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS scoped_api_tokens_active_idx
    ON scoped_api_tokens (
        token_hash,
        tenant_id,
        workspace_id,
        subject_class,
        updated_at DESC
    )
    WHERE status = 'active' AND revoked_at IS NULL;

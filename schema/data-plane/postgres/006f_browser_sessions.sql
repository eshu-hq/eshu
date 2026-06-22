CREATE TABLE IF NOT EXISTS browser_sessions (
    session_hash TEXT PRIMARY KEY,
    csrf_token_hash TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    subject_id_hash TEXT NOT NULL,
    subject_class TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    all_scopes BOOLEAN NOT NULL DEFAULT false,
    allowed_scope_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_repository_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    issued_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    idle_expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS browser_sessions_active_idx
    ON browser_sessions (
        session_hash,
        tenant_id,
        workspace_id,
        updated_at DESC
    )
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS browser_sessions (
    session_hash TEXT PRIMARY KEY,
    csrf_token_hash TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    subject_id_hash TEXT NOT NULL,
    subject_class TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    role_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    all_scopes BOOLEAN NOT NULL DEFAULT false,
    allowed_scope_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_repository_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    external_provider_config_id TEXT NULL,
    external_subject_id_hash TEXT NULL,
    external_auth_validated_at TIMESTAMPTZ NULL,
    external_auth_stale_after TIMESTAMPTZ NULL,
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

ALTER TABLE browser_sessions
    ADD COLUMN IF NOT EXISTS role_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE browser_sessions
    ADD COLUMN IF NOT EXISTS external_provider_config_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS external_subject_id_hash TEXT NULL,
    ADD COLUMN IF NOT EXISTS external_auth_validated_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS external_auth_stale_after TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS browser_sessions_active_idx
    ON browser_sessions (
        session_hash,
        tenant_id,
        workspace_id,
        updated_at DESC
    )
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS browser_sessions_external_auth_stale_idx
    ON browser_sessions (external_auth_stale_after)
    WHERE revoked_at IS NULL
      AND external_auth_stale_after IS NOT NULL;

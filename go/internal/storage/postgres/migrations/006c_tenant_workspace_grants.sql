CREATE TABLE IF NOT EXISTS tenants (
    tenant_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    display_handle_hash TEXT NULL,
    policy_revision_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    tombstoned_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS tenants_active_idx
    ON tenants (tenant_id, updated_at DESC)
    WHERE status = 'active' AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS workspaces (
    tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    status TEXT NOT NULL,
    display_handle_hash TEXT NULL,
    policy_revision_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    PRIMARY KEY (tenant_id, workspace_id)
);

CREATE INDEX IF NOT EXISTS workspaces_active_idx
    ON workspaces (tenant_id, workspace_id, updated_at DESC)
    WHERE status = 'active' AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS tenant_scope_grants (
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    subject_class TEXT NOT NULL,
    grant_source TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, scope_id, subject_class),
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS tenant_scope_grants_active_idx
    ON tenant_scope_grants (
        tenant_id,
        workspace_id,
        subject_class,
        scope_id,
        effective_at DESC
    )
    WHERE tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS tenant_repository_grants (
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    subject_class TEXT NOT NULL,
    grant_source TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, repo_id, subject_class),
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS tenant_repository_grants_active_idx
    ON tenant_repository_grants (
        tenant_id,
        workspace_id,
        subject_class,
        repo_id,
        scope_id,
        effective_at DESC
    )
    WHERE tombstoned_at IS NULL;

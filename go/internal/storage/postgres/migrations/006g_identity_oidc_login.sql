CREATE TABLE IF NOT EXISTS identity_oidc_login_states (
    state_hash TEXT PRIMARY KEY,
    nonce_hash TEXT NOT NULL,
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    redirect_uri_hash TEXT NOT NULL,
    return_to_path TEXT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_oidc_login_states_active_idx
    ON identity_oidc_login_states (provider_config_id, expires_at DESC)
    WHERE consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_provider_group_role_mappings (
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    external_group_hash TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    status TEXT NOT NULL,
    mapping_source TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider_config_id, external_group_hash, tenant_id, workspace_id, role_id),
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES identity_roles(tenant_id, role_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_provider_group_role_mappings_active_idx
    ON identity_provider_group_role_mappings (
        provider_config_id,
        tenant_id,
        workspace_id,
        external_group_hash,
        role_id,
        effective_at DESC
    )
    WHERE status = 'active' AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_role_scope_targets (
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    grant_source TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, role_id, scope_id),
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES identity_roles(tenant_id, role_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_role_scope_targets_active_idx
    ON identity_role_scope_targets (tenant_id, workspace_id, role_id, scope_id, effective_at DESC)
    WHERE status = 'active' AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_role_repository_targets (
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    grant_source TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, role_id, repo_id),
    FOREIGN KEY (tenant_id, workspace_id, role_id, scope_id)
        REFERENCES identity_role_scope_targets(tenant_id, workspace_id, role_id, scope_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES identity_roles(tenant_id, role_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_role_repository_targets_active_idx
    ON identity_role_repository_targets (
        tenant_id,
        workspace_id,
        role_id,
        repo_id,
        scope_id,
        effective_at DESC
    )
    WHERE status = 'active' AND tombstoned_at IS NULL;

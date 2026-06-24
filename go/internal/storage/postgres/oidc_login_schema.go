// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const oidcLoginSchemaSQL = `
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
`

const createOIDCLoginStateQuery = `
WITH provider AS (
INSERT INTO identity_provider_configs (
    provider_config_id,
    tenant_id,
    provider_kind,
    provider_key_hash,
    status,
    issuer_hash,
    client_id_hash,
    created_at,
    updated_at
) VALUES ($3, $7, 'external_oidc', $4, 'active', $5, $6, $13, $13)
ON CONFLICT (provider_config_id) DO UPDATE
SET tenant_id = EXCLUDED.tenant_id,
    provider_kind = EXCLUDED.provider_kind,
    provider_key_hash = EXCLUDED.provider_key_hash,
    status = EXCLUDED.status,
    issuer_hash = EXCLUDED.issuer_hash,
    client_id_hash = EXCLUDED.client_id_hash,
    updated_at = EXCLUDED.updated_at
WHERE identity_provider_configs.tombstoned_at IS NULL
RETURNING provider_config_id
)
INSERT INTO identity_oidc_login_states (
    state_hash,
    nonce_hash,
    provider_config_id,
    tenant_id,
    workspace_id,
    redirect_uri_hash,
    return_to_path,
    issued_at,
    expires_at,
    consumed_at,
    created_at,
    updated_at
)
SELECT
    $1,
    $2,
    provider.provider_config_id,
    $7,
    $8,
    $9,
    NULLIF($10, ''),
    $11,
    $12,
    NULL,
    $13,
    $13
FROM provider
`

const consumeOIDCLoginStateQuery = `
UPDATE identity_oidc_login_states
SET consumed_at = $2,
    updated_at = $2
WHERE state_hash = $1
  AND consumed_at IS NULL
  AND expires_at > $2
RETURNING
    state_hash,
    nonce_hash,
    provider_config_id,
    tenant_id,
    workspace_id,
    redirect_uri_hash,
    COALESCE(return_to_path, ''),
    issued_at,
    expires_at,
    updated_at
`

const resolveOIDCGroupRolesQuery = `
SELECT DISTINCT
    mapping.role_id,
    mapping.policy_revision_hash
FROM identity_provider_group_role_mappings mapping
JOIN identity_roles role
    ON role.tenant_id = mapping.tenant_id
   AND role.role_id = mapping.role_id
JOIN tenants ten ON ten.tenant_id = mapping.tenant_id
JOIN workspaces ws
    ON ws.tenant_id = mapping.tenant_id
   AND ws.workspace_id = mapping.workspace_id
WHERE mapping.tenant_id = $1
  AND mapping.workspace_id = $2
  AND mapping.provider_config_id = $3
  AND mapping.effective_at <= $4
  AND mapping.external_group_hash = ANY($5::text[])
  AND (mapping.expires_at IS NULL OR mapping.expires_at > $4)
  AND mapping.status = 'active'
  AND mapping.tombstoned_at IS NULL
  AND role.status = 'active'
  AND role.tombstoned_at IS NULL
  AND ten.status = 'active'
  AND ws.status = 'active'
  AND ten.tombstoned_at IS NULL
  AND ws.tombstoned_at IS NULL
ORDER BY mapping.role_id ASC
LIMIT $6
`

const resolveOIDCActiveRolesQuery = `
SELECT DISTINCT
    role.role_id,
    target.policy_revision_hash
FROM identity_roles role
JOIN identity_role_scope_targets target
    ON target.tenant_id = role.tenant_id
   AND target.role_id = role.role_id
JOIN tenants ten ON ten.tenant_id = role.tenant_id
JOIN workspaces ws
    ON ws.tenant_id = target.tenant_id
   AND ws.workspace_id = target.workspace_id
JOIN identity_provider_configs provider
    ON provider.provider_config_id = $3
   AND provider.tenant_id = role.tenant_id
WHERE role.tenant_id = $1
  AND target.workspace_id = $2
  AND role.role_id = ANY($5::text[])
  AND target.effective_at <= $4
  AND (target.expires_at IS NULL OR target.expires_at > $4)
  AND target.status = 'active'
  AND target.tombstoned_at IS NULL
  AND role.status = 'active'
  AND role.tombstoned_at IS NULL
  AND provider.status = 'active'
  AND provider.tombstoned_at IS NULL
  AND ten.status = 'active'
  AND ws.status = 'active'
  AND ten.tombstoned_at IS NULL
  AND ws.tombstoned_at IS NULL
ORDER BY role.role_id ASC
LIMIT $6
`

const externalSubjectActiveQuery = `
SELECT TRUE
FROM identity_external_subjects ext
JOIN identity_users usr ON usr.user_id = ext.user_id
WHERE ext.provider_config_id = $1
  AND ext.external_subject_id_hash = $2
  AND ext.status = 'active'
  AND ext.disabled_at IS NULL
  AND ext.tombstoned_at IS NULL
  AND usr.status = 'active'
  AND usr.disabled_at IS NULL
  AND usr.tombstoned_at IS NULL
LIMIT 1
`

const resolveOIDCRoleScopeTargetsQuery = `
SELECT DISTINCT target.scope_id
FROM identity_role_scope_targets target
WHERE target.tenant_id = $1
  AND target.workspace_id = $2
  AND target.role_id = ANY($3::text[])
  AND target.status = 'active'
  AND target.tombstoned_at IS NULL
  AND target.effective_at <= $4
  AND (target.expires_at IS NULL OR target.expires_at > $4)
ORDER BY target.scope_id ASC
LIMIT $5
`

const resolveOIDCRoleRepositoryTargetsQuery = `
SELECT DISTINCT repo.repo_id, repo.scope_id
FROM identity_role_repository_targets repo
JOIN identity_role_scope_targets scope
    ON scope.tenant_id = repo.tenant_id
   AND scope.workspace_id = repo.workspace_id
   AND scope.role_id = repo.role_id
   AND scope.scope_id = repo.scope_id
WHERE repo.tenant_id = $1
  AND repo.workspace_id = $2
  AND repo.role_id = ANY($3::text[])
  AND repo.status = 'active'
  AND repo.tombstoned_at IS NULL
  AND repo.effective_at <= $4
  AND (repo.expires_at IS NULL OR repo.expires_at > $4)
  AND scope.status = 'active'
  AND scope.tombstoned_at IS NULL
  AND scope.effective_at <= $4
  AND (scope.expires_at IS NULL OR scope.expires_at > $4)
ORDER BY repo.repo_id ASC
LIMIT $5
`

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const tenantWorkspaceGrantSchemaSQL = `
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
`

const upsertTenantRecordQuery = `
INSERT INTO tenants (
    tenant_id,
    status,
    display_handle_hash,
    policy_revision_hash,
    created_at,
    updated_at,
    tombstoned_at
) VALUES ($1, $2, NULLIF($3, ''), $4, $5, $5, $6)
ON CONFLICT (tenant_id) DO UPDATE
SET status = EXCLUDED.status,
    display_handle_hash = EXCLUDED.display_handle_hash,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    updated_at = EXCLUDED.updated_at,
    tombstoned_at = EXCLUDED.tombstoned_at
`

const upsertWorkspaceRecordQuery = `
INSERT INTO workspaces (
    tenant_id,
    workspace_id,
    status,
    display_handle_hash,
    policy_revision_hash,
    created_at,
    updated_at,
    tombstoned_at
) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $6, $7)
ON CONFLICT (tenant_id, workspace_id) DO UPDATE
SET status = EXCLUDED.status,
    display_handle_hash = EXCLUDED.display_handle_hash,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    updated_at = EXCLUDED.updated_at,
    tombstoned_at = EXCLUDED.tombstoned_at
`

const upsertTenantScopeGrantQuery = `
INSERT INTO tenant_scope_grants (
    tenant_id,
    workspace_id,
    scope_id,
    subject_class,
    grant_source,
    policy_revision_hash,
    effective_at,
    expires_at,
    tombstoned_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
ON CONFLICT (tenant_id, workspace_id, scope_id, subject_class) DO UPDATE
SET grant_source = EXCLUDED.grant_source,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    effective_at = EXCLUDED.effective_at,
    expires_at = EXCLUDED.expires_at,
    tombstoned_at = EXCLUDED.tombstoned_at,
    updated_at = EXCLUDED.updated_at
`

const upsertTenantRepositoryGrantQuery = `
INSERT INTO tenant_repository_grants (
    tenant_id,
    workspace_id,
    repo_id,
    scope_id,
    subject_class,
    grant_source,
    policy_revision_hash,
    effective_at,
    expires_at,
    tombstoned_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
ON CONFLICT (tenant_id, workspace_id, repo_id, subject_class) DO UPDATE
SET scope_id = EXCLUDED.scope_id,
    grant_source = EXCLUDED.grant_source,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    effective_at = EXCLUDED.effective_at,
    expires_at = EXCLUDED.expires_at,
    tombstoned_at = EXCLUDED.tombstoned_at,
    updated_at = EXCLUDED.updated_at
`

const listTenantScopeGrantsQuery = `
SELECT
    g.tenant_id,
    g.workspace_id,
    g.scope_id,
    g.subject_class,
    g.grant_source,
    g.policy_revision_hash,
    g.effective_at,
    g.expires_at
FROM tenant_scope_grants g
JOIN tenants t ON t.tenant_id = g.tenant_id
JOIN workspaces w ON w.tenant_id = g.tenant_id AND w.workspace_id = g.workspace_id
WHERE g.tenant_id = $1
  AND g.workspace_id = $2
  AND g.subject_class = $3
  AND t.status = 'active'
  AND w.status = 'active'
  AND t.tombstoned_at IS NULL
  AND w.tombstoned_at IS NULL
  AND g.tombstoned_at IS NULL
  AND g.effective_at <= $4
  AND (g.expires_at IS NULL OR g.expires_at > $4)
  AND (COALESCE(cardinality($5::text[]), 0) = 0 OR g.scope_id = ANY($5::text[]))
ORDER BY g.scope_id ASC
LIMIT $6
`

const listTenantRepositoryGrantsQuery = `
SELECT
    g.tenant_id,
    g.workspace_id,
    g.repo_id,
    g.scope_id,
    g.subject_class,
    g.grant_source,
    g.policy_revision_hash,
    g.effective_at,
    g.expires_at
FROM tenant_repository_grants g
JOIN tenants t ON t.tenant_id = g.tenant_id
JOIN workspaces w ON w.tenant_id = g.tenant_id AND w.workspace_id = g.workspace_id
JOIN tenant_scope_grants sg
    ON sg.tenant_id = g.tenant_id
   AND sg.workspace_id = g.workspace_id
   AND sg.scope_id = g.scope_id
   AND sg.subject_class = g.subject_class
WHERE g.tenant_id = $1
  AND g.workspace_id = $2
  AND g.subject_class = $3
  AND t.status = 'active'
  AND w.status = 'active'
  AND t.tombstoned_at IS NULL
  AND w.tombstoned_at IS NULL
  AND g.tombstoned_at IS NULL
  AND sg.tombstoned_at IS NULL
  AND g.effective_at <= $4
  AND sg.effective_at <= $4
  AND (g.expires_at IS NULL OR g.expires_at > $4)
  AND (sg.expires_at IS NULL OR sg.expires_at > $4)
  AND (COALESCE(cardinality($5::text[]), 0) = 0 OR g.scope_id = ANY($5::text[]))
ORDER BY g.repo_id ASC
LIMIT $6
`

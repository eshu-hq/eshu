package postgres

const browserSessionSchemaSQL = `
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
    permission_catalog_enforced BOOLEAN NOT NULL DEFAULT false,
    allowed_scope_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_repository_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_permission_features JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_permission_data_classes JSONB NOT NULL DEFAULT '[]'::jsonb,
    external_provider_config_id TEXT NULL,
    external_subject_id_hash TEXT NULL,
    external_group_hashes JSONB NOT NULL DEFAULT '[]'::jsonb,
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

ALTER TABLE browser_sessions
    ADD COLUMN IF NOT EXISTS external_group_hashes JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE browser_sessions
    ADD COLUMN IF NOT EXISTS permission_catalog_enforced BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS allowed_permission_features JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS allowed_permission_data_classes JSONB NOT NULL DEFAULT '[]'::jsonb;

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
`

const createBrowserSessionQuery = `
INSERT INTO browser_sessions (
    session_hash,
    csrf_token_hash,
    tenant_id,
    workspace_id,
    subject_id_hash,
    subject_class,
    policy_revision_hash,
    role_ids,
    all_scopes,
    permission_catalog_enforced,
    allowed_scope_ids,
    allowed_repository_ids,
    allowed_permission_features,
    allowed_permission_data_classes,
    external_provider_config_id,
    external_subject_id_hash,
    external_auth_validated_at,
    external_auth_stale_after,
    external_group_hashes,
    issued_at,
    last_seen_at,
    idle_expires_at,
    absolute_expires_at,
    revoked_at,
    created_at,
    updated_at
)
SELECT
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    COALESCE(NULLIF($7, ''), ws.policy_revision_hash),
    $8::jsonb,
    $9,
    $23,
    $10::jsonb,
    $11::jsonb,
    $24::jsonb,
    $25::jsonb,
    $12,
    $13,
    $14,
    $15,
    $22::jsonb,
    $16,
    $17,
    $18,
    $19,
    $20,
    $21,
    $21
FROM workspaces ws
JOIN tenants ten ON ten.tenant_id = ws.tenant_id
WHERE ws.tenant_id = $3
  AND ws.workspace_id = $4
  AND ws.status = 'active'
  AND ten.status = 'active'
  AND ws.tombstoned_at IS NULL
  AND ten.tombstoned_at IS NULL
ON CONFLICT (session_hash) DO UPDATE
SET csrf_token_hash = EXCLUDED.csrf_token_hash,
    tenant_id = EXCLUDED.tenant_id,
    workspace_id = EXCLUDED.workspace_id,
    subject_id_hash = EXCLUDED.subject_id_hash,
    subject_class = EXCLUDED.subject_class,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    role_ids = EXCLUDED.role_ids,
    all_scopes = EXCLUDED.all_scopes,
    permission_catalog_enforced = EXCLUDED.permission_catalog_enforced,
    allowed_scope_ids = EXCLUDED.allowed_scope_ids,
    allowed_repository_ids = EXCLUDED.allowed_repository_ids,
    allowed_permission_features = EXCLUDED.allowed_permission_features,
    allowed_permission_data_classes = EXCLUDED.allowed_permission_data_classes,
    external_provider_config_id = EXCLUDED.external_provider_config_id,
    external_subject_id_hash = EXCLUDED.external_subject_id_hash,
    external_auth_validated_at = EXCLUDED.external_auth_validated_at,
    external_auth_stale_after = EXCLUDED.external_auth_stale_after,
    external_group_hashes = EXCLUDED.external_group_hashes,
    issued_at = EXCLUDED.issued_at,
    last_seen_at = EXCLUDED.last_seen_at,
    idle_expires_at = EXCLUDED.idle_expires_at,
    absolute_expires_at = EXCLUDED.absolute_expires_at,
    revoked_at = EXCLUDED.revoked_at,
    updated_at = EXCLUDED.updated_at
`

const listStaleOIDCBrowserSessionsQuery = `
SELECT
    session_hash,
    external_provider_config_id,
    external_subject_id_hash,
    tenant_id,
    workspace_id,
    policy_revision_hash,
    role_ids,
    all_scopes,
    allowed_scope_ids,
    allowed_repository_ids,
    external_auth_validated_at,
    external_auth_stale_after,
    external_group_hashes
FROM browser_sessions
WHERE revoked_at IS NULL
  AND subject_class = 'external_oidc_user'
  AND external_provider_config_id IS NOT NULL
  AND external_subject_id_hash IS NOT NULL
  AND external_auth_stale_after IS NOT NULL
  AND external_auth_stale_after <= $1
ORDER BY external_auth_stale_after ASC
LIMIT $2
`

const updateOIDCBrowserSessionAuthProofQuery = `
UPDATE browser_sessions
SET external_auth_validated_at = $2,
    external_auth_stale_after = $3,
    policy_revision_hash = $4,
    role_ids = $5::jsonb,
    all_scopes = $6,
    allowed_scope_ids = $7::jsonb,
    allowed_repository_ids = $8::jsonb,
    updated_at = $9,
    external_group_hashes = $10::jsonb
WHERE session_hash = $1
  AND revoked_at IS NULL
  AND subject_class = 'external_oidc_user'
`

const revokeStaleOIDCBrowserSessionQuery = `
UPDATE browser_sessions
SET revoked_at = $2,
    updated_at = $2
WHERE session_hash = $1
  AND revoked_at IS NULL
  AND subject_class = 'external_oidc_user'
  AND (
    external_auth_validated_at IS NULL
    OR external_auth_stale_after IS NULL
    OR external_auth_stale_after <= $2
  )
`

const resolveBrowserSessionQuery = `
WITH active AS (
SELECT
    sess.session_hash,
    sess.csrf_token_hash,
    sess.tenant_id,
    sess.workspace_id,
    sess.subject_id_hash,
    sess.subject_class,
    sess.policy_revision_hash,
    sess.role_ids,
    sess.all_scopes,
    sess.permission_catalog_enforced,
    sess.allowed_scope_ids,
    sess.allowed_repository_ids,
    sess.allowed_permission_features,
    sess.allowed_permission_data_classes,
    sess.issued_at,
    sess.last_seen_at,
    sess.idle_expires_at,
    sess.absolute_expires_at,
    sess.revoked_at,
    (NOT $3 OR sess.csrf_token_hash = $2) AS csrf_ok
FROM browser_sessions sess
JOIN tenants ten ON ten.tenant_id = sess.tenant_id
JOIN workspaces ws ON ws.tenant_id = sess.tenant_id AND ws.workspace_id = sess.workspace_id
WHERE sess.session_hash = $1
  AND sess.revoked_at IS NULL
  AND sess.idle_expires_at > $4
  AND sess.absolute_expires_at > $4
  AND (
    sess.subject_class <> 'external_oidc_user'
    OR (
      sess.external_auth_validated_at IS NOT NULL
      AND sess.external_auth_stale_after IS NOT NULL
      AND sess.external_auth_stale_after > $4
    )
  )
  AND sess.policy_revision_hash = ws.policy_revision_hash
  AND ten.status = 'active'
  AND ws.status = 'active'
  AND ten.tombstoned_at IS NULL
  AND ws.tombstoned_at IS NULL
LIMIT 1
),
refreshed AS (
UPDATE browser_sessions sess
SET last_seen_at = $4,
    idle_expires_at = LEAST(sess.absolute_expires_at, $5),
    updated_at = $4
FROM active
WHERE sess.session_hash = active.session_hash
  AND active.csrf_ok
  AND sess.revoked_at IS NULL
  AND sess.idle_expires_at > $4
  AND sess.absolute_expires_at > $4
  AND (
    sess.subject_class <> 'external_oidc_user'
    OR (
      sess.external_auth_validated_at IS NOT NULL
      AND sess.external_auth_stale_after IS NOT NULL
      AND sess.external_auth_stale_after > $4
    )
  )
RETURNING
    sess.session_hash,
    sess.csrf_token_hash,
    sess.tenant_id,
    sess.workspace_id,
    sess.subject_id_hash,
    sess.subject_class,
    sess.policy_revision_hash,
    sess.role_ids,
    sess.all_scopes,
    sess.permission_catalog_enforced,
    sess.allowed_scope_ids,
    sess.allowed_repository_ids,
    sess.allowed_permission_features,
    sess.allowed_permission_data_classes,
    sess.issued_at,
    sess.last_seen_at,
    sess.idle_expires_at,
    sess.absolute_expires_at,
    sess.revoked_at,
    true AS csrf_ok
)
SELECT * FROM refreshed
UNION ALL
SELECT
    active.session_hash,
    active.csrf_token_hash,
    active.tenant_id,
    active.workspace_id,
    active.subject_id_hash,
    active.subject_class,
    active.policy_revision_hash,
    active.role_ids,
    active.all_scopes,
    active.permission_catalog_enforced,
    active.allowed_scope_ids,
    active.allowed_repository_ids,
    active.allowed_permission_features,
    active.allowed_permission_data_classes,
    active.issued_at,
    active.last_seen_at,
    active.idle_expires_at,
    active.absolute_expires_at,
    active.revoked_at,
    active.csrf_ok
FROM active
WHERE NOT active.csrf_ok
LIMIT 1
`

const revokeBrowserSessionQuery = `
UPDATE browser_sessions
SET revoked_at = $2,
    updated_at = $2
WHERE session_hash = $1
  AND revoked_at IS NULL
`

const switchBrowserSessionWorkspaceQuery = `
UPDATE browser_sessions sess
SET tenant_id = ws.tenant_id,
    workspace_id = ws.workspace_id,
    policy_revision_hash = ws.policy_revision_hash,
    last_seen_at = $4,
    updated_at = $4
FROM workspaces ws
JOIN tenants ten ON ten.tenant_id = ws.tenant_id
WHERE sess.session_hash = $1
  AND ws.tenant_id = $2
  AND ws.workspace_id = $3
  AND ws.status = 'active'
  AND ten.status = 'active'
  AND ws.tombstoned_at IS NULL
  AND ten.tombstoned_at IS NULL
  AND sess.revoked_at IS NULL
  AND sess.all_scopes = true
  AND sess.idle_expires_at > $4
  AND sess.absolute_expires_at > $4
RETURNING
    sess.session_hash,
    sess.csrf_token_hash,
    sess.tenant_id,
    sess.workspace_id,
    sess.subject_id_hash,
    sess.subject_class,
    sess.policy_revision_hash,
    sess.role_ids,
    sess.all_scopes,
    sess.permission_catalog_enforced,
    sess.allowed_scope_ids,
    sess.allowed_repository_ids,
    sess.allowed_permission_features,
    sess.allowed_permission_data_classes,
    sess.issued_at,
    sess.last_seen_at,
    sess.idle_expires_at,
    sess.absolute_expires_at,
    sess.revoked_at,
    true AS csrf_ok
`

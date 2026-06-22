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

ALTER TABLE browser_sessions
    ADD COLUMN IF NOT EXISTS role_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS browser_sessions_active_idx
    ON browser_sessions (
        session_hash,
        tenant_id,
        workspace_id,
        updated_at DESC
    )
    WHERE revoked_at IS NULL;
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
    allowed_scope_ids,
    allowed_repository_ids,
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
    $10::jsonb,
    $11::jsonb,
    $12,
    $13,
    $14,
    $15,
    $16,
    $17,
    $17
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
    allowed_scope_ids = EXCLUDED.allowed_scope_ids,
    allowed_repository_ids = EXCLUDED.allowed_repository_ids,
    issued_at = EXCLUDED.issued_at,
    last_seen_at = EXCLUDED.last_seen_at,
    idle_expires_at = EXCLUDED.idle_expires_at,
    absolute_expires_at = EXCLUDED.absolute_expires_at,
    revoked_at = EXCLUDED.revoked_at,
    updated_at = EXCLUDED.updated_at
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
    sess.allowed_scope_ids,
    sess.allowed_repository_ids,
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
    sess.allowed_scope_ids,
    sess.allowed_repository_ids,
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
    active.allowed_scope_ids,
    active.allowed_repository_ids,
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
    sess.allowed_scope_ids,
    sess.allowed_repository_ids,
    sess.issued_at,
    sess.last_seen_at,
    sess.idle_expires_at,
    sess.absolute_expires_at,
    sess.revoked_at,
    true AS csrf_ok
`

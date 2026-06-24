// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const scopedAPITokenSchemaSQL = `
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
`

const upsertScopedAPITokenQuery = `
INSERT INTO scoped_api_tokens (
    token_hash,
    tenant_id,
    workspace_id,
    subject_id_hash,
    subject_class,
    status,
    policy_revision_hash,
    issued_at,
    expires_at,
    revoked_at,
    last_used_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
ON CONFLICT (token_hash) DO UPDATE
SET tenant_id = EXCLUDED.tenant_id,
    workspace_id = EXCLUDED.workspace_id,
    subject_id_hash = EXCLUDED.subject_id_hash,
    subject_class = EXCLUDED.subject_class,
    status = EXCLUDED.status,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    issued_at = EXCLUDED.issued_at,
    expires_at = EXCLUDED.expires_at,
    revoked_at = EXCLUDED.revoked_at,
    last_used_at = EXCLUDED.last_used_at,
    updated_at = EXCLUDED.updated_at
`

const resolveScopedAPITokenQuery = `
SELECT
    tok.token_hash,
    tok.tenant_id,
    tok.workspace_id,
    tok.subject_id_hash,
    tok.subject_class,
    tok.status,
    tok.policy_revision_hash,
    tok.issued_at,
    tok.expires_at,
    tok.revoked_at,
    tok.last_used_at
FROM scoped_api_tokens tok
JOIN tenants ten ON ten.tenant_id = tok.tenant_id
JOIN workspaces ws ON ws.tenant_id = tok.tenant_id AND ws.workspace_id = tok.workspace_id
WHERE tok.token_hash = $1
  AND tok.status = 'active'
  AND ten.status = 'active'
  AND ws.status = 'active'
  AND ten.tombstoned_at IS NULL
  AND ws.tombstoned_at IS NULL
  AND tok.revoked_at IS NULL
  AND (tok.expires_at IS NULL OR tok.expires_at > $2)
LIMIT 1
`

const markScopedAPITokenUsedQuery = `
UPDATE scoped_api_tokens
SET last_used_at = $2,
    updated_at = $2
WHERE token_hash = $1
`

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const githubLoginSchemaSQL = `
CREATE TABLE IF NOT EXISTS identity_github_login_states (
    state_hash TEXT PRIMARY KEY,
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

CREATE INDEX IF NOT EXISTS identity_github_login_states_active_idx
    ON identity_github_login_states (provider_config_id, expires_at DESC)
    WHERE consumed_at IS NULL;
`

const createGitHubLoginStateQuery = `
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
) VALUES ($2, $6, 'external_github', $3, 'active', $4, $5, $10, $10)
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
INSERT INTO identity_github_login_states (
    state_hash,
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
    provider.provider_config_id,
    $6,
    $7,
    $8,
    NULLIF($9, ''),
    $10,
    $11,
    NULL,
    $10,
    $10
FROM provider
`

const consumeGitHubLoginStateQuery = `
UPDATE identity_github_login_states
SET consumed_at = $2,
    updated_at = $2
WHERE state_hash = $1
  AND consumed_at IS NULL
  AND expires_at > $2
RETURNING
    state_hash,
    provider_config_id,
    tenant_id,
    workspace_id,
    redirect_uri_hash,
    COALESCE(return_to_path, ''),
    issued_at,
    expires_at,
    updated_at
`

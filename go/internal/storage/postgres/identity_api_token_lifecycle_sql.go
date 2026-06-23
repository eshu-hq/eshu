package postgres

const insertLocalIdentityPersonalAPITokenQuery = `
INSERT INTO identity_token_metadata (
    token_id,
    token_hash,
    token_class,
    tenant_id,
    workspace_id,
    user_id,
    service_principal_id,
    status,
    display_handle_hash,
    policy_revision_hash,
    issued_at,
    expires_at,
    revoked_at,
    last_used_at,
    created_at,
    updated_at
)
SELECT
    $1, $2, 'personal', $3, $4, user_subject.user_id, NULL,
    'active', NULLIF($6, ''), $7, $8, $9,
    NULL, NULL, $8, $8
FROM identity_users user_subject
JOIN identity_tenant_memberships membership
  ON membership.user_id = user_subject.user_id
 AND membership.tenant_id = $3
 AND membership.workspace_id = $4
WHERE user_subject.user_id = $5
  AND user_subject.status = 'active'
  AND user_subject.disabled_at IS NULL
  AND user_subject.tombstoned_at IS NULL
  AND membership.status = 'active'
  AND membership.disabled_at IS NULL
  AND membership.tombstoned_at IS NULL
  AND membership.effective_at <= $8
  AND (membership.expires_at IS NULL OR membership.expires_at > $8)
`

const insertLocalIdentityServicePrincipalAPITokenQuery = `
INSERT INTO identity_token_metadata (
    token_id,
    token_hash,
    token_class,
    tenant_id,
    workspace_id,
    user_id,
    service_principal_id,
    status,
    display_handle_hash,
    policy_revision_hash,
    issued_at,
    expires_at,
    revoked_at,
    last_used_at,
    created_at,
    updated_at
)
SELECT
    $1, $2, 'service_principal', $3, $4, NULL, service_principal.service_principal_id,
    'active', NULLIF($6, ''), $7, $8, $9,
    NULL, NULL, $8, $8
FROM identity_service_principals service_principal
WHERE service_principal.service_principal_id = $5
  AND service_principal.tenant_id = $3
  AND service_principal.workspace_id = $4
  AND service_principal.owner_user_id IS NOT NULL
  AND service_principal.status = 'active'
  AND service_principal.disabled_at IS NULL
  AND service_principal.tombstoned_at IS NULL
`

const revokeLocalIdentityAPITokenQuery = `
UPDATE identity_token_metadata
SET status = 'revoked',
    revoked_at = $4,
    updated_at = $4
WHERE token_id = $1
  AND tenant_id = $2
  AND workspace_id = $3
  AND status = 'active'
  AND revoked_at IS NULL
`

const rotateLocalIdentityAPITokenQuery = `
INSERT INTO identity_token_metadata (
    token_id,
    token_hash,
    token_class,
    tenant_id,
    workspace_id,
    user_id,
    service_principal_id,
    status,
    display_handle_hash,
    policy_revision_hash,
    issued_at,
    expires_at,
    revoked_at,
    last_used_at,
    created_at,
    updated_at
)
SELECT
    $1,
    $2,
    old_token.token_class,
    old_token.tenant_id,
    old_token.workspace_id,
    old_token.user_id,
    old_token.service_principal_id,
    'active',
    old_token.display_handle_hash,
    old_token.policy_revision_hash,
    $6,
    $7,
    NULL,
    NULL,
    $6,
    $6
FROM identity_token_metadata old_token
WHERE old_token.token_id = $3
  AND old_token.tenant_id = $4
  AND old_token.workspace_id = $5
  AND old_token.status = 'active'
  AND old_token.revoked_at IS NULL
`

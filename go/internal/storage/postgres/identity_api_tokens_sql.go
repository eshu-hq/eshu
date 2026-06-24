// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const resolveIdentityAPITokenSubjectQuery = `
SELECT
    tok.token_hash,
    tok.token_class,
    tok.tenant_id,
    tok.workspace_id,
    COALESCE(user_subject.subject_id_hash, ''),
    COALESCE(service_principal.service_principal_id, ''),
    tok.policy_revision_hash
FROM identity_token_metadata tok
JOIN tenants ten ON ten.tenant_id = tok.tenant_id
JOIN workspaces ws
    ON ws.tenant_id = tok.tenant_id
   AND ws.workspace_id = tok.workspace_id
LEFT JOIN identity_users user_subject
    ON tok.token_class = 'personal'
   AND user_subject.user_id = tok.user_id
   AND user_subject.status = 'active'
   AND user_subject.disabled_at IS NULL
   AND user_subject.tombstoned_at IS NULL
LEFT JOIN identity_service_principals service_principal
    ON tok.token_class = 'service_principal'
   AND service_principal.service_principal_id = tok.service_principal_id
   AND service_principal.tenant_id = tok.tenant_id
   AND service_principal.workspace_id = tok.workspace_id
   AND service_principal.status = 'active'
   AND service_principal.disabled_at IS NULL
   AND service_principal.tombstoned_at IS NULL
WHERE tok.token_hash = $1
  AND tok.status = 'active'
  AND tok.revoked_at IS NULL
  AND (tok.expires_at IS NULL OR tok.expires_at > $2)
  AND ten.status = 'active'
  AND ws.status = 'active'
  AND ten.tombstoned_at IS NULL
  AND ws.tombstoned_at IS NULL
  AND (
      (tok.token_class = 'personal' AND user_subject.user_id IS NOT NULL)
      OR (tok.token_class = 'service_principal' AND service_principal.service_principal_id IS NOT NULL)
  )
LIMIT 1
`

const resolveIdentityPersonalAPITokenRolesQuery = `
SELECT DISTINCT
    role_assignment.role_id,
    role_assignment.policy_revision_hash
FROM identity_token_metadata tok
JOIN identity_users user_subject
    ON user_subject.user_id = tok.user_id
JOIN identity_tenant_memberships membership
    ON membership.tenant_id = tok.tenant_id
   AND membership.workspace_id = tok.workspace_id
   AND membership.user_id = tok.user_id
JOIN identity_membership_roles role_assignment
    ON role_assignment.tenant_id = tok.tenant_id
   AND role_assignment.workspace_id = tok.workspace_id
   AND role_assignment.user_id = tok.user_id
JOIN identity_roles role
    ON role.tenant_id = tok.tenant_id
   AND role.role_id = role_assignment.role_id
WHERE tok.token_hash = $1
  AND tok.token_class = 'personal'
  AND tok.status = 'active'
  AND tok.revoked_at IS NULL
  AND (tok.expires_at IS NULL OR tok.expires_at > $2)
  AND user_subject.status = 'active'
  AND user_subject.disabled_at IS NULL
  AND user_subject.tombstoned_at IS NULL
  AND membership.status = 'active'
  AND membership.disabled_at IS NULL
  AND membership.tombstoned_at IS NULL
  AND membership.effective_at <= $2
  AND (membership.expires_at IS NULL OR membership.expires_at > $2)
  AND role_assignment.status = 'active'
  AND role_assignment.tombstoned_at IS NULL
  AND role_assignment.effective_at <= $2
  AND (role_assignment.expires_at IS NULL OR role_assignment.expires_at > $2)
  AND role.status = 'active'
  AND role.tombstoned_at IS NULL
ORDER BY role_assignment.role_id ASC
LIMIT $3
`

const resolveIdentityServicePrincipalAPITokenRolesQuery = `
SELECT DISTINCT
    role_assignment.role_id,
    role_assignment.policy_revision_hash
FROM identity_token_metadata tok
JOIN identity_service_principals service_principal
    ON service_principal.service_principal_id = tok.service_principal_id
   AND service_principal.tenant_id = tok.tenant_id
   AND service_principal.workspace_id = tok.workspace_id
JOIN identity_service_principal_roles role_assignment
    ON role_assignment.tenant_id = tok.tenant_id
   AND role_assignment.workspace_id = tok.workspace_id
   AND role_assignment.service_principal_id = tok.service_principal_id
JOIN identity_roles role
    ON role.tenant_id = tok.tenant_id
   AND role.role_id = role_assignment.role_id
WHERE tok.token_hash = $1
  AND tok.token_class = 'service_principal'
  AND tok.status = 'active'
  AND tok.revoked_at IS NULL
  AND (tok.expires_at IS NULL OR tok.expires_at > $2)
  AND service_principal.status = 'active'
  AND service_principal.disabled_at IS NULL
  AND service_principal.tombstoned_at IS NULL
  AND role_assignment.status = 'active'
  AND role_assignment.tombstoned_at IS NULL
  AND role_assignment.effective_at <= $2
  AND (role_assignment.expires_at IS NULL OR role_assignment.expires_at > $2)
  AND role.status = 'active'
  AND role.tombstoned_at IS NULL
ORDER BY role_assignment.role_id ASC
LIMIT $3
`

const resolveIdentityAPITokenPermissionsQuery = `
SELECT DISTINCT
    grant.feature,
    grant.data_class
FROM identity_role_grants grant
JOIN identity_roles role
    ON role.tenant_id = grant.tenant_id
   AND role.role_id = grant.role_id
WHERE grant.tenant_id = $1
  AND grant.role_id = ANY($2::text[])
  AND grant.status = 'active'
  AND grant.tombstoned_at IS NULL
  AND grant.effective_at <= $3
  AND (grant.expires_at IS NULL OR grant.expires_at > $3)
  AND role.status = 'active'
  AND role.tombstoned_at IS NULL
ORDER BY grant.feature ASC, grant.data_class ASC
LIMIT $4
`

const markIdentityAPITokenUsedQuery = `
UPDATE identity_token_metadata
SET last_used_at = $2,
    updated_at = $2
WHERE token_hash = $1
  AND status = 'active'
  AND revoked_at IS NULL
`

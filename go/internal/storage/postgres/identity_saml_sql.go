// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// resolveSAMLExternalSubjectQuery resolves any active SAML subject through
// durable identity, membership, and role state. It returns one row per
// resolved subject with a has_admin_role boolean derived from whether the
// subject holds an owner or tenant_admin membership role. Resolution does not
// require an active grant row: a user with an active role but no grants still
// resolves (non-admin path; permission catalog enforces them). All existing
// safety predicates (active/not-disabled/not-tombstoned, effective_at/expires_at
// windows, group_claims_hash match) are preserved.
const resolveSAMLExternalSubjectQuery = `
SELECT
    m.tenant_id,
    m.workspace_id,
    u.subject_id_hash,
    m.policy_revision_hash,
    u.user_id,
    BOOL_OR(mr.role_id IN ('owner', 'tenant_admin')) AS has_admin_role
FROM identity_external_subjects es
JOIN identity_provider_configs pc
    ON pc.provider_config_id = es.provider_config_id
JOIN identity_users u
    ON u.user_id = es.user_id
JOIN identity_tenant_memberships m
    ON m.user_id = u.user_id
   AND m.tenant_id = pc.tenant_id
JOIN identity_membership_roles mr
    ON mr.tenant_id = m.tenant_id
   AND mr.workspace_id = m.workspace_id
   AND mr.user_id = m.user_id
JOIN tenants t
    ON t.tenant_id = m.tenant_id
JOIN workspaces w
    ON w.tenant_id = m.tenant_id
   AND w.workspace_id = m.workspace_id
WHERE es.provider_config_id = $1
  AND es.external_subject_id_hash = $2
  AND es.group_claims_hash = $3
  AND es.status = 'active'
  AND es.disabled_at IS NULL
  AND es.tombstoned_at IS NULL
  AND pc.provider_kind = 'external_saml'
  AND pc.status = 'active'
  AND pc.tombstoned_at IS NULL
  AND u.status = 'active'
  AND u.disabled_at IS NULL
  AND u.tombstoned_at IS NULL
  AND t.status = 'active'
  AND t.tombstoned_at IS NULL
  AND w.status = 'active'
  AND w.tombstoned_at IS NULL
  AND m.status = 'active'
  AND m.disabled_at IS NULL
  AND m.tombstoned_at IS NULL
  AND m.effective_at <= $4
  AND (m.expires_at IS NULL OR m.expires_at > $4)
  AND mr.status = 'active'
  AND mr.tombstoned_at IS NULL
  AND mr.effective_at <= $4
  AND (mr.expires_at IS NULL OR mr.expires_at > $4)
GROUP BY m.tenant_id, m.workspace_id, u.subject_id_hash, m.policy_revision_hash, u.user_id
ORDER BY MAX(m.effective_at) DESC, m.workspace_id ASC
LIMIT 1
`

const selectKnownSAMLExternalSubjectQuery = `
SELECT es.external_identity_id
FROM identity_external_subjects es
JOIN identity_provider_configs pc
    ON pc.provider_config_id = es.provider_config_id
JOIN identity_users u
    ON u.user_id = es.user_id
WHERE es.provider_config_id = $1
  AND es.external_subject_id_hash = $2
  AND es.status = 'active'
  AND es.disabled_at IS NULL
  AND es.tombstoned_at IS NULL
  AND pc.provider_kind = 'external_saml'
  AND pc.status = 'active'
  AND pc.tombstoned_at IS NULL
  AND u.status = 'active'
  AND u.disabled_at IS NULL
  AND u.tombstoned_at IS NULL
LIMIT 1
`

const selectActiveSAMLProviderConfigQuery = `
SELECT pc.provider_config_id
FROM identity_provider_configs pc
JOIN tenants t
    ON t.tenant_id = pc.tenant_id
WHERE pc.provider_config_id = $1
  AND pc.provider_kind = 'external_saml'
  AND pc.status = 'active'
  AND pc.tombstoned_at IS NULL
  AND t.status = 'active'
  AND t.tombstoned_at IS NULL
LIMIT 1
`

// selectActiveSAMLProviderConfigForTenantQuery is the tenant-scoped variant of
// selectActiveSAMLProviderConfigQuery. It adds pc.tenant_id = $2 so the check
// is confined to a single tenant and cannot leak cross-tenant SAML activity.
// Used only by the pre-auth provider-discovery endpoint.
const selectActiveSAMLProviderConfigForTenantQuery = `
SELECT pc.provider_config_id
FROM identity_provider_configs pc
JOIN tenants t
    ON t.tenant_id = pc.tenant_id
WHERE pc.provider_config_id = $1
  AND pc.tenant_id = $2
  AND pc.provider_kind = 'external_saml'
  AND pc.status = 'active'
  AND pc.tombstoned_at IS NULL
  AND t.status = 'active'
  AND t.tombstoned_at IS NULL
LIMIT 1
`

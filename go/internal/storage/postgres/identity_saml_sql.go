package postgres

// resolveSAMLExternalSubjectQuery resolves any active SAML subject through
// durable identity, membership, role, and grant state. It returns one row per
// resolved subject with a has_all_scope_role boolean derived from whether any
// of the subject's active role grants carry scope_class='all'. All existing
// safety predicates (active/not-disabled/not-tombstoned, effective_at/expires_at
// windows, group_claims_hash match) are preserved. The role-name allowlist and
// scope_class='all' hard-filter are removed from the resolution gate: any active
// mapped role now resolves the subject, and the all-scope flag is computed from
// the grant set rather than hard-coded.
const resolveSAMLExternalSubjectQuery = `
SELECT
    m.tenant_id,
    m.workspace_id,
    u.subject_id_hash,
    m.policy_revision_hash,
    u.user_id,
    BOOL_OR(rg.scope_class = 'all') AS has_all_scope_role
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
JOIN identity_roles r
    ON r.tenant_id = mr.tenant_id
   AND r.role_id = mr.role_id
JOIN identity_role_grants rg
    ON rg.tenant_id = r.tenant_id
   AND rg.role_id = r.role_id
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
  AND r.status = 'active'
  AND r.tombstoned_at IS NULL
  AND rg.status = 'active'
  AND rg.tombstoned_at IS NULL
  AND rg.effective_at <= $4
  AND (rg.expires_at IS NULL OR rg.expires_at > $4)
GROUP BY m.tenant_id, m.workspace_id, u.subject_id_hash, m.policy_revision_hash, u.user_id
ORDER BY MAX(m.effective_at) DESC
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

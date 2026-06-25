// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const localIdentityBootstrapLockQuery = `
SELECT pg_advisory_xact_lock(3455)
`

const countExistingLocalIdentityUsersQuery = `
SELECT COUNT(DISTINCT u.user_id)
FROM identity_users u
JOIN identity_local_credentials c ON c.user_id = u.user_id
WHERE u.tombstoned_at IS NULL
  AND c.status = 'active'
  AND c.revoked_at IS NULL
`

const insertLocalIdentityUserQuery = `
INSERT INTO identity_users (
    user_id,
    subject_id_hash,
    status,
    profile_handle_hash,
    created_at,
    updated_at,
    disabled_at,
    tombstoned_at
) VALUES ($1, $2, 'active', NULLIF($3, ''), $4, $4, NULL, NULL)
`

// #nosec G101 -- SQL DML whose const name contains "Credential"; the value is a fully-parameterized INSERT, not a credential literal
const insertLocalIdentityCredentialQuery = `
INSERT INTO identity_local_credentials (
    credential_id,
    user_id,
    password_hash,
    password_algorithm,
    password_parameters_hash,
    status,
    created_at,
    rotated_at,
    expires_at,
    revoked_at
) VALUES ($1, $2, $3, $4, $5, 'active', $6, $6, NULL, NULL)
`

const insertLocalIdentityMFAFactorQuery = `
INSERT INTO identity_mfa_factors (
    factor_id,
    user_id,
    factor_kind,
    status,
    secret_credential_handle,
    public_key_hash,
    created_at,
    verified_at,
    last_used_at,
    revoked_at
) VALUES ($1, $2, $3, 'active', NULLIF($4, ''), NULL, $5, $5, NULL, NULL)
`

const insertLocalIdentityRecoveryCodeQuery = `
INSERT INTO identity_mfa_recovery_codes (
    user_id,
    factor_id,
    recovery_code_hash,
    status,
    created_at,
    used_at,
    revoked_at
) VALUES ($1, $2, $3, 'active', $4, NULL, NULL)
`

const upsertLocalIdentityRoleQuery = `
INSERT INTO identity_roles (
    tenant_id,
    role_id,
    role_key_hash,
    status,
    built_in,
    policy_revision_hash,
    created_at,
    updated_at,
    tombstoned_at
) VALUES ($1, $2, $3, 'active', true, $4, $5, $5, NULL)
ON CONFLICT (tenant_id, role_id) DO UPDATE
SET status = 'active',
    built_in = true,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    updated_at = EXCLUDED.updated_at,
    tombstoned_at = NULL
`

const insertLocalIdentityMembershipQuery = `
INSERT INTO identity_tenant_memberships (
    tenant_id,
    workspace_id,
    user_id,
    status,
    membership_source,
    policy_revision_hash,
    effective_at,
    expires_at,
    disabled_at,
    tombstoned_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, 'active', $4, $5, $6, NULL, NULL, NULL, $6, $6)
ON CONFLICT (tenant_id, workspace_id, user_id) DO UPDATE
SET status = 'active',
    membership_source = EXCLUDED.membership_source,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    effective_at = EXCLUDED.effective_at,
    disabled_at = NULL,
    tombstoned_at = NULL,
    updated_at = EXCLUDED.updated_at
`

const insertLocalIdentityMembershipRoleQuery = `
INSERT INTO identity_membership_roles (
    tenant_id,
    workspace_id,
    user_id,
    role_id,
    assignment_source,
    status,
    policy_revision_hash,
    effective_at,
    expires_at,
    tombstoned_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, NULL, NULL, $7, $7)
ON CONFLICT (tenant_id, workspace_id, user_id, role_id) DO UPDATE
SET assignment_source = EXCLUDED.assignment_source,
    status = 'active',
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    effective_at = EXCLUDED.effective_at,
    tombstoned_at = NULL,
    updated_at = EXCLUDED.updated_at
`

const createLocalIdentityInvitationQuery = `
INSERT INTO identity_invitations (
    invite_id,
    tenant_id,
    workspace_id,
    invite_code_hash,
    invitee_handle_hash,
    inviter_subject_id_hash,
    role_id,
    status,
    policy_revision_hash,
    expires_at,
    accepted_by_user_id,
    accepted_at,
    revoked_at,
    tombstoned_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, NULL, NULL, NULL, NULL, $11, $12)
ON CONFLICT (invite_id) DO UPDATE
SET invite_code_hash = EXCLUDED.invite_code_hash,
    invitee_handle_hash = EXCLUDED.invitee_handle_hash,
    inviter_subject_id_hash = EXCLUDED.inviter_subject_id_hash,
    role_id = EXCLUDED.role_id,
    status = EXCLUDED.status,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    expires_at = EXCLUDED.expires_at,
    revoked_at = NULL,
    tombstoned_at = NULL,
    updated_at = EXCLUDED.updated_at
`

const selectLocalIdentityInvitationForAcceptQuery = `
SELECT invite_id, tenant_id, workspace_id, role_id, policy_revision_hash
FROM identity_invitations
WHERE invite_code_hash = $1
  AND status = 'active'
  AND accepted_at IS NULL
  AND revoked_at IS NULL
  AND tombstoned_at IS NULL
  AND expires_at > $2
FOR UPDATE
`

const markLocalIdentityInvitationAcceptedQuery = `
UPDATE identity_invitations
SET status = 'accepted',
    accepted_by_user_id = $2,
    accepted_at = $3,
    updated_at = $3
WHERE invite_id = $1
  AND status = 'active'
  AND accepted_at IS NULL
  AND revoked_at IS NULL
  AND tombstoned_at IS NULL
`

// #nosec G101 -- SQL SELECT whose const name contains "Credential"; the value is a fully-parameterized query selecting the password_hash column, not a credential literal
const selectLocalIdentityCredentialQuery = `
SELECT
    u.user_id,
    m.tenant_id,
    m.workspace_id,
    u.subject_id_hash,
    c.password_hash,
    u.status,
    u.disabled_at,
    a.locked_until,
    COALESCE(a.failed_attempts, 0),
    EXISTS (
        SELECT 1
        FROM identity_membership_roles mr
        WHERE mr.tenant_id = m.tenant_id
          AND mr.workspace_id = m.workspace_id
          AND mr.user_id = u.user_id
          AND mr.role_id IN ('owner', 'tenant_admin')
          AND mr.status = 'active'
          AND mr.tombstoned_at IS NULL
          AND mr.effective_at <= $2
          AND (mr.expires_at IS NULL OR mr.expires_at > $2)
    ) AS has_admin_role,
    EXISTS (
        SELECT 1
        FROM identity_mfa_factors f
        WHERE f.user_id = u.user_id
          AND f.status = 'active'
          AND f.revoked_at IS NULL
    ) AS has_active_mfa,
    m.policy_revision_hash
FROM identity_users u
JOIN identity_local_credentials c ON c.user_id = u.user_id
JOIN identity_tenant_memberships m ON m.user_id = u.user_id
LEFT JOIN identity_local_auth_attempts a ON a.user_id = u.user_id
WHERE u.subject_id_hash = $1
  AND u.tombstoned_at IS NULL
  AND c.status = 'active'
  AND c.revoked_at IS NULL
  AND m.status = 'active'
  AND m.disabled_at IS NULL
  AND m.tombstoned_at IS NULL
  AND m.effective_at <= $2
  AND (m.expires_at IS NULL OR m.expires_at > $2)
ORDER BY m.effective_at DESC, c.rotated_at DESC
LIMIT 1
`

const upsertLocalIdentityFailedAttemptQuery = `
INSERT INTO identity_local_auth_attempts (
    user_id,
    failed_attempts,
    locked_until,
    last_failed_at,
    updated_at
) VALUES ($1, 1, CASE WHEN 1 >= $2 THEN $3 ELSE NULL END, $4, $4)
ON CONFLICT (user_id) DO UPDATE
SET failed_attempts = identity_local_auth_attempts.failed_attempts + 1,
    locked_until = CASE
        WHEN identity_local_auth_attempts.failed_attempts + 1 >= $2 THEN $3
        ELSE identity_local_auth_attempts.locked_until
    END,
    last_failed_at = EXCLUDED.last_failed_at,
    updated_at = EXCLUDED.updated_at
`

const clearLocalIdentityFailedAttemptsQuery = `
DELETE FROM identity_local_auth_attempts
WHERE user_id = $1
`

// resolveLocalIdentityRolesQuery lists the active membership role IDs granted to
// one local user within a tenant/workspace as of a point in time. It applies the
// same active-membership, active-assignment, and active-role predicates as the
// personal-API-token role query so a local cookie session and a scoped token for
// the same user resolve to the same role set.
const resolveLocalIdentityRolesQuery = `
SELECT DISTINCT role_assignment.role_id
FROM identity_membership_roles role_assignment
JOIN identity_tenant_memberships membership
    ON membership.tenant_id = role_assignment.tenant_id
   AND membership.workspace_id = role_assignment.workspace_id
   AND membership.user_id = role_assignment.user_id
JOIN identity_roles role
    ON role.tenant_id = role_assignment.tenant_id
   AND role.role_id = role_assignment.role_id
WHERE role_assignment.tenant_id = $1
  AND role_assignment.workspace_id = $2
  AND role_assignment.user_id = $3
  AND role_assignment.status = 'active'
  AND role_assignment.tombstoned_at IS NULL
  AND role_assignment.effective_at <= $4
  AND (role_assignment.expires_at IS NULL OR role_assignment.expires_at > $4)
  AND membership.status = 'active'
  AND membership.disabled_at IS NULL
  AND membership.tombstoned_at IS NULL
  AND membership.effective_at <= $4
  AND (membership.expires_at IS NULL OR membership.expires_at > $4)
  AND role.status = 'active'
  AND role.tombstoned_at IS NULL
ORDER BY role_assignment.role_id ASC
LIMIT $5
`

const consumeLocalIdentityRecoveryCodeQuery = `
UPDATE identity_mfa_recovery_codes
SET status = 'used',
    used_at = $3
WHERE user_id = $1
  AND recovery_code_hash = $2
  AND status = 'active'
  AND used_at IS NULL
  AND revoked_at IS NULL
`

const revokeLocalIdentityCredentialsQuery = `
UPDATE identity_local_credentials
SET status = 'revoked',
    revoked_at = $2
WHERE user_id = $1
  AND status = 'active'
  AND revoked_at IS NULL
`

const revokeLocalIdentityRecoveryCodesQuery = `
UPDATE identity_mfa_recovery_codes
SET status = 'revoked',
    revoked_at = $2
WHERE user_id = $1
  AND status = 'active'
  AND revoked_at IS NULL
`

const revokeLocalIdentityMFAFactorsQuery = `
UPDATE identity_mfa_factors
SET status = 'revoked',
    revoked_at = $2
WHERE user_id = $1
  AND status = 'active'
  AND revoked_at IS NULL
`

const disableLocalIdentityUserQuery = `
UPDATE identity_users
SET status = 'disabled',
    disabled_at = $2,
    updated_at = $2
WHERE user_id = $1
  AND tombstoned_at IS NULL
`

const revokeLocalIdentityBrowserSessionsQuery = `
UPDATE browser_sessions
SET revoked_at = $2,
    updated_at = $2
WHERE subject_id_hash = (
    SELECT subject_id_hash
    FROM identity_users
    WHERE user_id = $1
)
  AND revoked_at IS NULL
`

const enableLocalIdentityBreakGlassQuery = `
INSERT INTO identity_break_glass_windows (
    recovery_id,
    tenant_id,
    workspace_id,
    subject_id_hash,
    break_glass_code_hash,
    status,
    reason_code,
    policy_revision_hash,
    enabled_at,
    expires_at,
    disabled_at,
    used_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, NULL, $11, $12)
ON CONFLICT (recovery_id) DO UPDATE
SET break_glass_code_hash = EXCLUDED.break_glass_code_hash,
    status = EXCLUDED.status,
    reason_code = EXCLUDED.reason_code,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    enabled_at = EXCLUDED.enabled_at,
    expires_at = EXCLUDED.expires_at,
    disabled_at = NULL,
    used_at = NULL,
    updated_at = EXCLUDED.updated_at
`

const consumeLocalIdentityBreakGlassQuery = `
UPDATE identity_break_glass_windows
SET used_at = $2,
    updated_at = $2
WHERE break_glass_code_hash = $1
  AND expires_at > $2
  AND enabled_at <= $2
  AND status = 'active'
  AND disabled_at IS NULL
  AND used_at IS NULL
RETURNING tenant_id, workspace_id, subject_id_hash, policy_revision_hash
`

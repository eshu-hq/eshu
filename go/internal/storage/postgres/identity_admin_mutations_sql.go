package postgres

// SQL for tenant-scoped admin identity mutations (issue #3703 PR-2). Every
// statement is parameterized and tenant/workspace scoped. Writes are idempotent
// under retry via active-row conflict keys and terminal-state guards, never via
// table locks or single-threading. No statement selects or writes a secret,
// invite code, credential handle, or raw external group name; the external group
// is supplied to the store only as its precomputed hash.

// selectAdminInvitationStatusQuery reads the current status of one invitation in
// the caller's tenant/workspace. tombstoned_at IS NULL excludes soft-deleted
// rows. It is used to distinguish a missing invitation (not found) from one that
// exists but is already in a terminal state (idempotent no-op). FOR UPDATE locks
// the row for the duration of the revoke transaction so a concurrent revoke
// cannot interleave between the read and the write.
const selectAdminInvitationStatusQuery = `
SELECT status, revoked_at, accepted_at, expires_at
FROM identity_invitations
WHERE invite_id = $1
  AND tenant_id = $2
  AND workspace_id = $3
  AND tombstoned_at IS NULL
FOR UPDATE
`

// revokeAdminInvitationQuery soft-revokes one invitation only when it is still
// active (status='active', not yet revoked or accepted). It is a no-op against
// any other state, so a double revoke affects zero rows without error. The
// tenant/workspace predicate guarantees an admin can only revoke within their
// own tenant.
const revokeAdminInvitationQuery = `
UPDATE identity_invitations
SET status = 'revoked',
    revoked_at = $4,
    updated_at = $4
WHERE invite_id = $1
  AND tenant_id = $2
  AND workspace_id = $3
  AND status = 'active'
  AND revoked_at IS NULL
  AND accepted_at IS NULL
  AND tombstoned_at IS NULL
`

// selectActiveRoleExistsQuery reports whether a role exists and is active in the
// tenant. A grant or mapping that references an unknown or tombstoned role must
// be rejected rather than fabricating a row.
const selectActiveRoleExistsQuery = `
SELECT 1
FROM identity_roles
WHERE tenant_id = $1
  AND role_id = $2
  AND status = 'active'
  AND tombstoned_at IS NULL
LIMIT 1
`

// selectActiveMembershipExistsQuery reports whether the user has an active
// (non-disabled, non-tombstoned) tenant/workspace membership. Granting a role to
// a non-member fails the identity_membership_roles foreign key; this precheck
// turns that foreseeable bad-input into an explicit 4xx instead of a 500.
const selectActiveMembershipExistsQuery = `
SELECT 1
FROM identity_tenant_memberships
WHERE tenant_id = $1
  AND workspace_id = $2
  AND user_id = $3
  AND status = 'active'
  AND tombstoned_at IS NULL
  AND disabled_at IS NULL
LIMIT 1
`

// selectActiveProviderExistsQuery reports whether a provider config exists and is
// active in the tenant. A group mapping that references an unknown or tombstoned
// provider must be rejected rather than fabricating a row.
const selectActiveProviderExistsQuery = `
SELECT 1
FROM identity_provider_configs
WHERE provider_config_id = $1
  AND tenant_id = $2
  AND status = 'active'
  AND tombstoned_at IS NULL
LIMIT 1
`

// grantAdminRoleAssignmentQuery idempotently activates a membership-role row.
// The conflict target is the table's full primary key (tenant, workspace, user,
// role), so a concurrent double grant converges on one row rather than creating
// duplicates or erroring on the active partial index. A previously revoked or
// tombstoned assignment is reactivated (status='active', tombstoned_at cleared).
// xmax = 0 distinguishes a fresh insert (changed=true) from a conflict update.
const grantAdminRoleAssignmentQuery = `
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
SET status = 'active',
    assignment_source = EXCLUDED.assignment_source,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    effective_at = EXCLUDED.effective_at,
    expires_at = NULL,
    tombstoned_at = NULL,
    updated_at = EXCLUDED.updated_at
RETURNING status, (xmax = 0) AS inserted
`

// revokeAdminRoleAssignmentQuery tombstones one active membership-role row. It
// is a no-op against an already-revoked or absent row, so a double revoke
// affects zero rows without error.
const revokeAdminRoleAssignmentQuery = `
UPDATE identity_membership_roles
SET status = 'revoked',
    tombstoned_at = $5,
    updated_at = $5
WHERE tenant_id = $1
  AND workspace_id = $2
  AND user_id = $3
  AND role_id = $4
  AND status = 'active'
  AND tombstoned_at IS NULL
`

// createAdminIdPGroupMappingQuery idempotently activates a group->role mapping.
// The conflict target is the table's full primary key (provider, group hash,
// tenant, workspace, role), so a concurrent re-create converges on one row. The
// external_group_hash ($2) is precomputed by the handler with the same hash the
// OIDC login path uses; the raw group name never reaches this layer. The
// RETURNING clause emits the opaque mapping_ref (md5 over the composite key,
// matching the read path) so the API can address the mapping without the hash.
const createAdminIdPGroupMappingQuery = `
INSERT INTO identity_provider_group_role_mappings (
    provider_config_id,
    external_group_hash,
    tenant_id,
    workspace_id,
    role_id,
    status,
    mapping_source,
    policy_revision_hash,
    effective_at,
    expires_at,
    tombstoned_at,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, NULL, NULL, $8, $8)
ON CONFLICT (provider_config_id, external_group_hash, tenant_id, workspace_id, role_id) DO UPDATE
SET status = 'active',
    mapping_source = EXCLUDED.mapping_source,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    effective_at = EXCLUDED.effective_at,
    expires_at = NULL,
    tombstoned_at = NULL,
    updated_at = EXCLUDED.updated_at
RETURNING
    md5(provider_config_id || ':' || tenant_id || ':' || workspace_id || ':' || role_id || ':' || external_group_hash) AS mapping_ref,
    status,
    (xmax = 0) AS inserted
`

// deleteAdminIdPGroupMappingQuery tombstones one active group->role mapping
// resolved by its opaque mapping_ref (an md5 digest over the composite key,
// computed the same way as the read path). The digest match is anchored to the
// caller's tenant/workspace, so an admin can never delete a mapping in another
// tenant even if a ref collides. It is a no-op against an already-deleted or
// absent mapping. RETURNING reports whether a row was tombstoned.
const deleteAdminIdPGroupMappingQuery = `
UPDATE identity_provider_group_role_mappings
SET status = 'revoked',
    tombstoned_at = $3,
    updated_at = $3
WHERE tenant_id = $1
  AND workspace_id = $2
  AND status = 'active'
  AND tombstoned_at IS NULL
  AND md5(provider_config_id || ':' || tenant_id || ':' || workspace_id || ':' || role_id || ':' || external_group_hash) = $4
RETURNING provider_config_id
`

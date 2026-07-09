// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// selectSignInPolicyQuery reads one tenant's sign-in policy for a
// non-transactional, non-locking read (GetSignInPolicy).
const selectSignInPolicyQuery = `
SELECT require_sso, allow_local_user_creation, require_mfa_for_all_users,
       idle_timeout_seconds, absolute_timeout_seconds,
       sso_admin_verified_at, sso_admin_verified_provider_config_id,
       policy_revision_hash, updated_at
FROM identity_sign_in_policies
WHERE tenant_id = $1
`

// ensureSignInPolicyRowQuery lazily materializes a default row so
// selectSignInPolicyForUpdateQuery's FOR UPDATE has a row to lock even on the
// very first write for a tenant. It is idempotent and never overwrites an
// existing row's values.
const ensureSignInPolicyRowQuery = `
INSERT INTO identity_sign_in_policies (tenant_id)
VALUES ($1)
ON CONFLICT (tenant_id) DO NOTHING
`

// selectSignInPolicyForUpdateQuery row-locks the tenant's policy for the
// duration of the caller's transaction, serializing concurrent
// UpsertSignInPolicy calls for the same tenant so a lost-update race can
// never silently drop one admin's change.
const selectSignInPolicyForUpdateQuery = `
SELECT require_sso, allow_local_user_creation, require_mfa_for_all_users,
       idle_timeout_seconds, absolute_timeout_seconds,
       sso_admin_verified_at, sso_admin_verified_provider_config_id,
       policy_revision_hash, updated_at
FROM identity_sign_in_policies
WHERE tenant_id = $1
FOR UPDATE
`

// upsertSignInPolicyRowQuery persists the full policy row within the caller's
// locked transaction.
const upsertSignInPolicyRowQuery = `
INSERT INTO identity_sign_in_policies (
    tenant_id, require_sso, allow_local_user_creation, require_mfa_for_all_users,
    idle_timeout_seconds, absolute_timeout_seconds, policy_revision_hash, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (tenant_id) DO UPDATE SET
    require_sso = EXCLUDED.require_sso,
    allow_local_user_creation = EXCLUDED.allow_local_user_creation,
    require_mfa_for_all_users = EXCLUDED.require_mfa_for_all_users,
    idle_timeout_seconds = EXCLUDED.idle_timeout_seconds,
    absolute_timeout_seconds = EXCLUDED.absolute_timeout_seconds,
    policy_revision_hash = EXCLUDED.policy_revision_hash,
    updated_at = EXCLUDED.updated_at
`

// countActiveProviderConfigsQuery counts provider configs a tenant has
// actually enabled. Status only reaches 'active' via a synchronous passing
// TestProviderConnection call (see AdminProviderConfigMutationStore in
// go/internal/query), so this count IS the durable "at least one provider
// has a passing connection test" proof — no separate "last test passed"
// column exists by design.
const countActiveProviderConfigsQuery = `
SELECT count(*) FROM identity_provider_configs
WHERE tenant_id = $1 AND status = 'active' AND tombstoned_at IS NULL
`

// recordSSOAdminVerificationQuery sets sso_admin_verified_at/
// sso_admin_verified_provider_config_id exactly once per tenant (sticky:
// COALESCE keeps the first-ever value on every later call). Best-effort,
// non-transactional: this is a proof timestamp, not itself a security
// boundary, and losing a duplicate write under a rare race only delays a
// later successful call from setting the same sticky value.
const recordSSOAdminVerificationQuery = `
INSERT INTO identity_sign_in_policies (
    tenant_id, sso_admin_verified_at, sso_admin_verified_provider_config_id, updated_at
)
VALUES ($1, $2, $3, $2)
ON CONFLICT (tenant_id) DO UPDATE SET
    sso_admin_verified_at = COALESCE(identity_sign_in_policies.sso_admin_verified_at, EXCLUDED.sso_admin_verified_at),
    sso_admin_verified_provider_config_id = COALESCE(identity_sign_in_policies.sso_admin_verified_provider_config_id, EXCLUDED.sso_admin_verified_provider_config_id)
`

// selectSignInPolicyRequireMFAQuery reads only the require_mfa_for_all_users
// flag for one tenant, used inside AcceptLocalIdentityInvitation's existing
// transaction. Absence of a row means the default (false).
const selectSignInPolicyRequireMFAQuery = `
SELECT require_mfa_for_all_users FROM identity_sign_in_policies WHERE tenant_id = $1
`

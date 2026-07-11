// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// insertLocalIdentityTOTPFactorQuery inserts one PENDING TOTP MFA factor.
// This is a dedicated insert (not the shared insertLocalIdentityMFAFactorQuery,
// which always inserts 'active') because TOTP enrollment MUST prove one
// submitted code before the factor can satisfy MFA login or count toward
// GetLocalIdentityMFAStatus.HasActiveMFA — both read status = 'active' only
// (issue #4986 approved design).
const insertLocalIdentityTOTPFactorQuery = `
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
) VALUES ($1, $2, 'totp', 'pending', $3, NULL, $4, NULL, NULL, NULL)
`

// selectLocalIdentityPendingTOTPSecretQuery reads the sealed secret for one
// pending (unconfirmed) TOTP enrollment, scoped to the exact user and
// factor id the enrollment began for.
const selectLocalIdentityPendingTOTPSecretQuery = `
SELECT secret_credential_handle
FROM identity_mfa_factors
WHERE user_id = $1
  AND factor_id = $2
  AND factor_kind = 'totp'
  AND status = 'pending'
  AND revoked_at IS NULL
`

// activateLocalIdentityTOTPFactorQuery flips a pending TOTP factor to active
// once its first submitted code has verified. Scoped by status = 'pending'
// so a concurrent second confirm (or a revoke) between the select and this
// update affects zero rows rather than double-activating or resurrecting a
// revoked factor.
const activateLocalIdentityTOTPFactorQuery = `
UPDATE identity_mfa_factors
SET status = 'active',
    verified_at = $3,
    last_used_at = $3
WHERE user_id = $1
  AND factor_id = $2
  AND factor_kind = 'totp'
  AND status = 'pending'
  AND revoked_at IS NULL
`

// selectLocalIdentityActiveTOTPSecretQuery reads the sealed secret(s) for
// every ACTIVE totp factor of one user, for login-path verification. A user
// has at most one active totp factor in the current enrollment flow (a
// second BeginLocalIdentityTOTPEnrollment call creates a second pending row
// rather than revoking the first), but the query does not assume
// uniqueness — the login path tries every returned row in order.
const selectLocalIdentityActiveTOTPSecretQuery = `
SELECT factor_id, secret_credential_handle
FROM identity_mfa_factors
WHERE user_id = $1
  AND factor_kind = 'totp'
  AND status = 'active'
  AND revoked_at IS NULL
ORDER BY created_at DESC
`

// touchLocalIdentityTOTPLastUsedQuery records the login-time last_used_at
// stamp for the totp factor that verified a submitted code.
const touchLocalIdentityTOTPLastUsedQuery = `
UPDATE identity_mfa_factors
SET last_used_at = $3
WHERE user_id = $1
  AND factor_id = $2
  AND factor_kind = 'totp'
  AND status = 'active'
  AND revoked_at IS NULL
`

// selectLocalIdentityUserIDBySubjectHashQuery resolves the internal user_id
// for a session's subject_id_hash, for self-service TOTP enrollment
// endpoints (go/internal/query) that only ever hold the session's
// subject_id_hash, never the internal user_id. Mirrors the subject-hash
// join getLocalIdentityMFAStatusQuery already uses.
const selectLocalIdentityUserIDBySubjectHashQuery = `
SELECT user_id
FROM identity_users
WHERE subject_id_hash = $1
  AND tombstoned_at IS NULL
`

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// bootstrapCredentialLockQuery serializes Generate and Reset against the
// identity_bootstrap_credentials table. ConsumeBootstrapCredential
// deliberately does not take this lock: its atomic conditional
// UPDATE ... WHERE consumed_at IS NULL is itself the concurrency guard (see
// ConsumeBootstrapCredential's doc comment in identity_bootstrap_credential.go).
// 3455 is BootstrapLocalIdentity's own
// local-identity advisory lock (identity_local_sql.go). The two keys are
// held together in one transaction only by GenerateBootstrapAdminWithCredential
// (identity_bootstrap_credential.go), always in the fixed order 3455 then
// 3456; that fixed same-session ordering, not separation, is what rules out
// a deadlock between them.
// #nosec G101 -- SQL DML whose const name contains "Credential"; the value is a fully-parameterized query, not a credential literal
const bootstrapCredentialLockQuery = `
SELECT pg_advisory_xact_lock(3456)
`

// generateBootstrapCredentialQuery idempotently inserts the sealed one-time
// admin credential envelope. RETURNING 1 only produces a row on a genuine
// insert; a conflict (already provisioned) returns zero rows.
// #nosec G101 -- SQL DML whose const name contains "Credential"; the value is a fully-parameterized query, not a credential literal
const generateBootstrapCredentialQuery = `
INSERT INTO identity_bootstrap_credentials (
    tenant_id,
    workspace_id,
    subject_id_hash,
    username_hash,
    sealed_credential,
    key_id,
    generated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id, workspace_id) DO NOTHING
RETURNING 1
`

// selectBootstrapCredentialQuery returns the retrievable sealed envelope: a
// row consumed (or reset-then-never-regenerated to empty) never matches.
// #nosec G101 -- SQL DML whose const name contains "Credential"; the value is a fully-parameterized query, not a credential literal
const selectBootstrapCredentialQuery = `
SELECT sealed_credential, key_id
FROM identity_bootstrap_credentials
WHERE tenant_id = $1
  AND workspace_id = $2
  AND consumed_at IS NULL
  AND sealed_credential <> ''
`

// consumeBootstrapCredentialQuery destroys the retrievable ciphertext on the
// bootstrap subject's first successful login. subject_id_hash scopes the
// update so a different subject's login can never consume this row.
const consumeBootstrapCredentialQuery = `
UPDATE identity_bootstrap_credentials
SET sealed_credential = '',
    consumed_at = $4
WHERE tenant_id = $1
  AND workspace_id = $2
  AND subject_id_hash = $3
  AND consumed_at IS NULL
`

// selectBootstrapCredentialSubjectQuery resolves the owning subject for a
// reset; ResetBootstrapCredential fails closed with
// ErrBootstrapCredentialNotFound when no row exists for the tenant/workspace.
const selectBootstrapCredentialSubjectQuery = `
SELECT subject_id_hash
FROM identity_bootstrap_credentials
WHERE tenant_id = $1
  AND workspace_id = $2
`

// selectBootstrapCredentialConsumedStateQuery reports whether the bootstrap
// credential row for (tenant, workspace, subject) has already been consumed.
// CompleteSetupMFA (identity_setup_completion.go, #4990) runs this inside its
// pg_advisory_xact_lock(3456) critical section to detect a concurrent
// completion that already won the race before this caller acquired the
// lock — a missing row is treated as "already consumed" by the caller (fail
// closed) rather than this query inventing a row that was never generated.
const selectBootstrapCredentialConsumedStateQuery = `
SELECT consumed_at IS NOT NULL
FROM identity_bootstrap_credentials
WHERE tenant_id = $1
  AND workspace_id = $2
  AND subject_id_hash = $3
`

// selectBootstrapCredentialOwnerUserIDQuery resolves the active local user
// that owns a bootstrap credential's subject, so Reset can rotate its bcrypt
// hash in the same transaction as the envelope re-seal.
// #nosec G101 -- SQL DML whose const name contains "Credential"; the value is a fully-parameterized query, not a credential literal
const selectBootstrapCredentialOwnerUserIDQuery = `
SELECT user_id
FROM identity_users
WHERE subject_id_hash = $1
  AND tombstoned_at IS NULL
`

// resetBootstrapCredentialQuery unconditionally re-arms retrieval: it always
// clears consumed_at, regardless of the row's prior consumption state, since
// the whole point of a reset is to make the credential retrievable again.
const resetBootstrapCredentialQuery = `
UPDATE identity_bootstrap_credentials
SET sealed_credential = $3,
    key_id = $4,
    consumed_at = NULL,
    reset_at = $5,
    reset_count = reset_count + 1
WHERE tenant_id = $1
  AND workspace_id = $2
`

// rotateBootstrapCredentialPasswordQuery keeps the database password and the
// sealed envelope from ever diverging: it rotates the same user's active
// local credential in the same transaction as resetBootstrapCredentialQuery.
const rotateBootstrapCredentialPasswordQuery = `
UPDATE identity_local_credentials
SET password_hash = $2,
    password_algorithm = $3,
    password_parameters_hash = $4,
    rotated_at = $5
WHERE user_id = $1
  AND status = 'active'
  AND revoked_at IS NULL
`

// revokeBootstrapCredentialRecoveryFactorsQuery revokes only the $2-kind MFA
// factor(s) for a user (ResetBootstrapCredential always passes
// localIdentityRecoveryCodeFactorKind). This is deliberately narrower than
// revokeLocalIdentityMFAFactorsQuery (identity_local_sql.go), which revokes
// every active factor regardless of kind: a bootstrap credential reset must
// never revoke a TOTP factor the admin enrolled after bootstrap.
const revokeBootstrapCredentialRecoveryFactorsQuery = `
UPDATE identity_mfa_factors
SET status = 'revoked',
    revoked_at = $3
WHERE user_id = $1
  AND factor_kind = $2
  AND status = 'active'
  AND revoked_at IS NULL
`

// revokeBootstrapCredentialRecoveryCodesQuery revokes only the recovery codes
// owned by a $2-kind MFA factor (ResetBootstrapCredential always passes
// localIdentityRecoveryCodeFactorKind). identity_mfa_recovery_codes.factor_id
// is a plain foreign key into identity_mfa_factors with no kind constraint of
// its own, and insertLocalIdentityMFA (identity_local_helpers.go) is a shared
// helper the general admin MFA-reset endpoint (ResetLocalIdentityMFA,
// identity_local_lifecycle.go) also calls with an operator-supplied
// mfa_factor_kind alongside recovery codes — so a TOTP-kind factor can end up
// owning rows in identity_mfa_recovery_codes even though TOTP enrollment
// itself never inserts there. Reusing the unscoped
// revokeLocalIdentityRecoveryCodesQuery (identity_local_sql.go) here would
// therefore silently destroy that TOTP factor's backup codes on every
// bootstrap credential reset (issue #5602 codex review). Scoping this query
// by owning factor_kind, mirroring revokeBootstrapCredentialRecoveryFactorsQuery
// above, keeps the reset from ever touching a factor kind other than
// recovery_code.
const revokeBootstrapCredentialRecoveryCodesQuery = `
UPDATE identity_mfa_recovery_codes
SET status = 'revoked',
    revoked_at = $3
WHERE user_id = $1
  AND status = 'active'
  AND revoked_at IS NULL
  AND factor_id IN (
      SELECT factor_id
      FROM identity_mfa_factors
      WHERE user_id = $1
        AND factor_kind = $2
  )
`

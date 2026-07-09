// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// bootstrapCredentialLockQuery serializes Generate/Consume/Reset against the
// identity_bootstrap_credentials table. 3455 is BootstrapLocalIdentity's own
// local-identity advisory lock (identity_local_sql.go); the two keys never
// nest inside one transaction, so they cannot deadlock each other.
const bootstrapCredentialLockQuery = `
SELECT pg_advisory_xact_lock(3456)
`

// generateBootstrapCredentialQuery idempotently inserts the sealed one-time
// admin credential envelope. RETURNING 1 only produces a row on a genuine
// insert; a conflict (already provisioned) returns zero rows.
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

// selectBootstrapCredentialOwnerUserIDQuery resolves the active local user
// that owns a bootstrap credential's subject, so Reset can rotate its bcrypt
// hash in the same transaction as the envelope re-seal.
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

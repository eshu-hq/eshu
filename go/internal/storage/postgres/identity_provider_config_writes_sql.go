// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// SQL for DB-backed provider-config CRUD writes (#4966). Every statement is
// parameterized and tenant-scoped. sealed_secret always carries an ESK1
// envelope (secretcrypto.Seal output) or is never selected here — no
// statement in this file ever selects sealed_secret; that is confined to
// identity_provider_config_reads.go's connection-test material query, which
// is documented as internal-only.

// insertProviderConfigQuery creates the provider-config row in draft status.
// ON CONFLICT on the tenant/kind/key partial unique index (WHERE tombstoned_at
// IS NULL) makes a duplicate create a no-op instead of a 500; the caller
// distinguishes a fresh insert from a conflict via the RETURNING inserted flag.
const insertProviderConfigQuery = `
INSERT INTO identity_provider_configs (
    provider_config_id,
    tenant_id,
    provider_kind,
    provider_key_hash,
    status,
    issuer_hash,
    metadata_url_hash,
    entity_id_hash,
    client_id_hash,
    credential_handle,
    active_revision_id,
    duplicate_of_provider_config_id,
    created_at,
    updated_at,
    tombstoned_at
) VALUES ($1, $2, $3, $4, 'draft', $5, $6, $7, $8, NULL, NULL, NULL, $9, $9, NULL)
ON CONFLICT (tenant_id, provider_kind, provider_key_hash)
    WHERE tombstoned_at IS NULL
    DO NOTHING
RETURNING provider_config_id
`

// insertProviderConfigRevisionQuery inserts one revision row carrying the
// sealed secret and non-secret configuration, immediately active.
const insertProviderConfigRevisionQuery = `
INSERT INTO identity_provider_config_revisions (
    provider_config_id,
    revision_id,
    status,
    configuration_hash,
    metadata_hash,
    metadata_handle,
    credential_handle,
    sealed_secret,
    configuration,
    created_at,
    activated_at,
    superseded_at
) VALUES ($1, $2, 'active', $3, $4, NULL, NULL, $5, $6, $7, $7, NULL)
`

// activateProviderConfigActiveRevisionQuery points the provider config at its
// newly created (Create/Update) or reactivated (Revert) active revision. It
// ALSO resets status back to 'draft' unconditionally. This is deliberate, not
// incidental: whenever the active revision changes, whatever secret/config
// material is now live has never been proven by a test-connection call
// against THIS revision — an already-'active' provider whose revision just
// changed must go back to requiring Enable (whose mandatory synchronous
// test-connection + compare-and-swap targets this exact revision id, see
// EnableProviderConfig) before it can be trusted for login again. Without
// this, an Update or Revert immediately after Enable would silently leave
// status='active' pointed at a revision nothing ever tested — the same
// invariant EnableProviderConfig's compare-and-swap protects, closed from the
// other direction (a real bug this package's own concurrency test caught:
// TestProviderConfigConcurrentUpdateDuringEnableRejectsStaleRevision). For
// CreateProviderConfig this is a no-op — status is already 'draft' from the
// INSERT moments earlier in the same transaction.
//
// RETURNING status lets UpdateProviderConfig report the row's actual
// post-transaction status (always 'draft' here) instead of the caller having
// to assume it from the pre-transaction row lock read (#4988: the prior
// status read under the lock, before this statement ran, went stale the
// moment this UPDATE committed).
const activateProviderConfigActiveRevisionQuery = `
UPDATE identity_provider_configs
SET active_revision_id = $3,
    status = 'draft',
    updated_at = $4
WHERE provider_config_id = $1 AND tenant_id = $2
RETURNING status
`

// selectProviderConfigForUpdateQuery row-locks the provider config for the
// duration of an update/revert/enable/disable transaction, serializing
// concurrent writers on this provider_config_id so exactly one revision is
// ever active (concurrency-deadlock-rigor: the conflict domain is one
// identity_provider_configs row).
const selectProviderConfigForUpdateQuery = `
SELECT provider_config_id, provider_kind, status, active_revision_id
FROM identity_provider_configs
WHERE provider_config_id = $1 AND tenant_id = $2 AND tombstoned_at IS NULL
FOR UPDATE
`

// supersedeProviderConfigRevisionQuery marks the currently active revision
// superseded. It is a no-op (0 rows) if the revision id is empty or already
// superseded, which is safe: the caller only reaches this after confirming
// the current active_revision_id under the row lock above.
const supersedeProviderConfigRevisionQuery = `
UPDATE identity_provider_config_revisions
SET status = 'superseded',
    superseded_at = $3
WHERE provider_config_id = $1 AND revision_id = $2 AND status = 'active'
`

// activateProviderConfigRevisionQuery (re)activates a specific revision —
// used by both the update path (for the just-inserted new revision, redundant
// with its own INSERT defaults but kept for revert reuse) and the revert path
// (for a prior, already-superseded revision).
const activateProviderConfigRevisionQuery = `
UPDATE identity_provider_config_revisions
SET status = 'active',
    activated_at = $3,
    superseded_at = NULL
WHERE provider_config_id = $1 AND revision_id = $2
`

// selectProviderConfigRevisionExistsQuery confirms a revision id belongs to
// the provider config before a revert targets it.
const selectProviderConfigRevisionExistsQuery = `
SELECT 1
FROM identity_provider_config_revisions
WHERE provider_config_id = $1 AND revision_id = $2
LIMIT 1
`

// setProviderConfigStatusQuery flips the provider config between draft and
// active. $3 is the target status ('active' or 'draft').
const setProviderConfigStatusQuery = `
UPDATE identity_provider_configs
SET status = $3,
    updated_at = $4
WHERE provider_config_id = $1 AND tenant_id = $2 AND tombstoned_at IS NULL
RETURNING status
`

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package consumption

// beginRebuildTransactionSQL bounds every lock wait in a rebuild transaction
// and marks the transaction as the sidecar writer, as the pre-#7663 rebuild
// did. Both settings are transaction-local (set_config's third argument).
// The writer value must match the guard in migration 139's
// package_manifest_consumption_keys_mark_dirty() trigger function.
const beginRebuildTransactionSQL = `
SELECT
    set_config('lock_timeout', $1, true),
    set_config('eshu.package_manifest_consumption_keys_writer', 'sidecar', true)
`

// lockScopeSQL takes FOR NO KEY UPDATE on the scope row. It conflicts with
// the ingester's upsertIngestionScopeQuery (ON CONFLICT DO UPDATE of non-key
// columns), the only writer that sets the sidecar-writer GUC and so bypasses
// the dirty trigger. It does not conflict with the FOR KEY SHARE that a
// foreign-key check takes, so reducer inserts into fact_records and the other
// tables that reference ingestion_scopes never wait on a rebuild (#7663).
const lockScopeSQL = `
SELECT scope_id FROM ingestion_scopes WHERE scope_id = $1 FOR NO KEY UPDATE
`

const dirtyScopePresentSQL = `
SELECT EXISTS (
    SELECT 1 FROM package_manifest_consumption_key_dirty_scopes WHERE scope_id = $1
)
`

// fenceDirtyScopeSQL inserts the scope's dirty row, or rewrites it in place
// with its marked_at unchanged so repair order is kept, and returns the new
// row version. Readers treat any dirty row as not ready, so the fence keeps
// the scope unready while its pages commit one at a time. Every later write
// to the row, by an old writer's trigger or by a concurrent rebuild of the
// same scope, creates a new version and so changes xmin.
const fenceDirtyScopeSQL = `
INSERT INTO package_manifest_consumption_key_dirty_scopes AS dirty (scope_id)
VALUES ($1)
ON CONFLICT (scope_id) DO UPDATE SET marked_at = dirty.marked_at
RETURNING xmin::text
`

// clearDirtyScopeIfUnchangedSQL removes the dirty row only when it is still
// the version the start transaction wrote. It compares xmin, not marked_at:
// the dirty trigger stamps marked_at with the writer's transaction start, which
// can be older than, or equal to, the rebuild's own stamp. Under READ
// COMMITTED a DELETE that waits on a concurrent update rechecks this predicate
// against the new version, so a write that lands during the compare keeps the
// row too.
const clearDirtyScopeIfUnchangedSQL = `
DELETE FROM package_manifest_consumption_key_dirty_scopes
WHERE scope_id = $1 AND xmin::text = $2
`

const deleteDirtyScopeSQL = `
DELETE FROM package_manifest_consumption_key_dirty_scopes WHERE scope_id = $1
`

// The two sidecar deletes have no scope-leading index (#7420 owns adding one),
// so each is a sequential scan whose cost tracks the sidecar table, not the
// scope: 61-85 ms at 1M rows and 252-449 ms at 5M rows on the #7663 fixture.
// They run once per rebuild, in the short start transaction.
const deleteManifestKeysSQL = `
DELETE FROM package_manifest_consumption_keys WHERE scope_id = $1
`

const deleteRegistryKeysSQL = `
DELETE FROM package_registry_identity_keys WHERE scope_id = $1
`

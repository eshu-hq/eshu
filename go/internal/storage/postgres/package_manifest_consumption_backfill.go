// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	// PackageManifestConsumptionKeyBackfillMarker identifies the completed
	// manifest and registry sidecar version required before target reads.
	PackageManifestConsumptionKeyBackfillMarker        = "package_manifest_consumption_keys_v2"
	packageManifestConsumptionKeyBackfillBatchSize     = 500
	packageManifestConsumptionKeyBackfillScopesPerPass = 25
)

const packageManifestConsumptionKeysReadySQL = `
SELECT EXISTS (
    SELECT 1
    FROM package_manifest_consumption_key_backfill_markers
    WHERE marker_name = $1
) AND NOT EXISTS (
    SELECT 1 FROM package_manifest_consumption_key_dirty_scopes
)
`

const packageManifestConsumptionKeyBackfillMarkerPresentSQL = `
SELECT EXISTS (
    SELECT 1 FROM package_manifest_consumption_key_backfill_markers WHERE marker_name = $1
)
`

const loadPackageManifestConsumptionKeyBackfillCursorSQL = `
SELECT cursor_scope_id
FROM package_manifest_consumption_key_backfill_progress
WHERE marker_name = $1
`

const savePackageManifestConsumptionKeyBackfillCursorSQL = `
INSERT INTO package_manifest_consumption_key_backfill_progress (marker_name, cursor_scope_id, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (marker_name) DO UPDATE
SET cursor_scope_id = EXCLUDED.cursor_scope_id, updated_at = EXCLUDED.updated_at
`

const listPackageManifestConsumptionKeyScopesSQL = `
SELECT DISTINCT scope_id
FROM fact_records
WHERE scope_id > $1
  AND (
      (fact_kind = 'content_entity'
          AND source_system = 'git'
          AND payload->>'entity_type' = 'Variable'
          AND COALESCE(NULLIF(payload->>'config_kind', ''), payload->'entity_metadata'->>'config_kind') = 'dependency')
      OR fact_kind = 'package_registry.package'
  )
ORDER BY scope_id
LIMIT $2
`

const lockPackageManifestConsumptionKeyScopeSQL = `
SELECT scope_id FROM ingestion_scopes WHERE scope_id = $1 FOR UPDATE
`

const listPackageManifestConsumptionKeyDirtyScopesSQL = `
SELECT scope_id
FROM package_manifest_consumption_key_dirty_scopes
ORDER BY marked_at, scope_id
LIMIT 1
`

const packageManifestConsumptionKeysDirtySQL = `
SELECT EXISTS (SELECT 1 FROM package_manifest_consumption_key_dirty_scopes)
`

const lockPackageManifestConsumptionKeyDirtyScopeSQL = `
SELECT scope_id
FROM package_manifest_consumption_key_dirty_scopes
WHERE scope_id = $1
FOR UPDATE
`

const deletePackageManifestConsumptionKeyScopeSQL = `
DELETE FROM package_manifest_consumption_keys WHERE scope_id = $1
`

const deletePackageRegistryIdentityKeyScopeSQL = `
DELETE FROM package_registry_identity_keys WHERE scope_id = $1
`

const deletePackageManifestConsumptionKeyDirtyScopeSQL = `
DELETE FROM package_manifest_consumption_key_dirty_scopes WHERE scope_id = $1
`

const listPackageManifestConsumptionKeyScopeFactsSQL = `
SELECT
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
    collector_kind, fencing_token, source_confidence, source_system,
    source_fact_key, COALESCE(source_uri, ''), COALESCE(source_record_id, ''),
    observed_at, is_tombstone, payload
FROM fact_records
WHERE scope_id = $1
  AND fact_id > $2
  AND (
      (fact_kind = 'content_entity'
          AND source_system = 'git'
          AND payload->>'entity_type' = 'Variable'
          AND COALESCE(NULLIF(payload->>'config_kind', ''), payload->'entity_metadata'->>'config_kind') = 'dependency')
      OR fact_kind = 'package_registry.package'
  )
ORDER BY fact_id
LIMIT $3
`

const markPackageManifestConsumptionKeyBackfillSQL = `
INSERT INTO package_manifest_consumption_key_backfill_markers (marker_name, completed_at)
VALUES ($1, $2)
ON CONFLICT (marker_name) DO NOTHING
`

// PackageManifestConsumptionKeysReady reports whether readers can use both
// consumption sidecars without omitting historical or old-writer facts.
func PackageManifestConsumptionKeysReady(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, packageManifestConsumptionKeysReadySQL, PackageManifestConsumptionKeyBackfillMarker)
	if err != nil {
		return false, fmt.Errorf("check package manifest consumption key readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var ready bool
	if err := rows.Scan(&ready); err != nil {
		return false, fmt.Errorf("scan package manifest consumption key readiness: %w", err)
	}
	return ready, rows.Err()
}

// BackfillPackageManifestConsumptionKeys performs one bounded, resumable pass.
// It records the v2 readiness marker only after both sidecars are current and
// the old-writer dirty fence is empty.
func BackfillPackageManifestConsumptionKeys(ctx context.Context, database db.ExecQueryer) error {
	beginner, ok := database.(db.Beginner)
	if !ok {
		return fmt.Errorf("package manifest consumption key backfill requires a transaction beginner")
	}
	ready, err := PackageManifestConsumptionKeysReady(ctx, database)
	if err != nil || ready {
		return err
	}
	markerPresent, err := packageManifestConsumptionKeyBackfillMarkerPresent(ctx, database)
	if err != nil {
		return err
	}
	if markerPresent {
		return repairPackageManifestConsumptionKeyDirtyScopes(ctx, beginner, database, packageManifestConsumptionKeyBackfillScopesPerPass)
	}

	cursor, err := loadPackageManifestConsumptionKeyBackfillCursor(ctx, database)
	if err != nil {
		return err
	}
	scopes, err := listPackageManifestConsumptionKeyScopes(ctx, database, cursor, packageManifestConsumptionKeyBackfillScopesPerPass)
	if err != nil {
		return err
	}
	for _, scopeID := range scopes {
		if err := rebuildPackageManifestConsumptionKeyScope(ctx, beginner, scopeID, false); err != nil {
			return err
		}
		if err := savePackageManifestConsumptionKeyBackfillCursor(ctx, database, scopeID); err != nil {
			return err
		}
		log.Printf("event_name=package_manifest_consumption_keys.backfill.progress phase=initial scope_id=%q", scopeID)
	}
	if len(scopes) == packageManifestConsumptionKeyBackfillScopesPerPass {
		return nil
	}
	if err := repairPackageManifestConsumptionKeyDirtyScopes(ctx, beginner, database, packageManifestConsumptionKeyBackfillScopesPerPass); err != nil {
		return err
	}
	dirty, err := packageManifestConsumptionKeysDirty(ctx, database)
	if err != nil || dirty {
		return err
	}
	if err := markPackageManifestConsumptionKeyBackfillReady(ctx, beginner); err != nil {
		return err
	}
	log.Printf("event_name=package_manifest_consumption_keys.backfill.completed marker=%q", PackageManifestConsumptionKeyBackfillMarker)
	return nil
}

func packageManifestConsumptionKeyBackfillMarkerPresent(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, packageManifestConsumptionKeyBackfillMarkerPresentSQL, PackageManifestConsumptionKeyBackfillMarker)
	if err != nil {
		return false, fmt.Errorf("check package manifest consumption key marker: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var present bool
	if err := rows.Scan(&present); err != nil {
		return false, fmt.Errorf("scan package manifest consumption key marker: %w", err)
	}
	return present, rows.Err()
}

func loadPackageManifestConsumptionKeyBackfillCursor(ctx context.Context, queryer db.Queryer) (string, error) {
	rows, err := queryer.QueryContext(ctx, loadPackageManifestConsumptionKeyBackfillCursorSQL, PackageManifestConsumptionKeyBackfillMarker)
	if err != nil {
		return "", fmt.Errorf("load package manifest consumption key backfill cursor: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", rows.Err()
	}
	var cursor string
	if err := rows.Scan(&cursor); err != nil {
		return "", fmt.Errorf("scan package manifest consumption key backfill cursor: %w", err)
	}
	return cursor, rows.Err()
}

func savePackageManifestConsumptionKeyBackfillCursor(ctx context.Context, database db.Executor, scopeID string) error {
	if _, err := database.ExecContext(ctx, savePackageManifestConsumptionKeyBackfillCursorSQL, PackageManifestConsumptionKeyBackfillMarker, scopeID, time.Now().UTC()); err != nil {
		return fmt.Errorf("save package manifest consumption key backfill cursor: %w", err)
	}
	return nil
}

func markPackageManifestConsumptionKeyBackfillReady(ctx context.Context, beginner db.Beginner) error {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin package manifest consumption key marker transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	dirty, err := packageManifestConsumptionKeysDirty(ctx, tx)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("package manifest consumption key backfill remains dirty before readiness marker")
	}
	if _, err := tx.ExecContext(ctx, markPackageManifestConsumptionKeyBackfillSQL, PackageManifestConsumptionKeyBackfillMarker, time.Now().UTC()); err != nil {
		return fmt.Errorf("mark package manifest consumption key backfill: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit package manifest consumption key marker: %w", err)
	}
	committed = true
	return nil
}

func repairPackageManifestConsumptionKeyDirtyScopes(ctx context.Context, beginner db.Beginner, database db.Queryer, limit int) error {
	for repaired := 0; repaired < limit; repaired++ {
		scopeID, found, err := nextPackageManifestConsumptionKeyDirtyScope(ctx, database)
		if err != nil || !found {
			return err
		}
		if err := rebuildPackageManifestConsumptionKeyScope(ctx, beginner, scopeID, true); err != nil {
			return err
		}
		log.Printf("event_name=package_manifest_consumption_keys.backfill.progress phase=dirty_repair scope_id=%q", scopeID)
	}
	return nil
}

func packageManifestConsumptionKeysDirty(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, packageManifestConsumptionKeysDirtySQL)
	if err != nil {
		return false, fmt.Errorf("check package manifest consumption key dirty scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var dirty bool
	if err := rows.Scan(&dirty); err != nil {
		return false, fmt.Errorf("scan package manifest consumption key dirty scopes: %w", err)
	}
	return dirty, rows.Err()
}

func nextPackageManifestConsumptionKeyDirtyScope(ctx context.Context, queryer db.Queryer) (string, bool, error) {
	rows, err := queryer.QueryContext(ctx, listPackageManifestConsumptionKeyDirtyScopesSQL)
	if err != nil {
		return "", false, fmt.Errorf("list package manifest consumption key dirty scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return "", false, rows.Err()
	}
	var scopeID string
	if err := rows.Scan(&scopeID); err != nil {
		return "", false, fmt.Errorf("scan package manifest consumption key dirty scope: %w", err)
	}
	return scopeID, true, rows.Err()
}

func rebuildPackageManifestConsumptionKeyScope(ctx context.Context, beginner db.Beginner, scopeID string, requireDirty bool) error {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin package manifest consumption key scope rebuild: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	lockedScope, err := lockPackageManifestConsumptionKeyScope(ctx, tx, scopeID)
	if err != nil {
		return err
	}
	if !lockedScope {
		if _, err := tx.ExecContext(ctx, deletePackageManifestConsumptionKeyDirtyScopeSQL, scopeID); err != nil {
			return fmt.Errorf("clear disappeared package manifest consumption key scope: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit disappeared package manifest consumption key scope: %w", err)
		}
		committed = true
		return nil
	}
	lockedDirty, err := nextPackageManifestConsumptionKeyDirtyScopeForUpdate(ctx, tx, scopeID)
	if err != nil {
		return err
	}
	if requireDirty && !lockedDirty {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit skipped package manifest consumption key dirty repair: %w", err)
		}
		committed = true
		return nil
	}
	if _, err := tx.ExecContext(ctx, setPackageManifestConsumptionKeysWriterSQL); err != nil {
		return fmt.Errorf("mark package manifest consumption key repair writer: %w", err)
	}
	if _, err := tx.ExecContext(ctx, deletePackageManifestConsumptionKeyScopeSQL, scopeID); err != nil {
		return fmt.Errorf("clear package manifest consumption key scope: %w", err)
	}
	if _, err := tx.ExecContext(ctx, deletePackageRegistryIdentityKeyScopeSQL, scopeID); err != nil {
		return fmt.Errorf("clear package registry identity key scope: %w", err)
	}
	lastFactID := ""
	for {
		envelopes, err := loadPackageManifestConsumptionKeyFacts(ctx, tx, scopeID, lastFactID)
		if err != nil {
			return err
		}
		if len(envelopes) == 0 {
			break
		}
		if err := refreshPackageManifestConsumptionKeys(ctx, tx, envelopes); err != nil {
			return err
		}
		if err := refreshPackageRegistryIdentityKeys(ctx, tx, envelopes); err != nil {
			return err
		}
		lastFactID = envelopes[len(envelopes)-1].FactID
	}
	if lockedDirty {
		if _, err := tx.ExecContext(ctx, deletePackageManifestConsumptionKeyDirtyScopeSQL, scopeID); err != nil {
			return fmt.Errorf("clear package manifest consumption key dirty scope: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit package manifest consumption key scope rebuild: %w", err)
	}
	committed = true
	return nil
}

func loadPackageManifestConsumptionKeyFacts(ctx context.Context, queryer db.Queryer, scopeID, afterFactID string) ([]facts.Envelope, error) {
	rows, err := queryer.QueryContext(ctx, listPackageManifestConsumptionKeyScopeFactsSQL, scopeID, afterFactID, packageManifestConsumptionKeyBackfillBatchSize)
	if err != nil {
		return nil, fmt.Errorf("load package manifest consumption key facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	loaded := make([]facts.Envelope, 0, packageManifestConsumptionKeyBackfillBatchSize)
	for rows.Next() {
		envelope, err := scanFactEnvelope(rows)
		if err != nil {
			return nil, fmt.Errorf("scan package manifest consumption key fact: %w", err)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load package manifest consumption key facts: %w", err)
	}
	return loaded, nil
}

func listPackageManifestConsumptionKeyScopes(ctx context.Context, queryer db.Queryer, cursor string, limit int) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, listPackageManifestConsumptionKeyScopesSQL, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list package manifest consumption key scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan package manifest consumption key scope: %w", err)
		}
		scopes = append(scopes, scopeID)
	}
	return scopes, rows.Err()
}

func lockPackageManifestConsumptionKeyScope(ctx context.Context, queryer db.Queryer, scopeID string) (bool, error) {
	rows, err := queryer.QueryContext(ctx, lockPackageManifestConsumptionKeyScopeSQL, scopeID)
	if err != nil {
		return false, fmt.Errorf("lock package manifest consumption key scope: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var locked string
	if err := rows.Scan(&locked); err != nil {
		return false, fmt.Errorf("scan locked package manifest consumption key scope: %w", err)
	}
	return true, nil
}

func nextPackageManifestConsumptionKeyDirtyScopeForUpdate(ctx context.Context, queryer db.Queryer, scopeID string) (bool, error) {
	rows, err := queryer.QueryContext(ctx, lockPackageManifestConsumptionKeyDirtyScopeSQL, scopeID)
	if err != nil {
		return false, fmt.Errorf("lock package manifest consumption key dirty scope: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var lockedScopeID string
	if err := rows.Scan(&lockedScopeID); err != nil {
		return false, fmt.Errorf("scan locked package manifest consumption key dirty scope: %w", err)
	}
	return true, nil
}

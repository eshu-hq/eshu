// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/consumption"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

const listPackageManifestConsumptionKeyDirtyScopesSQL = `
SELECT scope_id
FROM package_manifest_consumption_key_dirty_scopes
ORDER BY marked_at, scope_id
LIMIT 1
`

const packageManifestConsumptionKeysDirtySQL = `
SELECT EXISTS (SELECT 1 FROM package_manifest_consumption_key_dirty_scopes)
`

// listPackageManifestConsumptionKeyScopeFactsSQL reads one fact page for a
// scope rebuild. Migration 181's partial index
// fact_records_package_manifest_consumption_backfill_idx is built on
// (scope_id, fact_id) with exactly this fact predicate, so every page is one
// ordered index range scan with no sort, whatever the cursor (#7663).
// TestPackageManifestConsumptionBackfillIndexPredicateMatchesPageSQL derives
// the index predicate from this text; change both together.
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

// PackageManifestConsumptionKeyBackfill runs package-consumption sidecar
// backfill passes. Instruments is optional; when set, every scope rebuild
// reports its transactions, scope-row lock waits, and outcome (#7663).
type PackageManifestConsumptionKeyBackfill struct {
	Instruments *telemetry.Instruments
}

// BackfillPackageManifestConsumptionKeys performs one bounded, resumable pass
// without telemetry. See PackageManifestConsumptionKeyBackfill.Run.
func BackfillPackageManifestConsumptionKeys(ctx context.Context, database db.ExecQueryer) error {
	return PackageManifestConsumptionKeyBackfill{}.Run(ctx, database)
}

// Run performs one bounded, resumable pass, including the sbt key repair when
// it applies. See RunWithReport.
func (b PackageManifestConsumptionKeyBackfill) Run(ctx context.Context, database db.ExecQueryer) error {
	_, err := b.RunWithReport(ctx, database)
	return err
}

// RunWithReport performs one bounded, resumable pass and reports the sbt key
// repair's progress for the caller's pass log and poll interval. It runs the
// v2 backfill first, then the repair, which starts only once the v2 marker
// exists; dirty rows do not hold it back, because the repair is additive and
// never touches the dirty set (#8961).
func (b PackageManifestConsumptionKeyBackfill) RunWithReport(ctx context.Context, database db.ExecQueryer) (consumption.SBTRepairReport, error) {
	if err := b.runKeyBackfill(ctx, database); err != nil {
		return consumption.SBTRepairReport{}, err
	}
	beginner, ok := database.(db.Beginner)
	if !ok {
		return consumption.SBTRepairReport{}, fmt.Errorf("package manifest consumption key backfill requires a transaction beginner")
	}
	present, err := packageManifestConsumptionKeyBackfillMarkerPresent(ctx, database)
	if err != nil || !present {
		return consumption.SBTRepairReport{}, err
	}
	report, err := b.sbtRepair(beginner).Pass(ctx, database)
	logPackageManifestSBTRepairPass(report, err)
	return report, err
}

// runKeyBackfill performs the v2 pass. It records the v2 readiness marker only
// after both sidecars are current and the old-writer dirty fence is empty.
// Each scope is rebuilt in one short transaction per fact page, so no
// transaction holds a scope row for longer than one page.
func (b PackageManifestConsumptionKeyBackfill) runKeyBackfill(ctx context.Context, database db.ExecQueryer) error {
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
		return b.repairDirtyScopes(ctx, beginner, database, packageManifestConsumptionKeyBackfillScopesPerPass)
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
		result, err := b.rebuildScope(ctx, beginner, scopeID, consumption.PhaseInitial)
		if err != nil {
			return err
		}
		if err := savePackageManifestConsumptionKeyBackfillCursor(ctx, database, scopeID); err != nil {
			return err
		}
		logPackageManifestConsumptionScopeRebuild(result, nil)
	}
	if len(scopes) == packageManifestConsumptionKeyBackfillScopesPerPass {
		return nil
	}
	if err := b.repairDirtyScopes(ctx, beginner, database, packageManifestConsumptionKeyBackfillScopesPerPass); err != nil {
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

func (b PackageManifestConsumptionKeyBackfill) repairDirtyScopes(ctx context.Context, beginner db.Beginner, database db.Queryer, limit int) error {
	for repaired := 0; repaired < limit; repaired++ {
		scopeID, found, err := nextPackageManifestConsumptionKeyDirtyScope(ctx, database)
		if err != nil || !found {
			return err
		}
		result, err := b.rebuildScope(ctx, beginner, scopeID, consumption.PhaseDirtyRepair)
		if err != nil {
			return err
		}
		logPackageManifestConsumptionScopeRebuild(result, nil)
	}
	return nil
}

// rebuildScope rebuilds one scope's sidecars through the paged rebuilder and
// logs the failure line when it does not finish.
func (b PackageManifestConsumptionKeyBackfill) rebuildScope(ctx context.Context, beginner db.Beginner, scopeID string, phase consumption.Phase) (consumption.Result, error) {
	result, err := b.rebuilder(beginner).RebuildScope(ctx, scopeID, phase)
	if err != nil {
		logPackageManifestConsumptionScopeRebuild(result, err)
	}
	return result, err
}

// rebuilder wires the paged rebuilder to this package's page loader and
// refresh. PageSize is the same constant the page query binds as its LIMIT: the
// rebuilder ends a scope on a page shorter than PageSize, so the two must stay
// equal (TestPackageManifestConsumptionPageLimitEqualsRebuilderPageSize).
func (b PackageManifestConsumptionKeyBackfill) rebuilder(beginner db.Beginner) consumption.Rebuilder {
	return consumption.Rebuilder{
		Beginner:    beginner,
		LoadPage:    loadPackageManifestConsumptionKeyFacts,
		Refresh:     refreshPackageConsumptionSidecars,
		PageSize:    packageManifestConsumptionKeyBackfillBatchSize,
		LockTimeout: consumption.DefaultLockTimeout,
		Instruments: b.Instruments,
	}
}

// refreshPackageConsumptionSidecars rewrites both sidecars for one fact page.
func refreshPackageConsumptionSidecars(ctx context.Context, tx db.ExecQueryer, envelopes []facts.Envelope) error {
	if err := refreshPackageManifestConsumptionKeys(ctx, tx, envelopes); err != nil {
		return err
	}
	return refreshPackageRegistryIdentityKeys(ctx, tx, envelopes)
}

// logPackageManifestConsumptionScopeRebuild writes one progress line per
// finished scope, or one scope_failed line, with the page, fact, transaction,
// and scope-row lock-wait numbers an operator needs to spot a slow or blocked
// scope.
func logPackageManifestConsumptionScopeRebuild(result consumption.Result, err error) {
	event := "progress"
	if err != nil {
		event = "scope_failed"
	}
	line := fmt.Sprintf(
		"event_name=package_manifest_consumption_keys.backfill.%s phase=%s scope_id=%q outcome=%s pages=%d facts=%d transactions=%d max_transaction_ms=%d lock_wait_ms=%d max_lock_wait_ms=%d duration_ms=%d",
		event, result.Phase, result.ScopeID, result.Outcome, result.Pages, result.Facts, result.Transactions,
		result.MaxTransaction.Milliseconds(), result.LockWait.Milliseconds(), result.MaxLockWait.Milliseconds(), result.Duration.Milliseconds(),
	)
	if err != nil {
		line += fmt.Sprintf(" error=%q", err.Error())
	}
	log.Print(line)
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

func loadPackageManifestConsumptionKeyFacts(ctx context.Context, queryer db.Queryer, scopeID, afterFactID string) ([]facts.Envelope, error) {
	return loadPackageManifestConsumptionKeyFactsWith(ctx, queryer, listPackageManifestConsumptionKeyScopeFactsSQL, scopeID, afterFactID)
}

func loadPackageManifestConsumptionKeyFactsWith(ctx context.Context, queryer db.Queryer, query, scopeID, afterFactID string) ([]facts.Envelope, error) {
	rows, err := queryer.QueryContext(ctx, query, scopeID, afterFactID, packageManifestConsumptionKeyBackfillBatchSize)
	if err != nil {
		return nil, fmt.Errorf("load package manifest consumption key facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	loaded := make([]facts.Envelope, 0, packageManifestConsumptionKeyBackfillBatchSize)
	for rows.Next() {
		envelope, err := backfill.ScanFactEnvelope(rows)
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

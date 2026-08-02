// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const containerImageIdentityV3MigrationRows = 10_000

type containerImageIdentityV3MigrationCatalog struct {
	supportSetTableOID int64
	supportTableOID    int64
	stateTableOID      int64
	viewOID            int64
	constraintOID      int64
	resetTriggerOID    int64
	rejectTriggerOID   int64
	activationEpoch    int64
}

func TestContainerImageIdentityV3MigrationPopulatedUpgradeLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the populated digest-v3 migration proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	schema := fmt.Sprintf("eshu_5740_v3_upgrade_%d", time.Now().UnixNano())
	adminDB := openActiveOCIWarningIndexProofDB(t, dsn)
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated digest-v3 schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop isolated digest-v3 schema: %v", err)
		}
	})
	db := openActiveOCIWarningIndexProofDB(t, activeOCIWarningIndexSchemaDSN(t, dsn, schema))
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	exec := SQLDB{DB: db}
	preUpgrade, upgrade := containerImageIdentityV3UpgradeDefinitions(t)
	if err := ApplyDefinitions(ctx, exec, preUpgrade); err != nil {
		t.Fatalf("apply pre-092 bootstrap definitions: %v", err)
	}
	seedContainerImageIdentityV3MigrationRows(t, ctx, db)

	proveContainerImageIdentityV3MigrationLockTimeout(t, ctx, exec, upgrade, db)
	firstDuration := applyContainerImageIdentityV3MigrationWithConcurrentScope(
		t, ctx, exec, upgrade, db,
	)
	if firstDuration > 10*time.Second {
		t.Fatalf("populated first digest-v3 migration = %s, want <= 10s", firstDuration)
	}
	assertContainerImageIdentityV3MigrationRows(t, ctx, db, containerImageIdentityV3MigrationRows)
	first := readContainerImageIdentityV3MigrationCatalog(t, ctx, db)

	repeatDuration := repeatContainerImageIdentityV3MigrationWithWriters(
		t, ctx, exec, upgrade, db,
	)
	if repeatDuration > 2*time.Second {
		t.Fatalf("populated repeated digest-v3 migration = %s, want <= 2s", repeatDuration)
	}
	second := readContainerImageIdentityV3MigrationCatalog(t, ctx, db)
	if second != first {
		t.Fatalf("repeated migration changed catalog/state: first=%+v second=%+v", first, second)
	}
	assertContainerImageIdentityV3MigrationRows(t, ctx, db, containerImageIdentityV3MigrationRows)
	proveContainerImageIdentityV3FirstHeldPublication(t, ctx, db, first.activationEpoch)
	t.Logf("digest-v3 populated migration rows=%d first=%s repeat=%s", containerImageIdentityV3MigrationRows, firstDuration, repeatDuration)
}

func containerImageIdentityV3UpgradeDefinitions(t *testing.T) ([]Definition, []Definition) {
	t.Helper()
	upgradeNames := map[string]struct{}{
		"container_image_identity_support_store":                  {},
		"container_image_identity_support_current_view":           {},
		"container_image_identity_current_facts_function":         {},
		"container_image_identity_current_support_facts_function": {},
	}
	var preUpgrade, upgrade []Definition
	for _, definition := range BootstrapDefinitions() {
		if _, ok := upgradeNames[definition.Name]; ok {
			upgrade = append(upgrade, definition)
		} else {
			preUpgrade = append(preUpgrade, definition)
		}
	}
	if len(upgrade) != len(upgradeNames) {
		t.Fatalf("digest-v3 upgrade definitions = %d, want %d", len(upgrade), len(upgradeNames))
	}
	return preUpgrade, upgrade
}

func proveContainerImageIdentityV3MigrationLockTimeout(
	t *testing.T, ctx context.Context, exec SQLDB, upgrade []Definition, db *sql.DB,
) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin digest-v3 migration blocker: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fact_work_items SET updated_at = clock_timestamp() WHERE work_item_id = 'intent:5740-v3-upgrade:1'`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("hold fact_work_items writer lock: %v", err)
	}
	lockErr := ApplyDefinitionsWithLockTimeout(ctx, exec, upgrade, 100*time.Millisecond)
	if lockErr == nil || !strings.Contains(strings.ToLower(lockErr.Error()), "lock timeout") {
		_ = tx.Rollback()
		t.Fatalf("digest-v3 migration under active writer = %v, want bounded lock timeout", lockErr)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("release digest-v3 migration blocker: %v", err)
	}
	var supportStoreExists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass(format('%I.container_image_identity_support_sets', current_schema())) IS NOT NULL`).Scan(&supportStoreExists); err != nil {
		t.Fatalf("inspect rolled-back support store: %v", err)
	}
	if supportStoreExists {
		t.Fatalf("lock-timed-out digest-v3 migration left a partial support store: %v", lockErr)
	}
}

func applyContainerImageIdentityV3MigrationWithConcurrentScope(
	t *testing.T, ctx context.Context, exec SQLDB, upgrade []Definition, db *sql.DB,
) time.Duration {
	t.Helper()
	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin concurrent scope blocker: %v", err)
	}
	if _, err := blocker.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    'repository:5740-v3-before-lock', 'repository', 'git', 'synthetic-5740-v3-before-lock',
    'git', 'synthetic-5740-v3-before-lock', clock_timestamp(), clock_timestamp(), 'active'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES (
    'generation:5740-v3-before-lock', 'repository:5740-v3-before-lock', 'synthetic',
    clock_timestamp(), clock_timestamp(), 'active'
);
UPDATE ingestion_scopes
SET active_generation_id = 'generation:5740-v3-before-lock'
WHERE scope_id = 'repository:5740-v3-before-lock';
`); err != nil {
		_ = blocker.Rollback()
		t.Fatalf("seed pre-lock concurrent scope: %v", err)
	}
	errCh := make(chan error, 1)
	start := time.Now()
	go func() { errCh <- ApplyDefinitionsWithLockTimeout(ctx, exec, upgrade, 5*time.Second) }()
	waitForContainerImageIdentityV3MigrationLock(t, ctx, db, 1)
	insertErrCh := make(chan error, 1)
	go func() {
		_, insertErr := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    'repository:5740-v3-concurrent', 'repository', 'git', 'synthetic-5740-v3-concurrent',
    'git', 'synthetic-5740-v3-concurrent', clock_timestamp(), clock_timestamp(), 'active'
)`)
		insertErrCh <- insertErr
	}()
	waitForContainerImageIdentityV3MigrationLock(t, ctx, db, 2)
	if err := blocker.Commit(); err != nil {
		t.Fatalf("commit pre-lock concurrent scope: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("apply populated digest-v3 migration: %v", err)
	}
	if err := <-insertErrCh; err != nil {
		t.Fatalf("insert concurrent migration scope: %v", err)
	}
	duration := time.Since(start)
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES (
    'generation:5740-v3-concurrent', 'repository:5740-v3-concurrent', 'synthetic',
    clock_timestamp(), clock_timestamp(), 'active'
);
UPDATE ingestion_scopes
SET active_generation_id = 'generation:5740-v3-concurrent'
WHERE scope_id = 'repository:5740-v3-concurrent';
`); err != nil {
		t.Fatalf("activate post-trigger concurrent scope: %v", err)
	}
	var stateRows, positiveEpochs int
	if err := db.QueryRowContext(ctx, `
SELECT count(*), count(*) FILTER (WHERE activation_epoch > 0)
FROM container_image_identity_scope_state
WHERE scope_id IN ('repository:5740-v3-before-lock', 'repository:5740-v3-concurrent')
`).Scan(&stateRows, &positiveEpochs); err != nil {
		t.Fatalf("count concurrent scope state: %v", err)
	}
	if stateRows != 2 || positiveEpochs != 2 {
		t.Fatalf("pre/post-lock concurrent scope state/epochs = %d/%d, want 2/2", stateRows, positiveEpochs)
	}
	return duration
}

func waitForContainerImageIdentityV3MigrationLock(
	t *testing.T, ctx context.Context, db *sql.DB, wantWaiting int,
) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := db.QueryRowContext(waitCtx, `
SELECT count(*)
FROM pg_locks
WHERE relation = to_regclass(format('%I.ingestion_scopes', current_schema()))
  AND NOT granted
`).Scan(&waiting); err != nil {
			t.Fatalf("inspect migration lock count: %v", err)
		}
		if waiting >= wantWaiting {
			return
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf("wait for %d migration locks: %v", wantWaiting, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func repeatContainerImageIdentityV3MigrationWithWriters(
	t *testing.T, ctx context.Context, exec SQLDB, upgrade []Definition, db *sql.DB,
) time.Duration {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin repeated migration writers: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE fact_work_items
SET updated_at = clock_timestamp()
WHERE work_item_id = 'intent:5740-v3-upgrade:2';
UPDATE ingestion_scopes
SET observed_at = observed_at
WHERE scope_id = 'repository:5740-v3-upgrade';
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES (
    'content:5740-v3-upgrade:uncommitted',
    'repository:5740-v3-upgrade', 'generation:5740-v3-upgrade',
    'content_entity', 'content:5740-v3-upgrade:uncommitted',
    'reducer', 'git', 'intent:5740-v3-upgrade:2', clock_timestamp(), clock_timestamp(),
    '{"entity_name":"SyntheticUncommitted"}'::jsonb
);`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("hold repeated migration writers: %v", err)
	}
	start := time.Now()
	if err := ApplyDefinitionsWithLockTimeout(ctx, exec, upgrade, time.Second); err != nil {
		_ = tx.Rollback()
		t.Fatalf("repeat digest-v3 migration with active writers: %v", err)
	}
	duration := time.Since(start)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("release repeated migration writers: %v", err)
	}
	return duration
}

func assertContainerImageIdentityV3MigrationRows(
	t *testing.T, ctx context.Context, db *sql.DB, want int,
) {
	t.Helper()
	var legacyRows, workItems, supportSets, supports, visibleRows int
	if err := db.QueryRowContext(ctx, `
SELECT
    (SELECT count(*) FROM fact_records
     WHERE scope_id = 'repository:5740-v3-upgrade'
       AND generation_id = 'generation:5740-v3-upgrade'
       AND fact_kind = 'reducer_container_image_identity'),
    (SELECT count(*) FROM fact_work_items
     WHERE scope_id = 'repository:5740-v3-upgrade'
       AND generation_id = 'generation:5740-v3-upgrade'),
    (SELECT count(*) FROM container_image_identity_support_sets),
    (SELECT count(*) FROM container_image_identity_supports),
    (SELECT count(*) FROM container_image_identity_current_supports
     WHERE scope_id = 'repository:5740-v3-upgrade')
`).Scan(&legacyRows, &workItems, &supportSets, &supports, &visibleRows); err != nil {
		t.Fatalf("read populated digest-v3 migration rows: %v", err)
	}
	if legacyRows != want || workItems != want || visibleRows != want {
		t.Fatalf("legacy/work/visible rows = %d/%d/%d, want %d/%d/%d", legacyRows, workItems, visibleRows, want, want, want)
	}
	if supportSets != 0 || supports != 0 {
		t.Fatalf("migration eagerly created typed sets/supports = %d/%d, want 0/0", supportSets, supports)
	}
	var activeSetIsNull, constraintValidated bool
	if err := db.QueryRowContext(ctx, `
SELECT
    (SELECT active_set_id IS NULL
     FROM container_image_identity_scope_state
     WHERE scope_id = 'repository:5740-v3-upgrade'),
    (SELECT convalidated
     FROM pg_constraint
     WHERE conrelid = 'fact_work_items'::regclass
       AND conname = 'fact_work_items_container_image_identity_v3_status_check')
`).Scan(&activeSetIsNull, &constraintValidated); err != nil {
		t.Fatalf("read digest-v3 authority/constraint state: %v", err)
	}
	if !activeSetIsNull || !constraintValidated {
		t.Fatalf("active_set_is_null/constraint_validated = %t/%t, want true/true", activeSetIsNull, constraintValidated)
	}
}

func readContainerImageIdentityV3MigrationCatalog(
	t *testing.T, ctx context.Context, db *sql.DB,
) containerImageIdentityV3MigrationCatalog {
	t.Helper()
	var state containerImageIdentityV3MigrationCatalog
	if err := db.QueryRowContext(ctx, `
SELECT
    'container_image_identity_support_sets'::regclass::oid::bigint,
    'container_image_identity_supports'::regclass::oid::bigint,
    'container_image_identity_scope_state'::regclass::oid::bigint,
    'container_image_identity_current_supports'::regclass::oid::bigint,
    (SELECT oid::bigint FROM pg_constraint
     WHERE conrelid = 'fact_work_items'::regclass
       AND conname = 'fact_work_items_container_image_identity_v3_status_check'),
    (SELECT oid::bigint FROM pg_trigger
     WHERE tgrelid = 'ingestion_scopes'::regclass
       AND tgname = 'ingestion_scopes_container_image_identity_state_reset'
       AND NOT tgisinternal),
    (SELECT oid::bigint FROM pg_trigger
     WHERE tgrelid = 'fact_records'::regclass
       AND tgname = 'fact_records_reject_container_image_identity_v2'
       AND NOT tgisinternal),
    (SELECT activation_epoch FROM container_image_identity_scope_state
     WHERE scope_id = 'repository:5740-v3-upgrade')
`).Scan(
		&state.supportSetTableOID,
		&state.supportTableOID,
		&state.stateTableOID,
		&state.viewOID,
		&state.constraintOID,
		&state.resetTriggerOID,
		&state.rejectTriggerOID,
		&state.activationEpoch,
	); err != nil {
		t.Fatalf("read digest-v3 migration catalog: %v", err)
	}
	return state
}

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

type containerImageIdentityCutoverCatalogState struct {
	tableOID             int64
	factFunctionOID      int64
	markerFunctionOID    int64
	factUpdateTriggerOID int64
	factInsertTriggerOID int64
	markerTriggerOID     int64
	ackConstraintOID     int64
	requiredColumnNum    int16
	authorizedColumnNum  int16
}

func TestContainerImageIdentityCutoverMigrationLifecycleLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the identity cutover migration proof")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	schema := fmt.Sprintf("eshu_5854_identity_cutover_%d", time.Now().UnixNano())
	adminDB := openActiveOCIWarningIndexProofDB(t, dsn)
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated identity-cutover schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE",
		); err != nil {
			t.Errorf("drop isolated identity-cutover schema: %v", err)
		}
	})

	schemaDB := openActiveOCIWarningIndexProofDB(
		t,
		activeOCIWarningIndexSchemaDSN(t, dsn, schema),
	)
	exec := SQLDB{DB: schemaDB}
	preUpgrade, migration := containerImageIdentityCutoverUpgradeDefinitions(t)
	if err := ApplyDefinitions(ctx, exec, preUpgrade); err != nil {
		t.Fatalf("apply pre-088 bootstrap definitions: %v", err)
	}
	seedContainerImageIdentityCutoverMigrationProof(t, ctx, schemaDB)
	rowsBefore := countContainerImageIdentityCutoverMigrationRows(t, ctx, schemaDB)

	blockingTx, err := schemaDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin migration writer blocker: %v", err)
	}
	if _, err := blockingTx.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    'cutover-migration-blocking-writer',
    'repository:5854-cutover-migration',
    'generation:5854-cutover-migration',
    'content_entity',
    'cutover-migration-blocking-writer',
    'git',
    'git',
    'cutover-migration-blocking-writer',
    '2026-07-29T22:00:01Z',
    '2026-07-29T22:00:01Z',
    '{"entity_name":"SyntheticBlockingWriter"}'
)
`); err != nil {
		_ = blockingTx.Rollback()
		t.Fatalf("hold populated fact_records writer lock: %v", err)
	}
	lockErr := ApplyDefinitionsWithLockTimeout(
		ctx,
		exec,
		[]Definition{migration},
		100*time.Millisecond,
	)
	if lockErr == nil || !strings.Contains(strings.ToLower(lockErr.Error()), "lock timeout") {
		_ = blockingTx.Rollback()
		t.Fatalf("migration 088 under active writer = %v, want bounded lock timeout", lockErr)
	}
	assertContainerImageIdentityCutoverObjectsAbsent(t, ctx, schemaDB)
	if err := blockingTx.Rollback(); err != nil {
		t.Fatalf("release migration writer blocker: %v", err)
	}
	proveContainerImageIdentityCutoverWorkItemLockRollback(
		t,
		ctx,
		exec,
		migration,
		schemaDB,
	)

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("retry migration 088 through ApplyBootstrap(): %v", err)
	}
	first := readContainerImageIdentityCutoverCatalogState(t, ctx, schemaDB)
	if got := countContainerImageIdentityCutoverMigrationRows(t, ctx, schemaDB); got != rowsBefore {
		t.Fatalf("migration 088 fact rows = %d, want %d", got, rowsBefore)
	}

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("first repeated ApplyBootstrap(): %v", err)
	}
	second := readContainerImageIdentityCutoverCatalogState(t, ctx, schemaDB)
	if second != first {
		t.Fatalf("first repeated bootstrap changed cutover objects: first=%+v second=%+v", first, second)
	}
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("second repeated ApplyBootstrap(): %v", err)
	}
	third := readContainerImageIdentityCutoverCatalogState(t, ctx, schemaDB)
	if third != second {
		t.Fatalf("second repeated bootstrap changed cutover objects: second=%+v third=%+v", second, third)
	}

	proveContainerImageIdentityCutoverMigrationBehavior(t, ctx, schemaDB)
	proveContainerImageIdentityCutoverMigrationRerunStates(
		t,
		ctx,
		exec,
		schemaDB,
	)
}

func assertContainerImageIdentityCutoverObjectsAbsent(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	var (
		tableExists             bool
		factFunctionExists      bool
		markerFunctionExists    bool
		factUpdateTriggerExists bool
		factInsertTriggerExists bool
		markerTriggerExists     bool
		ackConstraintExists     bool
		requiredColumnExists    bool
		authorizedColumnExists  bool
	)
	if err := db.QueryRowContext(ctx, `
SELECT
    to_regclass(
        format('%I.container_image_identity_cutovers', current_schema())
    ) IS NOT NULL,
    to_regprocedure(
        format(
            '%I.guard_legacy_container_image_identity_statement()',
            current_schema()
        )
    ) IS NOT NULL,
    to_regprocedure(
        format(
            '%I.guard_container_image_identity_cutover_marker()',
            current_schema()
        )
    ) IS NOT NULL,
    EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = to_regclass(
                format('%I.fact_records', current_schema())
              )
          AND tgname =
              'fact_records_legacy_container_image_identity_cutover_guard_update_statement'
          AND NOT tgisinternal
    ),
    EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = to_regclass(
                format('%I.fact_records', current_schema())
              )
          AND tgname =
              'fact_records_legacy_container_image_identity_cutover_guard_insert_statement'
          AND NOT tgisinternal
    ),
    EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = to_regclass(
                format('%I.container_image_identity_cutovers', current_schema())
              )
          AND tgname = 'container_image_identity_cutover_marker_guard'
          AND NOT tgisinternal
    ),
    EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = to_regclass(
                format('%I.fact_work_items', current_schema())
              )
          AND conname =
              'fact_work_items_container_image_identity_v2_status_check'
    ),
    EXISTS (
        SELECT 1
        FROM pg_attribute
        WHERE attrelid = to_regclass(
                format('%I.fact_work_items', current_schema())
              )
          AND attname = 'container_image_identity_v2_required'
          AND NOT attisdropped
    ),
    EXISTS (
        SELECT 1
        FROM pg_attribute
        WHERE attrelid = to_regclass(
                format('%I.fact_work_items', current_schema())
              )
          AND attname = 'container_image_identity_v2_authorized_status'
          AND NOT attisdropped
    )
`).Scan(
		&tableExists,
		&factFunctionExists,
		&markerFunctionExists,
		&factUpdateTriggerExists,
		&factInsertTriggerExists,
		&markerTriggerExists,
		&ackConstraintExists,
		&requiredColumnExists,
		&authorizedColumnExists,
	); err != nil {
		t.Fatalf("read migration 088 objects after lock timeout: %v", err)
	}
	if tableExists ||
		factFunctionExists ||
		markerFunctionExists ||
		factUpdateTriggerExists ||
		factInsertTriggerExists ||
		markerTriggerExists ||
		ackConstraintExists ||
		requiredColumnExists ||
		authorizedColumnExists {
		t.Fatalf(
			"migration 088 partial objects after lock timeout = table %t fact_function %t marker_function %t fact_update_trigger %t fact_insert_trigger %t marker_trigger %t status_constraint %t required_column %t authorized_column %t, want all false",
			tableExists,
			factFunctionExists,
			markerFunctionExists,
			factUpdateTriggerExists,
			factInsertTriggerExists,
			markerTriggerExists,
			ackConstraintExists,
			requiredColumnExists,
			authorizedColumnExists,
		)
	}
}

func containerImageIdentityCutoverUpgradeDefinitions(
	t *testing.T,
) ([]Definition, Definition) {
	t.Helper()
	definitions := BootstrapDefinitions()
	preUpgrade := make([]Definition, 0, len(definitions)-1)
	var migration Definition
	for _, definition := range definitions {
		if definition.Name == "container_image_identity_cutover_guard" {
			migration = definition
			continue
		}
		preUpgrade = append(preUpgrade, definition)
	}
	if migration.Name == "" || len(preUpgrade) != len(definitions)-1 {
		t.Fatal("migration 088 missing from bootstrap definitions")
	}
	return preUpgrade, migration
}

func seedContainerImageIdentityCutoverMigrationProof(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    'repository:5854-cutover-migration', 'repository', 'git',
    'synthetic-5854-cutover-migration', 'git',
    'synthetic-5854-cutover-migration',
    '2026-07-29T22:00:00Z', '2026-07-29T22:00:00Z', 'active'
)`); err != nil {
		t.Fatalf("seed identity-cutover scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES (
    'generation:5854-cutover-migration',
    'repository:5854-cutover-migration',
    'synthetic', '2026-07-29T22:00:00Z', '2026-07-29T22:00:00Z', 'active'
)`); err != nil {
		t.Fatalf("seed identity-cutover generation: %v", err)
	}
	seedContainerImageIdentityAckWorkItem(
		t,
		ctx,
		db,
		"container-image-identity-5854-cutover-migration",
		"repository:5854-cutover-migration",
		"generation:5854-cutover-migration",
		"reducer-5854-cutover-migration",
		time.Date(2026, time.July, 29, 22, 10, 0, 0, time.UTC),
		time.Date(2026, time.July, 29, 22, 0, 0, 0, time.UTC),
	)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
)
SELECT
    'cutover-migration-proof:' || series,
    'repository:5854-cutover-migration',
    'generation:5854-cutover-migration',
    'content_entity',
    'cutover-migration-proof:' || series,
    'git',
    'git',
    'cutover-migration-proof:' || series,
    '2026-07-29T22:00:00Z'::timestamptz + series * interval '1 microsecond',
    '2026-07-29T22:00:00Z',
    jsonb_build_object('entity_name', 'Synthetic' || series)
FROM generate_series(1, 10000) AS series
`); err != nil {
		t.Fatalf("seed populated identity-cutover fact table: %v", err)
	}
}

func readContainerImageIdentityCutoverCatalogState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) containerImageIdentityCutoverCatalogState {
	t.Helper()
	var state containerImageIdentityCutoverCatalogState
	if err := db.QueryRowContext(ctx, `
SELECT
    'container_image_identity_cutovers'::regclass::oid::bigint,
    'guard_legacy_container_image_identity_statement()'::regprocedure::oid::bigint,
    'guard_container_image_identity_cutover_marker()'::regprocedure::oid::bigint,
    (
        SELECT oid::bigint
        FROM pg_trigger
        WHERE tgrelid = 'fact_records'::regclass
          AND tgname =
              'fact_records_legacy_container_image_identity_cutover_guard_update_statement'
          AND NOT tgisinternal
    ),
    (
        SELECT oid::bigint
        FROM pg_trigger
        WHERE tgrelid = 'fact_records'::regclass
          AND tgname =
              'fact_records_legacy_container_image_identity_cutover_guard_insert_statement'
          AND NOT tgisinternal
    ),
    (
        SELECT oid::bigint
        FROM pg_trigger
        WHERE tgrelid = 'container_image_identity_cutovers'::regclass
          AND tgname = 'container_image_identity_cutover_marker_guard'
          AND NOT tgisinternal
    ),
    (
        SELECT oid::bigint
        FROM pg_constraint
        WHERE conrelid = 'fact_work_items'::regclass
          AND conname =
              'fact_work_items_container_image_identity_v2_status_check'
    ),
    (
        SELECT attnum
        FROM pg_attribute
        WHERE attrelid = 'fact_work_items'::regclass
          AND attname = 'container_image_identity_v2_required'
          AND NOT attisdropped
    ),
    (
        SELECT attnum
        FROM pg_attribute
        WHERE attrelid = 'fact_work_items'::regclass
          AND attname = 'container_image_identity_v2_authorized_status'
          AND NOT attisdropped
    )
`).Scan(
		&state.tableOID,
		&state.factFunctionOID,
		&state.markerFunctionOID,
		&state.factUpdateTriggerOID,
		&state.factInsertTriggerOID,
		&state.markerTriggerOID,
		&state.ackConstraintOID,
		&state.requiredColumnNum,
		&state.authorizedColumnNum,
	); err != nil {
		t.Fatalf("read identity-cutover catalog state: %v", err)
	}
	return state
}

func countContainerImageIdentityCutoverMigrationRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM fact_records").Scan(&count); err != nil {
		t.Fatalf("count identity-cutover proof rows: %v", err)
	}
	return count
}

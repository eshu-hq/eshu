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
	tableOID   int64
	triggerOID int64
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

	if err := ApplyDefinitions(ctx, exec, []Definition{migration}); err != nil {
		t.Fatalf("apply migration 088 to populated fact table: %v", err)
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
SELECT marker.oid::bigint, trigger.oid::bigint
FROM pg_class AS marker
CROSS JOIN pg_trigger AS trigger
WHERE marker.oid = 'container_image_identity_cutovers'::regclass
  AND trigger.tgrelid = 'fact_records'::regclass
  AND trigger.tgname = 'fact_records_legacy_container_image_identity_cutover_guard'
  AND NOT trigger.tgisinternal
`).Scan(&state.tableOID, &state.triggerOID); err != nil {
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

func proveContainerImageIdentityCutoverMigrationBehavior(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	const (
		legacyFactID = "reducer_container_image_identity:5854-legacy-migration"
		newFactID    = "reducer_container_image_identity:5854-v2-migration"
		scopeID      = "repository:5854-cutover-migration"
		generationID = "generation:5854-cutover-migration"
	)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    $1, $2, $3, 'reducer_container_image_identity', 'legacy:tag',
    'reducer', 'git', 'legacy:tag',
    '2026-07-29T22:01:00Z', '2026-07-29T22:01:00Z',
    '{"image_ref":"registry.example.com/team/api:prod","outcome":"tag_resolved"}'
)`, legacyFactID, scopeID, generationID); err != nil {
		t.Fatalf("insert pre-cutover legacy row: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin identity-cutover proof transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
WITH locked AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(
        hashtextextended($1 || E'\x1f' || $2, 5854)
    )
)
INSERT INTO container_image_identity_cutovers (scope_id, generation_id)
SELECT $1, $2 FROM locked
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeID, generationID); err != nil {
		t.Fatalf("insert identity-cutover marker: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    $1, $2, $3, 'reducer_container_image_identity', 'image-ref:prod',
    'reducer', 'git', 'image-ref:prod',
    '2026-07-29T22:02:00Z', '2026-07-29T22:02:00Z',
    '{"identity_format":"image_ref_v2","image_ref":"registry.example.com/team/api:prod"}'
)
`, newFactID, scopeID, generationID); err != nil {
		t.Fatalf("insert new-format identity row: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM fact_records WHERE fact_id = $1", legacyFactID); err != nil {
		t.Fatalf("delete pre-cutover legacy row: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit identity-cutover proof transaction: %v", err)
	}

	result, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    payload
) VALUES (
    $1, $2, $3, 'reducer_container_image_identity', 'legacy:tag',
    'reducer', 'git', 'legacy:tag',
    '2026-07-29T22:03:00Z', '2026-07-29T22:03:00Z',
    '{"image_ref":"registry.example.com/team/api:prod","outcome":"tag_resolved"}'
)
`, legacyFactID, scopeID, generationID)
	if err != nil {
		t.Fatalf("attempt post-cutover legacy insert: %v", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("count post-cutover legacy insert: %v", err)
	}
	if affected != 0 {
		t.Fatalf("post-cutover legacy inserted rows = %d, want 0", affected)
	}

	var (
		legacyRows int
		newRows    int
		markers    int
	)
	if err := db.QueryRowContext(ctx, `
SELECT
    count(*) FILTER (WHERE fact_id = $1),
    count(*) FILTER (WHERE fact_id = $2),
    (SELECT count(*) FROM container_image_identity_cutovers
     WHERE scope_id = $3 AND generation_id = $4)
FROM fact_records
`, legacyFactID, newFactID, scopeID, generationID).Scan(
		&legacyRows,
		&newRows,
		&markers,
	); err != nil {
		t.Fatalf("read identity-cutover proof result: %v", err)
	}
	if legacyRows != 0 || newRows != 1 || markers != 1 {
		t.Fatalf(
			"identity-cutover rows = legacy %d new %d markers %d, want 0/1/1",
			legacyRows,
			newRows,
			markers,
		)
	}
}

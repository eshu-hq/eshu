// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const activeOCIWarningIndexName = "fact_records_active_oci_warning_idx"

type activeOCIWarningIndexState struct {
	oid        int64
	definition string
	valid      bool
	ready      bool
}

func TestActiveOCIWarningIndexMigrationLifecycleLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the active OCI warning index lifecycle proof")
	}

	preUpgrade, migration := activeOCIWarningIndexUpgradeDefinitions(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	// PreUpgrade installs the lane's public-only authority migrations, so
	// the proof bootstraps a fresh database (public schema, where the 241
	// guard passes) instead of an isolated schema, where it fails closed.
	// One handle serves every role: the separate pools existed to share one
	// database, and a fresh database is already private to this proof.
	db := openDisposablePublicBootstrapDB(t, dsn,
		os.Getenv("ESHU_POSTGRES_DISPOSABLE"),
		"set ESHU_POSTGRES_TEST_DSN to run the active OCI warning index lifecycle proof",
		func(applyCtx context.Context, database *sql.DB) error {
			return ApplyDefinitions(applyCtx, SQLDB{DB: database}, preUpgrade)
		})
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	applyDB, blockerDB, writerDB := db, db, db

	exec := SQLDB{DB: applyDB}
	// Fresh bootstrap schema carries the read index for new installations.
	// Remove it here to recreate the populated pre-087 upgrade state that the
	// standalone concurrent migration must handle.
	if _, err := applyDB.ExecContext(
		ctx,
		"DROP INDEX IF EXISTS "+activeOCIWarningIndexName,
	); err != nil {
		t.Fatalf("remove fresh-bootstrap warning index for upgrade proof: %v", err)
	}
	seedActiveOCIWarningIndexProof(t, ctx, applyDB)
	assertActiveOCIWarningIndexAbsent(t, ctx, applyDB)
	rowsBefore := countActiveOCIWarningIndexProofRows(t, ctx, applyDB)

	blocker := beginActiveOCIWarningIndexSnapshot(t, ctx, blockerDB)
	firstApplyErrors := make(chan error, 1)
	go func() {
		firstApplyErrors <- ApplyDefinitionsWithLockTimeout(
			ctx,
			exec,
			[]Definition{migration},
			5*time.Second,
		)
	}()
	waitForActiveOCIWarningIndexSnapshotPhase(t, ctx, db)
	assertActiveOCIWarningIndexWriterAvailable(t, ctx, writerDB)
	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release first index-build snapshot: %v", err)
	}
	if err := <-firstApplyErrors; err != nil {
		t.Fatalf("apply migration 087 to populated store: %v", err)
	}
	first := readActiveOCIWarningIndexState(t, ctx, applyDB)
	assertActiveOCIWarningIndexReady(t, first)
	if got := countActiveOCIWarningIndexProofRows(t, ctx, applyDB); got != rowsBefore+1 {
		t.Fatalf("populated migration row count = %d, want %d", got, rowsBefore+1)
	}

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("first repeated ApplyBootstrap(): %v", err)
	}
	second := readActiveOCIWarningIndexState(t, ctx, applyDB)
	assertActiveOCIWarningIndexReady(t, second)
	if second != first {
		t.Fatalf("first repeated bootstrap changed warning index: first=%+v second=%+v", first, second)
	}
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("second repeated ApplyBootstrap(): %v", err)
	}
	third := readActiveOCIWarningIndexState(t, ctx, applyDB)
	assertActiveOCIWarningIndexReady(t, third)
	if third != second {
		t.Fatalf("second repeated bootstrap changed warning index: second=%+v third=%+v", second, third)
	}

	if _, err := applyDB.ExecContext(
		ctx,
		"DROP INDEX CONCURRENTLY "+activeOCIWarningIndexName,
	); err != nil {
		t.Fatalf("drop warning index before cancellation proof: %v", err)
	}
	blocker = beginActiveOCIWarningIndexSnapshot(t, ctx, blockerDB)
	cancelCtx, cancelBuild := context.WithCancel(ctx)
	cancelErrors := make(chan error, 1)
	go func() {
		cancelErrors <- ApplyDefinitionsWithLockTimeout(
			cancelCtx,
			exec,
			[]Definition{migration},
			5*time.Second,
		)
	}()
	waitForActiveOCIWarningIndexSnapshotPhase(t, ctx, db)
	cancelBuild()
	if err := <-cancelErrors; err == nil {
		t.Fatal("cancelled migration 087 error = nil, want cancellation")
	}
	// A client-observed cancellation is not a server-side abort: the
	// concurrent build leaves its invalid index row behind from the start,
	// so the read below cannot tell an aborted build from one still
	// shutting down. If blocker.Rollback lands first, the surviving backend
	// completes the build instead of aborting it, and the retry keeps the
	// same OID. Wait for the backend to go idle before releasing the
	// snapshot; a timeout fails loudly, never silently.
	waitForActiveOCIWarningIndexBuildAbort(t, ctx, db)
	invalid := readActiveOCIWarningIndexState(t, ctx, writerDB)
	if invalid.valid {
		t.Fatalf("cancelled warning index is valid before releasing old snapshot, want invalid: %+v", invalid)
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release cancelled index-build snapshot: %v", err)
	}

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("retry full bootstrap after invalid migration 087 cleanup: %v", err)
	}
	recovered := readActiveOCIWarningIndexState(t, ctx, applyDB)
	assertActiveOCIWarningIndexReady(t, recovered)
	if recovered.oid == invalid.oid {
		t.Fatalf("retry retained invalid warning-index OID %d", invalid.oid)
	}
	if got := countActiveOCIWarningIndexProofRows(t, ctx, applyDB); got != rowsBefore+1 {
		t.Fatalf("cancellation recovery row count = %d, want %d", got, rowsBefore+1)
	}
}

func activeOCIWarningIndexUpgradeDefinitions(t *testing.T) ([]Definition, Definition) {
	t.Helper()
	definitions := BootstrapDefinitions()
	preUpgrade := make([]Definition, 0, len(definitions)-1)
	var migration Definition
	for _, definition := range definitions {
		if definition.Name == "fact_records_active_oci_warning_idx" {
			migration = definition
			continue
		}
		preUpgrade = append(preUpgrade, definition)
	}
	if migration.Name == "" || len(preUpgrade) != len(definitions)-1 {
		t.Fatal("migration 087 missing from bootstrap definitions")
	}
	return preUpgrade, migration
}

func seedActiveOCIWarningIndexProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    'oci-registry://registry.example.com/team/api', 'oci_registry',
    'oci_registry', 'registry.example.com/team/api', 'oci_registry',
    'registry.example.com/team/api', clock_timestamp(), clock_timestamp(), 'active'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES (
    'generation-5854-warning-index',
    'oci-registry://registry.example.com/team/api',
    'snapshot', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()
);
UPDATE ingestion_scopes
SET active_generation_id = 'generation-5854-warning-index'
WHERE scope_id = 'oci-registry://registry.example.com/team/api';
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    is_tombstone, payload
)
SELECT
    'warning-index-proof:' || series,
    'oci-registry://registry.example.com/team/api',
    'generation-5854-warning-index',
    CASE WHEN series % 10 = 0 THEN 'oci_registry.warning' ELSE 'content_entity' END,
    'warning-index-proof:' || series,
    'oci_registry',
    CASE WHEN series % 10 = 0 THEN 'oci_registry' ELSE 'git' END,
    'warning-index-proof:' || series,
    '2026-07-29T12:00:00Z'::timestamptz + series * interval '1 microsecond',
    clock_timestamp(),
    FALSE,
    CASE WHEN series % 10 = 0
        THEN jsonb_build_object(
            'warning_code', 'tag_list_truncated',
            'repository_id', 'oci-registry://registry.example.com/team/api'
        )
        ELSE jsonb_build_object('entity_name', 'Synthetic' || series)
    END
FROM generate_series(1, 100000) AS series;
`); err != nil {
		t.Fatalf("seed populated warning-index store: %v", err)
	}
}

func beginActiveOCIWarningIndexSnapshot(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatalf("begin old snapshot: %v", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM fact_records").Scan(&count); err != nil {
		_ = tx.Rollback()
		t.Fatalf("establish old fact-record snapshot: %v", err)
	}
	if count == 0 {
		_ = tx.Rollback()
		t.Fatal("old fact-record snapshot is empty")
	}
	return tx
}

func waitForActiveOCIWarningIndexSnapshotPhase(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var phase string
		// Unqualified names resolve through the proof database's
		// public search_path: the proof owns the whole database, so no
		// schema qualification is needed to isolate the wait.
		err := db.QueryRowContext(ctx, `
SELECT phase
FROM pg_stat_progress_create_index
WHERE relid = to_regclass($1)
  AND index_relid = to_regclass($2)
  AND command = 'CREATE INDEX CONCURRENTLY'
`, "fact_records", activeOCIWarningIndexName).Scan(&phase)
		if err == nil && strings.Contains(phase, "waiting for old snapshots") {
			return
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("inspect warning-index progress: %v", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("wait for warning-index old-snapshot phase: %v", ctx.Err())
		}
	}
}

// waitForActiveOCIWarningIndexBuildAbort waits until no backend is still
// actively running the cancelled 087 index build. The build backend reports
// its migration SQL as its query text, so an active row with that text is
// the build still shutting down; only an idle cluster (for this database)
// means the abort completed and blocker.Rollback cannot revive the build.
// Scoped to the proof database, which no other test shares.
func waitForActiveOCIWarningIndexBuildAbort(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	deadlineCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var active int
		// The poll query itself contains the migration name, so it would
		// match its own activity row without the pid guard below.
		if err := db.QueryRowContext(deadlineCtx, `
SELECT count(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND state = 'active'
  AND pid <> pg_backend_pid()
  AND query LIKE '%fact_records_active_oci_warning_idx%'
`).Scan(&active); err != nil {
			t.Fatalf("inspect warning-index build backend: %v", err)
		}
		if active == 0 {
			return
		}
		select {
		case <-ticker.C:
		case <-deadlineCtx.Done():
			t.Fatalf("wait for cancelled warning-index build to stop: %v", deadlineCtx.Err())
		}
	}
}

func assertActiveOCIWarningIndexWriterAvailable(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	started := time.Now()
	if _, err := db.ExecContext(writeCtx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    collector_kind, source_system, source_fact_key, observed_at, ingested_at,
    is_tombstone, payload
) VALUES (
    'warning-index-proof:concurrent-writer',
    'oci-registry://registry.example.com/team/api',
    'generation-5854-warning-index',
    'oci_registry.warning',
    'warning-index-proof:concurrent-writer',
    'oci_registry',
    'oci_registry',
    'warning-index-proof:concurrent-writer',
    clock_timestamp(),
    clock_timestamp(),
    FALSE,
    '{"warning_code":"tag_list_truncated","repository_id":"oci-registry://registry.example.com/team/api"}'::jsonb
)
`); err != nil {
		t.Fatalf("write during concurrent warning-index build: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("write during concurrent warning-index build took %s, want under 2s", elapsed)
	}
}

func readActiveOCIWarningIndexState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) activeOCIWarningIndexState {
	t.Helper()
	var state activeOCIWarningIndexState
	if err := db.QueryRowContext(ctx, `
SELECT index_class.oid::bigint, pg_get_indexdef(index_class.oid),
       index.indisvalid, index.indisready
FROM pg_class AS index_class
JOIN pg_index AS index ON index.indexrelid = index_class.oid
WHERE index_class.relname = $1
  AND index_class.relnamespace = to_regnamespace(current_schema())
`, activeOCIWarningIndexName).Scan(
		&state.oid,
		&state.definition,
		&state.valid,
		&state.ready,
	); err != nil {
		t.Fatalf("read active OCI warning index state: %v", err)
	}
	return state
}

func assertActiveOCIWarningIndexReady(t *testing.T, state activeOCIWarningIndexState) {
	t.Helper()
	if !state.valid || !state.ready {
		t.Fatalf("active OCI warning index is not valid and ready: %+v", state)
	}
	for _, want := range []string{
		"fact_records_active_oci_warning_idx",
		"observed_at",
		"fact_id",
		"scope_id",
		"generation_id",
		"fact_kind = 'oci_registry.warning'",
		"source_system = 'oci_registry'",
		"is_tombstone = false",
	} {
		if !strings.Contains(state.definition, want) {
			t.Fatalf("active OCI warning index definition missing %q: %s", want, state.definition)
		}
	}
}

func assertActiveOCIWarningIndexAbsent(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(
		ctx,
		`SELECT count(*)
		 FROM pg_class
		 WHERE relname = $1
		   AND relnamespace = to_regnamespace(current_schema())`,
		activeOCIWarningIndexName,
	).Scan(&count); err != nil {
		t.Fatalf("inspect absent active OCI warning index: %v", err)
	}
	if count != 0 {
		t.Fatalf("active OCI warning index count before migration = %d, want 0", count)
	}
}

func countActiveOCIWarningIndexProofRows(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM fact_records").Scan(&count); err != nil {
		t.Fatalf("count warning-index proof rows: %v", err)
	}
	return count
}

func openActiveOCIWarningIndexProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open warning-index proof database: %v", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("ping warning-index proof database: %v", err)
	}
	return db
}

func activeOCIWarningIndexSchemaDSN(t *testing.T, dsn string, schema string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse warning-index proof DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

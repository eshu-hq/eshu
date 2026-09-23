// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// #7002 fixture values: mirror the constants pinned in
// migrations/checksum_alias_test.go (that package cannot be reused directly
// since these live tests need untyped package-level constants of their own).
const (
	migration093Path              = "go/internal/storage/postgres/migrations/093_cross_scope_completion_queue.sql"
	migration093ShippedChecksum   = "c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42"
	migration093PR6785Checksum    = "6cdb3e58545d3cbf13649402549ee7c1e577f99666bc2a5e0e87c1b9d0735e50"
	migration093PR6923Checksum    = "7f73153be9a782875b054085c6eafa9d7e6723507b343e2e5ec43b307168473c"
	migration093UnknownChecksum   = "0000000000000000000000000000000000000000000000000000000000000"
	otherMigrationPathForAliasNeg = "go/internal/storage/postgres/migrations/112_value_flow_refresh_producer_domains.sql"
)

// definitionByPath returns the current BootstrapDefinitions() entry for path,
// or fails the test -- used to compute the real checksum a test must restore
// after deliberately planting a different one in the ledger.
func definitionByPath(t *testing.T, path string) Definition {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if def.Path == path {
			return def
		}
	}
	t.Fatalf("no BootstrapDefinitions() entry for path %q", path)
	return Definition{}
}

// plantChecksumAndRestore overwrites path's recorded full checksum and
// registers a t.Cleanup that restores it to its real, current checksum --
// the shared-database convention TestBootstrapRejectsChangedRecordedMigrationBeforeDDLLive
// already uses, so these #7002 tests can safely share one bootstrapped
// database with the rest of this file's live tests.
func plantChecksumAndRestore(t *testing.T, ctx context.Context, db *sql.DB, path, checksum string) {
	t.Helper()
	real := migrationChecksum(definitionByPath(t, path).SQL)
	if _, err := db.ExecContext(ctx,
		"UPDATE eshu_schema_migrations SET checksum_sha256 = $1 WHERE path = $2 AND variant = 'full'",
		checksum, path,
	); err != nil {
		t.Fatalf("plant checksum for %s: %v", path, err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(),
			"UPDATE eshu_schema_migrations SET checksum_sha256 = $1 WHERE path = $2 AND variant = 'full'",
			real, path,
		); err != nil {
			t.Errorf("restore checksum for %s: %v", path, err)
		}
	})
}

// TestBootstrapAcceptsLedgerRecordingShippedChecksum093Live reproduces
// #7002's exact ops-qa failure: a database that recorded 093 with its
// originally shipped checksum must not be refused once 093 is restored, and
// bootstrap must still apply every migration recorded after it. Before the
// #7002 fix (093 carrying the checksum #6785/#6923 left behind on main), this
// fails with "checksum changed" -- see the RED tail captured in the handoff.
func TestBootstrapAcceptsLedgerRecordingShippedChecksum093Live(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("initial apply bootstrap: %v", err)
	}
	plantChecksumAndRestore(t, ctx, db, migration093Path, migration093ShippedChecksum)

	// Simulate ops-qa: a rollout stuck because no migration recorded after
	// 093 (112 through 120) ever ran.
	definitions := BootstrapDefinitions()
	pending := 0
	for _, def := range definitions {
		if def.Path <= migration093Path {
			continue
		}
		if _, err := db.ExecContext(ctx,
			"DELETE FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'", def.Path,
		); err != nil {
			t.Fatalf("unrecord pending migration %s: %v", def.Path, err)
		}
		pending++
	}
	if pending == 0 {
		t.Fatal("test setup produced no pending migrations after 093 -- nothing would exercise the fix")
	}

	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap() with ledger recording 093's shipped checksum = %v, want success", err)
	}

	var recorded int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM eshu_schema_migrations WHERE variant = 'full'",
	).Scan(&recorded); err != nil {
		t.Fatalf("count recorded migrations: %v", err)
	}
	if recorded != len(definitions) {
		t.Fatalf("recorded migrations = %d, want %d (later pending migrations must apply)", recorded, len(definitions))
	}
}

// TestBootstrapAcceptsSupersededChecksumAliasesFor093Live proves the narrow
// alias path end-to-end: a ledger that recorded either edited-in-place
// checksum of 093 (#6785 or #6923) must still bootstrap successfully once 093
// is restored, because those checksums were genuinely applied to real
// databases during the window described in migrations/checksum_alias.go.
func TestBootstrapAcceptsSupersededChecksumAliasesFor093Live(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("initial apply bootstrap: %v", err)
	}
	for _, alias := range []string{migration093PR6785Checksum, migration093PR6923Checksum} {
		plantChecksumAndRestore(t, ctx, db, migration093Path, alias)
		if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
			t.Fatalf("ApplyBootstrap() with ledger recording superseded checksum %s = %v, want success", alias, err)
		}
	}
}

// TestBootstrapRejectsUnknownChecksumFor093Live proves the alias is not a
// blanket bypass: an unrecognized recorded checksum for 093 still fails loud.
func TestBootstrapRejectsUnknownChecksumFor093Live(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("initial apply bootstrap: %v", err)
	}
	plantChecksumAndRestore(t, ctx, db, migration093Path, migration093UnknownChecksum)
	err = ApplyBootstrap(ctx, SQLDB{DB: db})
	if err == nil || !strings.Contains(err.Error(), "checksum changed") {
		t.Fatalf("ApplyBootstrap() error = %v, want checksum changed for an unrecognized 093 checksum", err)
	}
}

// TestBootstrapAliasDoesNotLeakToOtherPathLive proves 093's alias entries do
// not accidentally validate a checksum drift on a different migration file.
func TestBootstrapAliasDoesNotLeakToOtherPathLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("initial apply bootstrap: %v", err)
	}
	plantChecksumAndRestore(t, ctx, db, otherMigrationPathForAliasNeg, migration093PR6785Checksum)
	err = ApplyBootstrap(ctx, SQLDB{DB: db})
	if err == nil || !strings.Contains(err.Error(), "checksum changed") {
		t.Fatalf("ApplyBootstrap() error = %v, want checksum changed (093's alias must not cover %s)",
			err, otherMigrationPathForAliasNeg)
	}
}

// TestBootstrapFreshInstallMatchesPreFix093Live proves the restored
// 093 -> 112 -> 120 upgrade chain lands on the same constraint and trigger
// definitions that a fresh bootstrap of main's edited-in-place 093 produced
// before this fix. The oracle strings below were captured live, once, from
// this same postgres:18-alpine image, against 093 exactly as main carried it
// (874012542e) prior to this fix.
func TestBootstrapFreshInstallMatchesPreFix093Live(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_7002_FRESH_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_7002_FRESH_TEST_DSN to an empty disposable PostgreSQL database")
	}
	const preFixConstraintDef = "CHECK ((producer_domain = ANY (ARRAY['aws_resource_materialization'::text, 'ci_cd_run_correlation'::text, 'code_function_summary'::text, 'container_image_identity'::text, 'iam_can_perform_materialization'::text, 'workload_cloud_relationship_materialization'::text, 'workload_materialization'::text])))"
	const preFixTriggerDef = "CREATE TRIGGER fact_work_items_cross_scope_completion AFTER UPDATE OF status ON public.fact_work_items FOR EACH ROW WHEN (((old.stage = 'reducer'::text) AND (new.stage = 'reducer'::text) AND (old.domain = new.domain) AND (new.domain = ANY (ARRAY['aws_resource_materialization'::text, 'ci_cd_run_correlation'::text, 'code_function_summary'::text, 'container_image_identity'::text, 'iam_can_perform_materialization'::text, 'workload_cloud_relationship_materialization'::text, 'workload_materialization'::text])) AND (old.status = ANY (ARRAY['claimed'::text, 'running'::text])) AND (new.status = 'succeeded'::text) AND (old.cross_scope_completion_ack_epoch = new.cross_scope_completion_ack_epoch))) EXECUTE FUNCTION enqueue_cross_scope_completion_event()"

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: db}, BootstrapDefinitions(), slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("fresh apply bootstrap: %v", err)
	}

	var constraintDef string
	if err := db.QueryRowContext(ctx,
		"SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'cross_scope_completion_events_producer_domain_check'",
	).Scan(&constraintDef); err != nil {
		t.Fatalf("read constraint: %v", err)
	}
	if constraintDef != preFixConstraintDef {
		t.Fatalf("fresh-install constraint = %q, want the pre-fix equivalence oracle %q", constraintDef, preFixConstraintDef)
	}

	var triggerDef string
	if err := db.QueryRowContext(ctx,
		"SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgname = 'fact_work_items_cross_scope_completion' AND NOT tgisinternal",
	).Scan(&triggerDef); err != nil {
		t.Fatalf("read trigger: %v", err)
	}
	if triggerDef != preFixTriggerDef {
		t.Fatalf("fresh-install trigger = %q, want the pre-fix equivalence oracle %q", triggerDef, preFixTriggerDef)
	}
}

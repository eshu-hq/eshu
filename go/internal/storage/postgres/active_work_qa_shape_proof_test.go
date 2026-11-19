// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestStatusActiveWorkSummaryQAShapePlan is the #10872 QA-shape plan proof:
// EXPLAIN (ANALYZE, BUFFERS) of the unchanged activeWorkSummaryQuery main
// form at ~268,750 scope_generations / ~268,750 fact_work_items rows with a
// ~1.7 GB database, the grouped branch QA runs. It reuses the #4446 seed
// shape (uniform generations-per-scope, work items on the active generation)
// and the #7009 gate-state guard, so the plan properties it asserts are the
// same single-pass contract: gate CTE loops=1, no per-row generation
// rescan. Payload padding is incompressible random hex so the buffer counts
// reflect physical pages, not compressible filler.
//
// Skipped unless a live Postgres DSN is provided, like the fixture-scale
// proof. It seeds and analyzes roughly half a million rows plus ~1.7 GB of
// payload, so it takes minutes even on a quiet host.
func TestStatusActiveWorkSummaryQAShapePlan(t *testing.T) {
	dsn := statusActiveGenerationBenchDSN()
	if dsn == "" {
		if os.Getenv("ESHU_REQUIRE_ACTIVE_WORK_PROJECTION_PROOF") == "1" {
			t.Fatal("ESHU_STATUS_ACTIVE_GENERATION_BENCH_DSN or ESHU_POSTGRES_DSN is required for the #10872 QA-shape plan proof")
		}
		t.Skip("set ESHU_STATUS_ACTIVE_GENERATION_BENCH_DSN or ESHU_POSTGRES_DSN to run the status QA-shape plan proof")
	}

	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated postgres connection: %v", err)
	}
	conn := reducerClaimBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("status_qa_shape_proof_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = sqlConn.Close() })
	if os.Getenv("ESHU_STATUS_ACTIVE_GENERATION_KEEP_SCHEMA") == "" {
		t.Cleanup(func() {
			_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		})
	} else {
		t.Logf("keeping schema %s for inspection", schemaName)
	}
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}

	// 5,375 scopes x 50 generations = 268,750 scope_generations and
	// 5,375 x 50 = 268,750 fact_work_items, the QA row-count band.
	benchCase := statusActiveGenerationBenchCase{scopeCount: 5_375, generationsPerScope: 50, workItemsPerScope: 50}
	if err := seedStatusActiveGenerationBenchmark(ctx, conn, benchCase); err != nil {
		t.Fatalf("seed QA-shape data: %v", err)
	}

	// Pad payloads with incompressible random hex so the database reaches
	// the QA ~1.7 GB band physically. TOAST compresses repetitive filler,
	// which would understate buffer counts; gen_random_bytes does not.
	if _, err := conn.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public"); err != nil {
		t.Fatalf("install pgcrypto for padding: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET payload = payload || jsonb_build_object('qa_shape_pad', encode(public.gen_random_bytes(1024) || public.gen_random_bytes(976), 'hex'))`); err != nil {
		t.Fatalf("pad work-item payloads: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE scope_generations SET payload = payload || jsonb_build_object('qa_shape_pad', encode(public.gen_random_bytes(1000), 'hex'))`); err != nil {
		t.Fatalf("pad generation payloads: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM ANALYZE fact_work_items"); err != nil {
		t.Fatalf("vacuum work items: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM ANALYZE scope_generations"); err != nil {
		t.Fatalf("vacuum generations: %v", err)
	}
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		t.Fatalf("analyze proof tables: %v", err)
	}
	logQAShapeSizes(ctx, t, conn)

	for _, stmt := range []string{
		MigrationSQL("shared_projection_intents"),
		MigrationSQL("shared_projection_intent_reopened_at"),
		MigrationSQL("shared_projection_intents_domain_pending_age_idx"),
		`ALTER TABLE fact_work_items ADD COLUMN IF NOT EXISTS provenance_edge_identity_upgrade_required BOOLEAN NOT NULL DEFAULT FALSE`,
		`CREATE TABLE IF NOT EXISTS cross_scope_completion_upgrade_markers (marker_name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("extend proof schema for activeWorkSummaryQuery: %v", err)
		}
	}

	// Both gate branches (#7009 S5 ruling D5.5), then a forced generic plan.
	checkSummaryPlanInGateStates(ctx, t, conn, time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC))
}

// logQAShapeSizes records the physical footprint the QA-shape plans ran
// against: per-table relation sizes plus the database total.
func logQAShapeSizes(ctx context.Context, t *testing.T, conn reducerClaimBenchmarkConn) {
	t.Helper()
	rows, err := conn.QueryContext(ctx, `
SELECT relname,
    pg_size_pretty(pg_relation_size(oid)) AS rel,
    pg_size_pretty(pg_total_relation_size(oid)) AS total
FROM pg_class
WHERE relname IN ('fact_work_items', 'scope_generations', 'ingestion_scopes')
ORDER BY relname`)
	if err != nil {
		t.Fatalf("read relation sizes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name, rel, total string
		if err := rows.Scan(&name, &rel, &total); err != nil {
			t.Fatalf("scan relation size: %v", err)
		}
		t.Logf("qa-shape size %s: rel=%s total=%s", name, rel, total)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate relation sizes: %v", err)
	}
	dbRows, err := conn.QueryContext(ctx, `SELECT pg_size_pretty(pg_database_size(current_database()))`)
	if err != nil {
		t.Fatalf("read database size: %v", err)
	}
	defer func() { _ = dbRows.Close() }()
	for dbRows.Next() {
		var size string
		if err := dbRows.Scan(&size); err != nil {
			t.Fatalf("scan database size: %v", err)
		}
		t.Logf("qa-shape database size: %s", size)
	}
	if err := dbRows.Err(); err != nil {
		t.Fatalf("iterate database size: %v", err)
	}
}

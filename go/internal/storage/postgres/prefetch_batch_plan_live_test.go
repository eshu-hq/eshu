// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestPrefetchBatchQueriesUsePrimaryKeyLive is the #7724 1A plan proof:
// at 500-key scale over 100K-row tables both batch queries probe the
// primary key with no sequential scan. It also proves the shipped SQL is
// valid against real Postgres (UNNEST array binding included) and that
// LookupBatch returns exactly the present subset.
func TestPrefetchBatchQueriesUsePrimaryKeyLive(t *testing.T) {
	const schema = "eshu_7724_prefetch_batch_live"

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live prefetch plan proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated proof schema: %v", err)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	sqldb, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	sqldb.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqldb.Close() })
	database := SQLDB{DB: sqldb}

	// Stub parents for the REFERENCES clauses, then the real table DDL
	// through the shipped EnsureSchema paths (no hand-copied schema).
	if _, err := sqldb.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (scope_id TEXT PRIMARY KEY);
CREATE TABLE scope_generations (generation_id TEXT PRIMARY KEY, ingested_at TIMESTAMPTZ NOT NULL DEFAULT now());
INSERT INTO ingestion_scopes (scope_id) SELECT 'scope-' || g FROM generate_series(0, 120000) g;
INSERT INTO scope_generations (generation_id) SELECT 'gen-' || g FROM generate_series(0, 120000) g;
`); err != nil {
		t.Fatalf("seed stub parents: %v", err)
	}
	if err := NewSharedProjectionAcceptanceStore(database).EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure acceptance schema: %v", err)
	}
	if err := NewGraphProjectionPhaseStateStore(database).EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure phase schema: %v", err)
	}
	// 100K rows per table: large enough that a sequential scan is never
	// the cheapest plan for a 500-key selective lookup. Shaped like
	// production: 10 scopes of 10K units each, so a scope-only index
	// cond still matches 10K rows and the planner must use the full key.
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO shared_projection_acceptance (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
SELECT 'scope-' || (g % 10), 'unit-' || (g / 10), 'run-1', 'gen-' || g, now(), now()
FROM generate_series(0, 99999) g;
INSERT INTO graph_projection_phase_state (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase, committed_at, updated_at)
SELECT 'scope-' || (g % 10), 'unit-' || (g / 10), 'run-1', 'gen-' || g, 'code_entities_uid', 'canonical_nodes_committed', now(), now()
FROM generate_series(0, 99999) g;
ANALYZE shared_projection_acceptance;
ANALYZE graph_projection_phase_state;
`); err != nil {
		t.Fatalf("seed 100K-row tables: %v", err)
	}

	t.Run("acceptance", func(t *testing.T) {
		var keys []sharedintent.AcceptanceKey
		scopes := make([]string, 0, 500)
		units := make([]string, 0, 500)
		runs := make([]string, 0, 500)
		for i := 0; i < 500; i++ {
			// Even keys hit rows; odd keys miss (unit shifted out of range).
			unit := fmt.Sprintf("unit-%d", i*20)
			if i%2 == 1 {
				unit = fmt.Sprintf("unit-missing-%d", i)
			}
			key := sharedintent.AcceptanceKey{ScopeID: fmt.Sprintf("scope-%d", i%10), AcceptanceUnitID: unit, SourceRunID: "run-1"}
			keys = append(keys, key)
			scopes = append(scopes, key.ScopeID)
			units = append(units, key.AcceptanceUnitID)
			runs = append(runs, key.SourceRunID)
		}

		plan := explainQuery(t, ctx, sqldb, lookupSharedProjectionAcceptanceBatchSQL, scopes, units, runs)
		assertIndexPlan(t, "acceptance batch", plan, "shared_projection_acceptance")

		store := NewSharedProjectionAcceptanceStore(database)
		got, err := store.LookupBatch(ctx, keys)
		if err != nil {
			t.Fatalf("LookupBatch() error = %v", err)
		}
		if len(got) != 250 {
			t.Fatalf("LookupBatch() returned %d rows, want 250 (present subset)", len(got))
		}
		for i := 0; i < 500; i += 2 {
			want := fmt.Sprintf("gen-%d", (i*20)*10+(i%10))
			if got[keys[i]] != want {
				t.Fatalf("LookupBatch()[key %d] = %q, want %q", i, got[keys[i]], want)
			}
		}
	})

	t.Run("readiness", func(t *testing.T) {
		var keys []reducer.GraphProjectionPhaseKey
		scopes := make([]string, 0, 500)
		units := make([]string, 0, 500)
		runs := make([]string, 0, 500)
		generations := make([]string, 0, 500)
		keyspaces := make([]string, 0, 500)
		for i := 0; i < 500; i++ {
			gen := fmt.Sprintf("gen-%d", (i*20)*10+(i%10))
			if i%2 == 1 {
				gen = fmt.Sprintf("gen-missing-%d", i)
			}
			key := reducer.GraphProjectionPhaseKey{
				ScopeID: fmt.Sprintf("scope-%d", i%10), AcceptanceUnitID: fmt.Sprintf("unit-%d", i*20),
				SourceRunID: "run-1", GenerationID: gen, Keyspace: reducer.GraphProjectionKeyspaceCodeEntitiesUID,
			}
			keys = append(keys, key)
			scopes = append(scopes, key.ScopeID)
			units = append(units, key.AcceptanceUnitID)
			runs = append(runs, key.SourceRunID)
			generations = append(generations, key.GenerationID)
			keyspaces = append(keyspaces, string(key.Keyspace))
		}

		plan := explainQuery(t, ctx, sqldb, lookupGraphProjectionPhaseStateBatchSQL,
			scopes, units, runs, generations, keyspaces, string(reducer.GraphProjectionPhaseCanonicalNodesCommitted))
		assertIndexPlan(t, "readiness batch", plan, "graph_projection_phase_state")

		store := NewGraphProjectionPhaseStateStore(database)
		got, err := store.LookupBatch(ctx, keys, reducer.GraphProjectionPhaseCanonicalNodesCommitted)
		if err != nil {
			t.Fatalf("LookupBatch() error = %v", err)
		}
		if len(got) != 250 {
			t.Fatalf("LookupBatch() returned %d rows, want 250 (present subset)", len(got))
		}
	})
}

// explainQuery runs EXPLAIN over the shipped query constant with the
// given bind arguments and returns the plan text. The constant is used
// directly (same package), never hand-copied, so the proof cannot drift
// from the shipped shape.
func explainQuery(t *testing.T, ctx context.Context, sqldb *sql.DB, shipped string, args ...any) string {
	t.Helper()
	rows, err := sqldb.QueryContext(ctx, "EXPLAIN "+shipped, args...)
	if err != nil {
		t.Fatalf("EXPLAIN error = %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate EXPLAIN: %v", err)
	}
	t.Logf("plan:\n%s", plan.String())
	return plan.String()
}

// assertIndexPlan fails when the plan sequentially scans the table or
// probes anything but the table's primary-key index (#7724 1A: EXPLAIN
// proof of PK use, no seq scan).
func assertIndexPlan(t *testing.T, name, plan, table string) {
	t.Helper()
	if strings.Contains(plan, "Seq Scan") {
		t.Fatalf("%s plan sequentially scans:\n%s", name, plan)
	}
	if !strings.Contains(plan, table+"_pkey") {
		t.Fatalf("%s plan does not probe the %s primary key:\n%s", name, table, plan)
	}
}

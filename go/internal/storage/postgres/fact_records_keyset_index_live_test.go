// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestFactRecordsKeysetIndexLifecycleAndSeekLive proves migration 099
// (fact_records_scope_generation_keyset_idx) on a live Postgres against the
// shape that made pagination quadratic: one generation whose facts all share a
// single observed_at.
//
// It covers what the fake-based tests structurally cannot. Those pin statement
// shape; they cannot see a planner decision. The assertion that matters here is
// the last one: that the cursor statement takes an index seek under a GENERIC
// plan. That is the exact regression #6154 was about — the folded predicate
// planned fine with literal parameters and collapsed to a filter scan under the
// generic plan the services actually get through database/sql, so an EXPLAIN
// with literals reported 4.7ms for a shape that cost 580ms per page in
// production.
func TestFactRecordsKeysetIndexLifecycleAndSeekLive(t *testing.T) {
	const (
		schema    = "eshu_6154_fact_records_keyset_live"
		indexName = "fact_records_scope_generation_keyset_idx"
		scopeID   = "git-repository-scope:repository:keyset-live"
		genID     = "generation-keyset-live"
	)

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #6154 keyset index proof")
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
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	// The pathological shape, reduced: 20,000 facts in one generation, every
	// one carrying the same observed_at, so the cursor's leading column cannot
	// advance and only fact_id can bound a page. A second generation and a
	// second fact_kind are present so the seek has to discriminate rather than
	// matching the whole table.
	if _, err := db.ExecContext(ctx, `
CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '0.0.0',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT 'keyset-live-' || lpad(i::text, 8, '0'),
       '`+scopeID+`', '`+genID+`', 'content_entity',
       'content_entity:keyset-live:' || i,
       'git', 'keyset-live-' || i,
       TIMESTAMPTZ '2026-08-12 14:17:59.452906+00',
       TIMESTAMPTZ '2026-08-12 14:17:59.452906+00',
       '{}'::jsonb
FROM generate_series(1, 20000) AS i;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT 'keyset-live-other-' || lpad(i::text, 8, '0'),
       '`+scopeID+`', 'generation-keyset-live-other', 'content_entity',
       'content_entity:keyset-live-other:' || i,
       'git', 'keyset-live-other-' || i,
       TIMESTAMPTZ '2026-08-13 09:00:00+00',
       TIMESTAMPTZ '2026-08-13 09:00:00+00',
       '{}'::jsonb
FROM generate_series(1, 5000) AS i;
`); err != nil {
		t.Fatalf("seed constant-observed_at generation: %v", err)
	}

	definitions := factRecordsKeysetIndexDefinitions(t)

	// First application, then an identical reapplication: CONCURRENTLY
	// IF NOT EXISTS must be a clean no-op the second time, not an error and not
	// a rebuild.
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
			t.Fatalf("apply %s pass %d: %v", indexName, pass, err)
		}
	}
	assertKeysetIndexState(ctx, t, db, schema, indexName, true)

	// The seek, under both plan modes. Custom plan first, then a forced generic
	// plan — the mode the services actually run, and the one that made the
	// pre-fix statement collapse to a filter scan.
	for _, mode := range []string{"auto", "force_generic_plan"} {
		assertKeysetCursorPlanSeeks(ctx, t, db, indexName, mode, scopeID, genID)
	}

	// Rollback from a populated, previously-indexed store.
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+indexName); err != nil {
		t.Fatalf("rollback %s: %v", indexName, err)
	}
	assertKeysetIndexState(ctx, t, db, schema, indexName, false)

	// Reapplying after a rollback must rebuild cleanly, which is the path a
	// retry takes after the schema apply drops an invalid index by name.
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("rebuild %s after rollback: %v", indexName, err)
	}
	assertKeysetIndexState(ctx, t, db, schema, indexName, true)
}

// assertKeysetIndexState checks presence and, when present, that the index is
// both valid and ready.
//
// This duplicates a helper that already exists in
// content_entities_k8s_select_partial_index_live_test.go rather than sharing
// it, because that file is behind `//go:build integration` and this one is
// not. Sharing would drag this test back behind a tag nothing in the repo
// passes.
func assertKeysetIndexState(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	schema string,
	indexName string,
	wantPresent bool,
) {
	t.Helper()

	var (
		present bool
		valid   bool
		ready   bool
	)
	err := db.QueryRowContext(ctx, `
SELECT true, i.indisvalid, i.indisready
FROM pg_index AS i
JOIN pg_class AS c ON c.oid = i.indexrelid
JOIN pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2
`, schema, indexName).Scan(&present, &valid, &ready)
	switch {
	case err == sql.ErrNoRows:
		present = false
	case err != nil:
		t.Fatalf("inspect index %s: %v", indexName, err)
	}

	if present != wantPresent {
		t.Fatalf("index %s present = %t, want %t", indexName, present, wantPresent)
	}
	if present && (!valid || !ready) {
		t.Fatalf("index %s state valid=%t ready=%t, want true/true", indexName, valid, ready)
	}
}

func factRecordsKeysetIndexDefinitions(t *testing.T) []Definition {
	t.Helper()
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == "fact_records_keyset_index" {
			return []Definition{definition}
		}
	}
	t.Fatal("fact_records_keyset_index definition not found")
	return nil
}

// assertKeysetCursorPlanSeeks fails unless the cursor statement plans as an
// index scan over the keyset index with the row comparison pushed down as an
// index condition rather than left as a filter.
//
// Checking the plan text for "Index Cond" carrying the row comparison is the
// point: a plan that names the index but leaves the comparison in "Filter" is
// the regression this migration exists to prevent, and it reads as a perfectly
// healthy index scan to anyone who only checks which index was chosen.
func assertKeysetCursorPlanSeeks(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	indexName string,
	planMode string,
	scopeID string,
	generationID string,
) {
	t.Helper()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection for %s plan: %v", planMode, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+planMode); err != nil {
		t.Fatalf("set plan_cache_mode=%s: %v", planMode, err)
	}
	if _, err := conn.ExecContext(ctx, `
PREPARE keyset_cursor_probe (text, text, text[], timestamptz, text, int) AS `+
		listFactsByKindCursorQuery); err != nil {
		t.Fatalf("prepare cursor probe for %s plan: %v", planMode, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "DEALLOCATE keyset_cursor_probe") }()

	// EXECUTE carries its arguments as literals rather than bind parameters:
	// under the extended protocol an EXECUTE cannot take outer parameters of its
	// own. The values are test-controlled constants. What matters for this probe
	// is that the PREPARED statement underneath has parameters, which is what
	// gives Postgres a generic plan to choose.
	execute := "EXECUTE keyset_cursor_probe(" +
		"'" + scopeID + "', " +
		"'" + generationID + "', " +
		"ARRAY['content_entity']::text[], " +
		"TIMESTAMPTZ '2026-08-12 14:17:59.452906+00', " +
		"'keyset-live-00000500', " +
		"500)"

	// Six executions so a generic plan is actually in play by the time the plan
	// is captured; Postgres only considers one from the sixth onward.
	for i := 0; i < 6; i++ {
		if _, err := conn.ExecContext(ctx, execute); err != nil {
			t.Fatalf("execute cursor probe under %s plan: %v", planMode, err)
		}
	}

	rows, err := conn.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+execute)
	if err != nil {
		t.Fatalf("explain cursor probe under %s plan: %v", planMode, err)
	}
	defer func() { _ = rows.Close() }()

	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line under %s plan: %v", planMode, err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan under %s plan: %v", planMode, err)
	}

	planText := plan.String()
	if !strings.Contains(planText, indexName) {
		t.Fatalf("cursor statement did not use %s under %s plan:\n%s", indexName, planMode, planText)
	}

	condStart := strings.Index(planText, "Index Cond")
	if condStart < 0 {
		t.Fatalf("cursor statement has no index condition under %s plan:\n%s", planMode, planText)
	}
	condLine := planText[condStart:]
	if end := strings.Index(condLine, "\n"); end >= 0 {
		condLine = condLine[:end]
	}
	if !strings.Contains(condLine, "observed_at") || !strings.Contains(condLine, "fact_id") {
		t.Fatalf(
			"row comparison is not in the index condition under %s plan, so the page cannot seek:\n%s",
			planMode, planText,
		)
	}
}

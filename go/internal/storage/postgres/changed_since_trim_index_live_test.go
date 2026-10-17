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

// TestChangedSinceTrimIndexLifecycleAndSeekLive proves migrations 194 and 195
// (fact_records_changed_since_live_keys_idx,
// fact_records_changed_since_tombstone_keys_idx) on a live Postgres against
// the shape that made the changed-since diff fetch-then-discard whole
// generations: one probed generation holding live rows plus ~43%
// reducer-derived rows, and a current generation holding tombstones.
//
// It covers what the hermetic pinning test cannot: that each migration
// applies and reapplies cleanly (CONCURRENTLY IF NOT EXISTS), that the
// production aggregate shapes plan as bitmap/index seeks over the new
// partial indexes with the reducer exclusion proved into the index access
// (no Rows Removed by Filter) under both the custom plan and the forced
// generic plan services get through database/sql, and that both roll back
// and rebuild cleanly.
func TestChangedSinceTrimIndexLifecycleAndSeekLive(t *testing.T) {
	const (
		schema         = "eshu_7098_changed_since_trim_live"
		liveIndex      = "fact_records_changed_since_live_keys_idx"
		tombIndex      = "fact_records_changed_since_tombstone_keys_idx"
		probeScope     = "scope-1"
		priorGen       = "gen-1"
		currentGen     = "gen-2"
		wantLiveGroups = 2000
		wantTombGroups = 200
	)

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #7098 changed-since trim proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
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
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	// The waste shape, reduced: 8 scopes x 2 generations, each generation
	// carrying 2,000 live rows plus 1,500 reducer-derived rows (~43%), so the
	// probed generation is 1/16 of the table and the planner must seek rather
	// than scan. The current generation of the probed scope additionally
	// carries 200 tombstones for retired keys.
	if _, err := db.ExecContext(ctx, `
CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, payload)
SELECT 'trim-live-' || s || '-' || g || '-' || i,
       'scope-' || s, 'gen-' || g,
       CASE WHEN i % 10 < 4 THEN 'file'
            WHEN i % 10 < 8 THEN 'content_entity'
            ELSE 'vulnerability.cve' END,
       CASE WHEN i % 10 < 4 THEN 'file:k' || i
            WHEN i % 10 < 8 THEN 'content_entity:k' || i
            ELSE 'facts:k' || i END,
       '{}'::jsonb
FROM generate_series(1, 8) AS s
CROSS JOIN generate_series(1, 2) AS g
CROSS JOIN generate_series(1, 2000) AS i;

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, payload)
SELECT 'trim-red-' || s || '-' || g || '-' || i,
       'scope-' || s, 'gen-' || g,
       'reducer_correlation', 'facts:red' || i,
       '{}'::jsonb
FROM generate_series(1, 8) AS s
CROSS JOIN generate_series(1, 2) AS g
CROSS JOIN generate_series(1, 1500) AS i;

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, is_tombstone, payload)
SELECT 'trim-tomb-' || i, '`+probeScope+`', '`+currentGen+`',
       CASE WHEN i % 10 < 4 THEN 'file'
            WHEN i % 10 < 8 THEN 'content_entity'
            ELSE 'vulnerability.cve' END,
       CASE WHEN i % 10 < 4 THEN 'file:k' || i
            WHEN i % 10 < 8 THEN 'content_entity:k' || i
            ELSE 'facts:k' || i END,
       TRUE, '{}'::jsonb
FROM generate_series(1, 200) AS i;
`); err != nil {
		t.Fatalf("seed trim fixture: %v", err)
	}

	definitions := changedSinceTrimIndexDefinitions(t)

	// First application, then an identical reapplication: CONCURRENTLY
	// IF NOT EXISTS must be a clean no-op the second time, not an error and
	// not a rebuild.
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
			t.Fatalf("apply changed-since trim indexes pass %d: %v", pass, err)
		}
	}
	assertChangedSinceTrimIndexState(ctx, t, db, schema, liveIndex, true)
	assertChangedSinceTrimIndexState(ctx, t, db, schema, tombIndex, true)

	// The seeks, under both plan modes. Custom plan first, then a forced
	// generic plan — the mode the services actually run, and the one where an
	// index whose predicate does not textually match the query filters
	// collapses back to fetching whole generations.
	for _, mode := range []string{"auto", "force_generic_plan"} {
		assertChangedSinceLivePlanSeeks(ctx, t, db, mode, probeScope, priorGen, wantLiveGroups)
		assertChangedSinceTombstonePlanSeeks(ctx, t, db, mode, probeScope, currentGen, wantTombGroups)
	}

	// Rollback from a populated, previously-indexed store.
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+liveIndex); err != nil {
		t.Fatalf("rollback %s: %v", liveIndex, err)
	}
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+tombIndex); err != nil {
		t.Fatalf("rollback %s: %v", tombIndex, err)
	}
	assertChangedSinceTrimIndexState(ctx, t, db, schema, liveIndex, false)
	assertChangedSinceTrimIndexState(ctx, t, db, schema, tombIndex, false)

	// Reapplying after a rollback must rebuild cleanly, which is the path a
	// retry takes after the schema apply drops an invalid index by name.
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("rebuild changed-since trim indexes after rollback: %v", err)
	}
	assertChangedSinceTrimIndexState(ctx, t, db, schema, liveIndex, true)
	assertChangedSinceTrimIndexState(ctx, t, db, schema, tombIndex, true)
}

// changedSinceTrimLiveAggregate is the production prior_keys shape
// (changed_since_sql.go) with its scope/generation parameters: the live
// generation scan that fetched-then-discarded reducer rows.
const changedSinceTrimLiveAggregate = `
SELECT
    CASE
        WHEN fact_kind = 'file' THEN 'files'
        WHEN fact_kind = 'content_entity' THEN 'content_entities'
        ELSE 'facts'
    END AS fact_category,
    stable_fact_key,
    MIN(fact_kind) AS fact_kind,
    COUNT(*) AS row_count,
    MIN(sha256(convert_to((` + changedSincePayloadDigestInput + `)::text, 'UTF8'))) AS single_payload_hash
FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND is_tombstone = FALSE
  AND ` + changedSinceExcludeReducerDerivedKinds + `
GROUP BY fact_category, stable_fact_key`

// changedSinceTrimTombstoneAggregate is the production current_tombstones
// shape: the range-scan over whole generations this slice turns into a seek.
const changedSinceTrimTombstoneAggregate = `
SELECT
    CASE
        WHEN fact_kind = 'file' THEN 'files'
        WHEN fact_kind = 'content_entity' THEN 'content_entities'
        ELSE 'facts'
    END AS fact_category,
    stable_fact_key,
    MIN(fact_kind) AS fact_kind
FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND is_tombstone = TRUE
  AND ` + changedSinceExcludeReducerDerivedKinds + `
GROUP BY fact_category, stable_fact_key`

// assertChangedSinceLivePlanSeeks fails unless the live aggregate plans over
// the live trim index with the reducer exclusion proved into the access (no
// filter removal) and returns the expected group count, so the probe cannot
// pass on an empty fixture. Deriving the statement from the production
// constants (with a hermetic prefix guard below) keeps the probe from
// drifting from the shipped query.
func assertChangedSinceLivePlanSeeks(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	planMode string,
	scopeID string,
	generationID string,
	wantGroups int,
) {
	t.Helper()

	assertChangedSinceTrimProbeDerivesFromProduction(t)
	planText, groupCount := explainChangedSinceTrimProbe(
		ctx, t, db, planMode, "trim_live_probe", changedSinceTrimLiveAggregate, scopeID, generationID,
	)
	const indexName = "fact_records_changed_since_live_keys_idx"
	if !strings.Contains(planText, indexName) {
		t.Fatalf("live aggregate did not use %s under %s plan:\n%s", indexName, planMode, planText)
	}
	// The regression this index exists to prevent reads as a healthy bitmap
	// scan to anyone who only checks which index was chosen: the reducer
	// exclusion left in Filter with thousands of rows removed.
	if strings.Contains(planText, "Rows Removed by Filter") {
		t.Fatalf("live aggregate still filter-discards rows under %s plan:\n%s", planMode, planText)
	}
	if groupCount != wantGroups {
		t.Fatalf("live aggregate groups = %d, want %d under %s plan", groupCount, wantGroups, planMode)
	}
}

// assertChangedSinceTombstonePlanSeeks fails unless the tombstone aggregate
// plans over the tombstone trim index and returns the expected group count.
func assertChangedSinceTombstonePlanSeeks(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	planMode string,
	scopeID string,
	generationID string,
	wantGroups int,
) {
	t.Helper()

	assertChangedSinceTrimProbeDerivesFromProduction(t)
	planText, groupCount := explainChangedSinceTrimProbe(
		ctx, t, db, planMode, "trim_tombstone_probe", changedSinceTrimTombstoneAggregate, scopeID, generationID,
	)
	const indexName = "fact_records_changed_since_tombstone_keys_idx"
	if !strings.Contains(planText, indexName) {
		t.Fatalf("tombstone aggregate did not use %s under %s plan:\n%s", indexName, planMode, planText)
	}
	if strings.Contains(planText, "Rows Removed by Filter") {
		t.Fatalf("tombstone aggregate still filter-discards rows under %s plan:\n%s", planMode, planText)
	}
	if groupCount != wantGroups {
		t.Fatalf("tombstone aggregate groups = %d, want %d under %s plan", groupCount, wantGroups, planMode)
	}
}

// assertChangedSinceTrimProbeDerivesFromProduction is the hermetic prefix
// guard: the probe statements must stay byte-prefixes of the shipped
// classification CTEs, so a hand-frozen copy cannot drift false-green.
func assertChangedSinceTrimProbeDerivesFromProduction(t *testing.T) {
	t.Helper()

	// The CTE bodies share the SELECT list, WHERE filters, and GROUP BY with
	// the probes; assert the filters textually so a predicate change to the
	// shipped query fails here instead of silently unmatching the indexes.
	probeAggregates := changedSinceTrimLiveAggregate + changedSinceTrimTombstoneAggregate
	for _, filter := range []string{
		"is_tombstone = FALSE",
		"is_tombstone = TRUE",
		changedSinceExcludeReducerDerivedKinds,
	} {
		if !strings.Contains(changedSinceClassificationCTEs, filter) {
			t.Fatalf("production classification CTEs lost filter %q", filter)
		}
		if !strings.Contains(probeAggregates, filter) {
			t.Fatalf("trim probe aggregates lost filter %q", filter)
		}
	}
}

// explainChangedSinceTrimProbe prepares the aggregate with parameters (the
// shape that gives Postgres a generic plan to choose), executes it past the
// generic-plan threshold, and returns the EXPLAIN (ANALYZE) text plus the
// result group count.
func explainChangedSinceTrimProbe(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	planMode string,
	probeName string,
	aggregate string,
	scopeID string,
	generationID string,
) (string, int) {
	t.Helper()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire connection for %s plan: %v", planMode, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+planMode); err != nil {
		t.Fatalf("set plan_cache_mode=%s: %v", planMode, err)
	}
	if _, err := conn.ExecContext(ctx, "PREPARE "+probeName+" (text, text) AS "+aggregate); err != nil {
		t.Fatalf("prepare %s probe for %s plan: %v", probeName, planMode, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "DEALLOCATE "+probeName) }()

	// EXECUTE carries its arguments as literals rather than bind parameters:
	// under the extended protocol an EXECUTE cannot take outer parameters of
	// its own. The values are test-controlled constants. What matters is that
	// the PREPARED statement underneath has parameters, which is what gives
	// Postgres a generic plan to choose.
	execute := "EXECUTE " + probeName + "('" + scopeID + "', '" + generationID + "')"

	// Six executions so a generic plan is actually in play by the time the
	// plan is captured; Postgres only considers one from the sixth onward.
	groupCount := 0
	for i := 0; i < 6; i++ {
		rows, err := conn.QueryContext(ctx, execute)
		if err != nil {
			t.Fatalf("execute %s probe under %s plan: %v", probeName, planMode, err)
		}
		count := 0
		for rows.Next() {
			count++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("read %s probe under %s plan: %v", probeName, planMode, err)
		}
		_ = rows.Close()
		groupCount = count
	}

	rows, err := conn.QueryContext(ctx, "EXPLAIN (ANALYZE) "+execute)
	if err != nil {
		t.Fatalf("explain %s probe under %s plan: %v", probeName, planMode, err)
	}
	defer func() { _ = rows.Close() }()

	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan %s plan line under %s plan: %v", probeName, planMode, err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s plan under %s plan: %v", probeName, planMode, err)
	}
	return plan.String(), groupCount
}

// assertChangedSinceTrimIndexState checks presence and, when present, that
// the index is both valid and ready.
func assertChangedSinceTrimIndexState(
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

func changedSinceTrimIndexDefinitions(t *testing.T) []Definition {
	t.Helper()

	var definitions []Definition
	for _, definition := range BootstrapDefinitions() {
		switch definition.Name {
		case "fact_records_changed_since_live_keys_idx",
			"fact_records_changed_since_tombstone_keys_idx":
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) != 2 {
		t.Fatalf("changed-since trim definitions found = %d, want 2", len(definitions))
	}
	return definitions
}

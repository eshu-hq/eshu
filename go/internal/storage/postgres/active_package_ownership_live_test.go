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

// Frozen pre-#11082 baseline of the ownership load, copied verbatim from the
// listActivePackageOwnershipFactsQuery const (facts_active_package_ownership.go
// at fc458eb345, the drive/11082-code-import-hang base) before the split. It
// is the "before" side of the #11082 differential: the shipped statements
// must return exactly this row set on the same corpus, and its generic plan
// (kind/tombstone/cursor-OR left as a Filter with rows removed scaling per
// page) is the regression the split plus migration 327 removes.
const legacyActivePackageOwnershipFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind IN (
    'package_registry.package',
    'reducer_package_ownership_correlation',
    'reducer_package_publication_correlation'
  )
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    $1::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// TestActivePackageOwnershipDriveFromFactLive proves migration 327
// (fact_records_package_ownership_active_idx) with the split first/cursor
// statements on a live Postgres against the #11082 shape: many generations
// sharing one observed_at each, plus inactive-generation target-kind rows
// interleaved in (observed_at, fact_id) order that a pre-join LIMIT would
// silently drop.
//
// It covers what the fake-based tests structurally cannot. Those pin
// statement shape; they cannot see a planner decision. The assertions that
// matter here are that the cursor statement seeks the partial index with the
// row comparison as an Index Cond under a GENERIC plan (the mode the services
// actually get through database/sql), that the full drain stays buffers
// bounded independent of corpus size, and that the drained row set is exactly
// the legacy row set including inactive-generation exclusion.
func TestActivePackageOwnershipDriveFromFactLive(t *testing.T) {
	const (
		schema    = "eshu_11082_package_ownership_live"
		indexName = "fact_records_package_ownership_active_idx"
		scopes    = 60
	)

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #11082 ownership proof")
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

	// The #11082 shape, scaled for CI: 60 scopes x (200 active-gen rows +
	// 200 superseded-gen rows) sharing one observed_at per scope, 20 active
	// target-kind rows per scope (1200 total), 25 inactive target-kind rows
	// per scope (1500 total) interleaved in (observed_at, fact_id) order, and
	// tombstoned file rows the partial index must skip.
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
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    active_generation_id TEXT NOT NULL
);
CREATE TABLE scope_generations (
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    status TEXT NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
SELECT 'scope-' || i, 'gen-' || i || '-active' FROM generate_series(0, 59) AS i;
INSERT INTO scope_generations (scope_id, generation_id, status)
SELECT 'scope-' || i, 'gen-' || i || '-active', 'active' FROM generate_series(0, 59) AS i
UNION ALL
SELECT 'scope-' || i, 'gen-' || i || '-sup', 'superseded' FROM generate_series(0, 59) AS i;
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, is_tombstone, payload
)
SELECT
    'fact-11082-' || lpad(s::text, 9, '0'),
    'scope-' || ((s - 1) / 400),
    CASE WHEN (s - 1) % 400 < 200 THEN 'gen-' || ((s - 1) / 400) || '-active'
         ELSE 'gen-' || ((s - 1) / 400) || '-sup' END,
    CASE
        WHEN (s - 1) % 400 < 200 AND (s - 1) % 400 < 20 THEN
            (ARRAY['package_registry.package',
                   'reducer_package_ownership_correlation',
                   'reducer_package_publication_correlation'])[((s - 1) % 400) % 3 + 1]
        WHEN (s - 1) % 400 >= 200 AND (s - 1) % 400 < 225 THEN
            (ARRAY['package_registry.package',
                   'reducer_package_ownership_correlation',
                   'reducer_package_publication_correlation'])[((s - 1) % 400) % 3 + 1]
        ELSE 'file' END,
    'key-11082-' || lpad(s::text, 9, '0'),
    'git',
    'fact-11082-' || lpad(s::text, 9, '0'),
    TIMESTAMPTZ '2026-01-01 00:00:00+00' + ((((s - 1) / 400)) || ' minutes')::interval,
    CASE WHEN (s - 1) % 400 >= 20 AND (s - 1) % 23 = 0 THEN TRUE ELSE FALSE END,
    jsonb_build_object('data', repeat('x', 800))
FROM generate_series(1, 24000) AS s;
ANALYZE fact_records;
`); err != nil {
		t.Fatalf("seed ownership fixture: %v", err)
	}

	definitions := ownershipIndexDefinitions(t)

	// First application, then an identical reapplication: CONCURRENTLY
	// IF NOT EXISTS must be a clean no-op the second time.
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
			t.Fatalf("apply %s pass %d: %v", indexName, pass, err)
		}
	}
	assertKeysetIndexState(ctx, t, db, schema, indexName, true)

	// Drain through the real paging loop: this exercises statement
	// selection, cursor advance, and the short-page stop, not just one page.
	store := NewFactStore(SQLDB{DB: db})
	loaded, err := store.ListActivePackageOwnershipFacts(ctx)
	if err != nil {
		t.Fatalf("drain ownership facts: %v", err)
	}
	if got, want := len(loaded), 1200; got != want {
		t.Fatalf("drained rows = %d, want %d (inactive generations must be excluded)", got, want)
	}
	for _, e := range loaded {
		if strings.HasSuffix(e.GenerationID, "-sup") {
			t.Fatalf("drained superseded-generation row %q", e.FactID)
		}
	}
	for i := 1; i < len(loaded); i++ {
		prev, curr := loaded[i-1], loaded[i]
		if prev.ObservedAt.After(curr.ObservedAt) ||
			(prev.ObservedAt.Equal(curr.ObservedAt) && prev.FactID >= curr.FactID) {
			t.Fatalf("drain out of order at %d", i)
		}
	}

	// Differential against the frozen legacy statement, drained with the
	// same short-page-stop loop production used before the split.
	legacy := drainLegacyOwnership(t, ctx, db)
	if len(legacy) != len(loaded) {
		t.Fatalf("legacy rows = %d, shipped rows = %d, want equal", len(legacy), len(loaded))
	}
	for i := range legacy {
		shipped := ownershipTupleKey(loaded[i].ObservedAt, loaded[i].FactID, loaded[i].FactKind, loaded[i].ScopeID, loaded[i].GenerationID)
		if legacy[i] != shipped {
			t.Fatalf("row %d differs: legacy %q shipped %q", i, legacy[i], shipped)
		}
	}

	// Plans under the generic plan the services actually run.
	deep := loaded[len(loaded)/2]
	assertOwnershipPlanSeeks(t, ctx, db, indexName, "first", "", "")
	assertOwnershipPlanSeeks(t, ctx, db, indexName, "cursor",
		deep.ObservedAt.UTC().Format(time.RFC3339Nano), deep.FactID)

	// Buffers-bounded full drain: every page's shared buffers must stay
	// bounded by the matching set, never by the corpus.
	var totalBuffers int64
	totalBuffers += explainOwnershipPageBuffers(t, ctx, db, false, "", "")
	for _, c := range ownershipPageCursors(loaded) {
		totalBuffers += explainOwnershipPageBuffers(t, ctx, db, true, c[0], c[1])
	}
	t.Logf("full drain shared buffers=%d over %d pages", totalBuffers, len(loaded)/listFactsByKindPageSize+1)
	// Calibrated 2026-11-21 on disposable Postgres 18: 5080 buffers for the
	// 1200-row/3-page drain above. The 3x ceiling leaves plan-choice margin
	// while staying two orders of magnitude below a corpus-scaled scan (the
	// folded statement spent 145647 buffers on a single 500-row page of the
	// 1.2M-row deciding fixture).
	if totalBuffers > 15000 {
		t.Fatalf("full drain shared buffers=%d, want <= 15000 (corpus-scaled scan)", totalBuffers)
	}
}

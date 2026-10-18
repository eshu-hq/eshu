// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8316 migration 198 lifecycle and seek proof: the deployable_unit_gap_admitted_anchor_idx partial index applies and reapplies cleanly, the per-pass latest-decision listing plans as an index seek over it with exact rows, and rollback plus rebuild round-trips; isolated schema from ESHU_POSTGRES_TEST_DSN; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestDeployableUnitGapAdmittedAnchorIndexLifecycleAndSeekLive proves
// migration 198 (deployable_unit_gap_admitted_anchor_idx) on a live
// Postgres against the per-pass listing it serves: the admitted-anchor
// probe over anchors with multi-generation decision history plus
// other-domain noise.
//
// It covers what the hermetic pinning test cannot: that the migration
// applies and reapplies cleanly (CONCURRENTLY IF NOT EXISTS), that the
// production listing plans as an index seek over the new partial index
// with exact rows, and that rollback and rebuild round-trip cleanly.
func TestDeployableUnitGapAdmittedAnchorIndexLifecycleAndSeekLive(t *testing.T) {
	const (
		indexName = "deployable_unit_gap_admitted_anchor_idx"
		anchors   = 1500
		gens      = 10
		noiseRows = 105000
	)

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #8316 gap-index lifecycle proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
	defer cancel()
	db, schema := openReducerFairnessDBWithSchema(t, ctx, dsn)

	definitions := gapAdmittedAnchorIndexDefinitions(t)

	// First application, then an identical reapplication: CONCURRENTLY
	// IF NOT EXISTS must be a clean no-op the second time.
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
			t.Fatalf("apply gap admitted-anchor index pass %d: %v", pass, err)
		}
	}
	assertChangedSinceTrimIndexState(ctx, t, db, schema, indexName, true)

	w := seedFingerprintGateWorld(t, ctx, db)
	for g := 1; g <= gens; g++ {
		advanceFingerprintGateScopeGeneration(t, ctx, db, w.a.scopeID, fmt.Sprintf("gen-idx-%d", g))
	}
	base := time.Now().UTC().Truncate(time.Second)

	// Bulk seed at the selectivity the index exists for: 1,500 anchors x
	// 10 generations admitted, one stale anchor admitted then rejected, and
	// ~105k other-domain noise rows. Per-row seeding helpers would take
	// minutes at this scale; generate_series keeps it to two statements.
	// decided_at is distinct per row so DISTINCT ON ordering is deterministic.
	if _, err := db.ExecContext(ctx, `
INSERT INTO admission_decisions (
    decision_id, domain, state, domain_state, scope_id, generation_id,
    anchor_kind, anchor_id, candidate_kind, candidate_id,
    confidence_score, confidence_bucket, confidence_basis,
    freshness_state, freshness_cause,
    redaction_state, redaction_reason, payload_version, decided_at, updated_at, canonical_write
)
SELECT 'adm-gap-idx-' || a || '-' || g, 'deployable_unit_correlation', 'admitted',
    'admitted-candidate', $1, 'gen-idx-' || g,
    'repository', 'repo-gap-idx-' || a, 'deployable_unit', 'adm-gap-idx-' || a || '-' || g,
    0.95, 'high', 'test-pack',
    'fresh', 'test-seed',
    'unredacted', '', 'v1',
    $2::timestamptz + ((g * 2000 + a) || ' seconds')::interval,
    $2::timestamptz + ((g * 2000 + a) || ' seconds')::interval,
    '{"Written":true}'::jsonb
FROM generate_series(1, $3) AS a
CROSS JOIN generate_series(1, $4) AS g`,
		w.a.scopeID, base, anchors, gens); err != nil {
		t.Fatalf("seed admitted anchors: %v", err)
	}
	seedGapAdmissionDecisionAt(t, ctx, db, w.a.scopeID, "gen-idx-1",
		"adm-gap-stale-admit", "repo-gap-stale", "admitted", `{"Written":true}`, base)
	seedGapAdmissionDecisionAt(t, ctx, db, w.a.scopeID, "gen-idx-10",
		"adm-gap-stale-reject", "repo-gap-stale", "rejected", `{"Written":true}`, base.Add(time.Hour))
	if _, err := db.ExecContext(ctx, `
INSERT INTO admission_decisions (
    decision_id, domain, state, domain_state, scope_id, generation_id,
    anchor_kind, anchor_id, candidate_kind, candidate_id,
    confidence_score, confidence_bucket, confidence_basis,
    freshness_state, freshness_cause,
    redaction_state, redaction_reason, payload_version, decided_at, updated_at, canonical_write
)
SELECT 'adm-gap-noise-' || i, 'unrelated_probe', 'admitted',
    'admitted-candidate', $1, 'gen-idx-1',
    'repository', 'repo-gap-noise', 'deployable_unit', 'adm-gap-noise-' || i,
    0.95, 'high', 'test-pack',
    'fresh', 'test-seed',
    'unredacted', '', 'v1', $2, $2,
    '{"Written":true}'::jsonb
FROM generate_series(1, $3) AS i`,
		w.a.scopeID, base, noiseRows); err != nil {
		t.Fatalf("seed noise rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE admission_decisions"); err != nil {
		t.Fatalf("analyze admission_decisions: %v", err)
	}

	store := NewRelationshipStore(SQLDB{DB: db})
	assertGapAdmittedAnchorSeek(ctx, t, db, store, indexName, true, anchors)

	// Rollback from a populated, previously-indexed store.
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+indexName); err != nil {
		t.Fatalf("rollback %s: %v", indexName, err)
	}
	assertChangedSinceTrimIndexState(ctx, t, db, schema, indexName, false)
	assertGapAdmittedAnchorSeek(ctx, t, db, store, indexName, false, anchors)

	// Reapplying after a rollback must rebuild cleanly.
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("rebuild gap admitted-anchor index after rollback: %v", err)
	}
	assertChangedSinceTrimIndexState(ctx, t, db, schema, indexName, true)
	assertGapAdmittedAnchorSeek(ctx, t, db, store, indexName, true, anchors)
}

// assertGapAdmittedAnchorSeek runs the production admitted-anchor listing,
// checks the exact row count, and checks the plan uses (or, after rollback,
// does not use) the gap index. Rows stay exact in both cases: the index
// changes the access path, never the answer.
func assertGapAdmittedAnchorSeek(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	store *RelationshipStore,
	indexName string,
	wantIndex bool,
	wantAnchors int,
) {
	t.Helper()

	anchors, err := store.ListAllDeployableUnitAdmittedAnchorRepos(ctx)
	if err != nil {
		t.Fatalf("ListAllDeployableUnitAdmittedAnchorRepos() error = %v", err)
	}
	if len(anchors) != wantAnchors {
		t.Fatalf("admitted anchors = %d, want %d", len(anchors), wantAnchors)
	}

	var plan strings.Builder
	rows, err := db.QueryContext(ctx, "EXPLAIN "+backfill.ListAllDeployableUnitAdmittedAnchorReposQuery)
	if err != nil {
		t.Fatalf("explain admitted-anchor listing: %v", err)
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan plan line: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	usesIndex := strings.Contains(plan.String(), indexName)
	if usesIndex != wantIndex {
		t.Fatalf("listing plan uses %s = %t, want %t:\n%s", indexName, usesIndex, wantIndex, plan.String())
	}
	if wantIndex && strings.Contains(plan.String(), "Seq Scan on admission_decisions") {
		t.Fatalf("listing seq-scans despite %s:\n%s", indexName, plan.String())
	}
}

func gapAdmittedAnchorIndexDefinitions(t *testing.T) []Definition {
	t.Helper()

	var definitions []Definition
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == "deployable_unit_gap_admitted_anchor_idx" {
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) != 1 {
		t.Fatalf("gap admitted-anchor index definitions found = %d, want 1", len(definitions))
	}
	return definitions
}

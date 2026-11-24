// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The byte-identical differential of PR-C2 h (arbiter-prc-design section 5): the supports the writer's in-memory
// upsert leaves for a set of rows equal, column for column, what the fact-derived upsert (the backfill's)
// leaves for the same facts. The facts are planted with raw SQL (the pre-C2 model, so the trigger is
// disarmed for the test); the fact-derived side is ProofSyncSupportsFromFacts, the writer's own fact-derived
// statements; the in-memory side is UpsertImpactSupportRows through the real transaction wrapper.

const differentialFactsSQL = `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
    source_system, source_fact_key, source_confidence, observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'diff:' || g, $1, $2, $3, 'key:' || g, '1.0.0', 'synthetic', 'diff:' || g,
    CASE WHEN g % 3 = 0 THEN 'inferred' ELSE 'reported' END, now(), now(), FALSE,
    jsonb_strip_nulls(jsonb_build_object(
        'cve_id', CASE WHEN g % 5 = 0 THEN '' ELSE 'CVE-2026-' || lpad(g::text, 4, '0') END,
        'advisory_id', 'GHSA-' || g,
        'package_id', 'pkg:npm/p' || (g % 7),
        'repository_id', CASE WHEN g % 4 = 0 THEN NULL ELSE 'repository:r' || (g % 3) END,
        'subject_digest', CASE WHEN g % 2 = 0 THEN 'sha256:' || g ELSE NULL END,
        'image_ref', CASE WHEN g % 6 = 0 THEN 'registry.example/app:' || g ELSE NULL END,
        'ecosystem', CASE WHEN g % 2 = 0 THEN 'NPM' ELSE 'npm' END,
        'impact_status', 'affected_exact', 'priority_score', (g * 7) % 100, 'priority_bucket', 'high',
        'observed_version', '1.' || g, 'match_reason', 'npm_semver_affected_range',
        'detection_profile', 'precise', 'suppression_state', 'active',
        'provenance', jsonb_build_object('advisory_group_members', jsonb_build_array('CVE-2026-' || g)),
        'pad', repeat(md5(g::text), 4))), 1
FROM generate_series(1, 60) AS g`

func TestSupplyChainImpactSupportsRowsUpsertEqualsFactDerivedLive(t *testing.T) {
	ctx, db := openPreC2ReplaceSetLiveDB(t)
	kind := facts.ReducerSupplyChainImpactFindingFactKind
	if _, err := db.ExecContext(ctx, differentialFactsSQL, replaceSetLiveScope, replaceSetLiveGeneration, kind); err != nil {
		t.Fatalf("plant finding facts: %v", err)
	}
	if err := postgres.ProofSyncSupportsFromFacts(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("fact-derived supports: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE supports_from_facts AS SELECT * FROM supply_chain_impact_finding_supports`); err != nil {
		t.Fatalf("copy the fact-derived supports: %v", err)
	}
	rows := differentialRows(t, ctx, db)
	if len(rows) != 60 {
		t.Fatalf("read %d fact rows, want 60", len(rows))
	}

	writeRows := func(rows []factwrite.VersionedRow) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports`); err != nil {
			t.Fatalf("clear supports: %v", err)
		}
		tx, err := (postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}}).BeginSupplyChainImpactTx(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := supplychaincore.WriteSupplyChainImpactFindingRows(ctx, tx, rows); err != nil {
			_ = tx.Rollback()
			t.Fatalf("write the supports from the rows: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	diff := func() int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM (SELECT * FROM supply_chain_impact_finding_supports EXCEPT ALL SELECT * FROM supports_from_facts) a)
     + (SELECT count(*) FROM (SELECT * FROM supports_from_facts EXCEPT ALL SELECT * FROM supply_chain_impact_finding_supports) b)`).Scan(&n); err != nil {
			t.Fatalf("compare the supports: %v", err)
		}
		return n
	}

	writeRows(rows)
	if n := diff(); n != 0 {
		t.Fatalf("%d support rows differ between the in-memory upsert and the fact-derived upsert, want 0", n)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports`).Scan(&count); err != nil || count != 60 {
		t.Fatalf("supports = %d (err %v), want 60: the comparison would pass on empty tables", count, err)
	}

	// Seeded violation: one row's payload changed in memory must show as a difference.
	planted := append([]factwrite.VersionedRow(nil), rows...)
	planted[0].Payload = `{"cve_id":"CVE-PLANTED","package_id":"pkg:npm/planted"}`
	writeRows(planted)
	if n := diff(); n == 0 {
		t.Fatal("a changed payload was not reported: the comparison cannot bite")
	}
}

// differentialRows reads the planted facts back as the writer's rows: the same columns the finding row carries.
func differentialRows(t *testing.T, ctx context.Context, db *sql.DB) []factwrite.VersionedRow {
	t.Helper()
	cursor, err := db.QueryContext(ctx, `
SELECT fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_confidence, payload::text
FROM fact_records WHERE fact_kind = $1 ORDER BY fact_id`, facts.ReducerSupplyChainImpactFindingFactKind)
	if err != nil {
		t.Fatalf("read the planted facts: %v", err)
	}
	defer func() { _ = cursor.Close() }()
	var rows []factwrite.VersionedRow
	for cursor.Next() {
		var row factwrite.VersionedRow
		if err := cursor.Scan(&row.FactID, &row.ScopeID, &row.GenerationID, &row.FactKind, &row.StableFactKey, &row.SourceConfidence, &row.Payload); err != nil {
			t.Fatalf("scan a planted fact: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

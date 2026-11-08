// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestCVEListFullShardWriterLive drives the real Postgres writer at
// full-shard volume for eshu-hq/eshu#10354 (Phase 5 5.2, criterion 1
// writer layer): 334 cve_list CVEs (≈1,000 fact rows) through
// WriteSupplyChainImpactFindings on a disposable database. It records
// wall-clock and row counts, proves idempotent replay (stable row count,
// empty change set), exercises the retraction statement with a dropped-CVE
// rescan, and EXPLAINs the shipped versioned-upsert statement with
// arguments read back from the writer's own rows (derived, never
// hand-copied). The retraction statement text is unexported, so its plan
// is covered by execution (non-zero retracted count) rather than EXPLAIN;
// the evidence note records that boundary explicitly.
func TestCVEListFullShardWriterLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "cve-shard", "g2", "g1")
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
	}

	cves := make([]string, 0, 334)
	for i := 0; i < 334; i++ {
		cves = append(cves, fmt.Sprintf("CVE-2024-%04d", 1000+i))
	}
	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, len(cves))
	for _, cve := range cves {
		findings = append(findings, fanOutLiveFinding(cve))
	}

	start := time.Now()
	result, err := writer.WriteSupplyChainImpactFindings(ctx, supplychaincore.SupplyChainImpactWrite{
		IntentID: "intent-cve-shard-g1", ScopeID: "cve-shard", GenerationID: fanOutGen("cve-shard", "g1"),
		SourceSystem: "synthetic", Cause: "5.2 full-shard proof", Findings: findings, FencingToken: 1,
	})
	writeElapsed := time.Since(start)
	if err != nil {
		t.Fatalf("full-shard write: %v", err)
	}
	if len(result.ChangedCVEIDs) != len(cves) {
		t.Fatalf("first pass changed %d CVEs, want all %d", len(result.ChangedCVEIDs), len(cves))
	}
	if result.FactsWritten == 0 {
		t.Fatal("first pass FactsWritten = 0, want the written row count")
	}
	t.Logf("5.2 full-shard write: %d findings, %d fact rows in %v", len(findings), result.FactsWritten, writeElapsed)

	var stored int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE scope_id = 'cve-shard' AND generation_id = $1`, fanOutGen("cve-shard", "g1")).Scan(&stored); err != nil {
		t.Fatalf("count stored rows: %v", err)
	}
	if stored != result.FactsWritten {
		t.Fatalf("stored rows = %d, writer reported FactsWritten = %d", stored, result.FactsWritten)
	}

	// Identical replay: empty change set, stable row count (idempotent).
	replay, err := writer.WriteSupplyChainImpactFindings(ctx, supplychaincore.SupplyChainImpactWrite{
		IntentID: "intent-cve-shard-g1-replay", ScopeID: "cve-shard", GenerationID: fanOutGen("cve-shard", "g1"),
		SourceSystem: "synthetic", Cause: "5.2 idempotency replay", Findings: findings, FencingToken: 2,
	})
	if err != nil {
		t.Fatalf("replay write: %v", err)
	}
	if len(replay.ChangedCVEIDs) != 0 {
		t.Fatalf("replay changed = %v, want none", replay.ChangedCVEIDs)
	}
	var storedAfter int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE scope_id = 'cve-shard' AND generation_id = $1`, fanOutGen("cve-shard", "g1")).Scan(&storedAfter); err != nil {
		t.Fatalf("count rows after replay: %v", err)
	}
	if storedAfter != stored {
		t.Fatalf("rows after replay = %d, want stable %d", storedAfter, stored)
	}

	// Same-generation rewrite that drops 34 CVEs exercises the retraction
	// statement for real: retraction tombstones rows of the same
	// (scope, generation) this pass no longer derives.
	kept := findings[:300]
	rescan, err := writer.WriteSupplyChainImpactFindings(ctx, supplychaincore.SupplyChainImpactWrite{
		IntentID: "intent-cve-shard-g1-rewrite", ScopeID: "cve-shard", GenerationID: fanOutGen("cve-shard", "g1"),
		SourceSystem: "synthetic", Cause: "5.2 retraction proof", Findings: kept, FencingToken: 3,
	})
	if err != nil {
		t.Fatalf("rescan write: %v", err)
	}
	if rescan.FactsRetracted == 0 {
		t.Fatal("rescan FactsRetracted = 0, want the dropped-CVE tombstones")
	}
	t.Logf("5.2 rescan: retracted %d rows for 34 dropped CVEs", rescan.FactsRetracted)

	// EXPLAIN the shipped versioned-upsert statement with arguments read
	// back from the writer's own rows: derived evidence, no hand copy.
	var factIDs, scopeIDs, genIDs, kinds, keys, versions, collectors, confidences, systems, factKeys, uris, recordIDs []string
	var observed, ingested []time.Time
	var tombs []bool
	var payloads []string
	var tokens []int64
	rows, err := db.QueryContext(ctx, `SELECT fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at, ingested_at, is_tombstone, payload::text, fencing_token FROM fact_records WHERE scope_id = 'cve-shard' AND generation_id = $1`, fanOutGen("cve-shard", "g1"))
	if err != nil {
		t.Fatalf("read back written rows: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var factID, scopeID, genID, kind, key, version, collector, confidence, system, factKey, uri, recordID sql.NullString
		var observ, ingest sql.NullTime
		var tomb sql.NullBool
		var payload sql.NullString
		var token sql.NullInt64
		if err := rows.Scan(&factID, &scopeID, &genID, &kind, &key, &version, &collector, &confidence, &system, &factKey, &uri, &recordID, &observ, &ingest, &tomb, &payload, &token); err != nil {
			t.Fatalf("scan written row: %v", err)
		}
		factIDs, scopeIDs, genIDs, kinds, keys, versions, collectors, confidences, systems, factKeys, uris, recordIDs = append(factIDs, factID.String), append(scopeIDs, scopeID.String), append(genIDs, genID.String), append(kinds, kind.String), append(keys, key.String), append(versions, version.String), append(collectors, collector.String), append(confidences, confidence.String), append(systems, system.String), append(factKeys, factKey.String), append(uris, uri.String), append(recordIDs, recordID.String)
		observed, ingested, tombs, payloads, tokens = append(observed, observ.Time), append(ingested, ingest.Time), append(tombs, tomb.Bool), append(payloads, payload.String), append(tokens, token.Int64)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(factIDs) == 0 {
		t.Fatal("no written rows to derive upsert arguments from")
	}
	args := []any{factIDs, scopeIDs, genIDs, kinds, keys, versions, collectors, confidences, systems, factKeys, uris, recordIDs, observed, ingested, tombs, payloads, tokens}
	planRows, err := db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+factwrite.BatchInsertVersionedQuery, args...)
	if err != nil {
		t.Fatalf("explain upsert: %v", err)
	}
	defer planRows.Close()
	var plan []string
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := planRows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	if len(plan) == 0 {
		t.Fatal("empty upsert plan")
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "fact_records") {
		t.Fatalf("upsert plan names no fact_records access:\n%s", joined)
	}
	for _, line := range plan {
		t.Logf("5.2 upsert plan: %s", line)
	}

	var pgVersion string
	if err := db.QueryRowContext(ctx, `SELECT version()`).Scan(&pgVersion); err != nil {
		t.Fatalf("pg version: %v", err)
	}
	t.Logf("5.2 writer proof backend: %s", pgVersion)
}

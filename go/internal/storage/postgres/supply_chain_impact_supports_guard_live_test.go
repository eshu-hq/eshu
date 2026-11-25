// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The supports write-on-change guard ignores the per-pass payload keys intent_id and cause
// (#10880 B3a, arbiter ruling 2026-11-24). Every pass stamps its own intent id and cause into
// the finding payload, so an identical replay used to differ from every stored row and
// rewrote all of them (measured in the B3 lab: 10,000 of 10,000 supports rows). These proofs
// run the real writer at 10,000 findings and count rewritten supports rows by xmin.

const supportsGuardLiveRows = 10000

// supportsGuardFindings returns n distinct findings, each with two evidence facts.
func supportsGuardFindings(n int) []supplychaincore.SupplyChainImpactFinding {
	out := make([]supplychaincore.SupplyChainImpactFinding, 0, n)
	for i := 0; i < n; i++ {
		finding := replaceSetFinding(fmt.Sprintf("repository:r_b3a_%d", i))
		finding.CVSSScore = 5 + float64(i%50)/10
		finding.FixedVersion = fmt.Sprintf("3.0.%d-1", i%40)
		finding.EvidenceFactIDs = []string{fmt.Sprintf("fact:b3a:evidence:%d:a", i), fmt.Sprintf("fact:b3a:evidence:%d:b", i)}
		out = append(out, finding)
	}
	return out
}

// supportsGuardSnapshot records the row version (xmin) of every support, so a later count of
// changed xmin values is the number of rows a pass rewrote.
func supportsGuardSnapshot(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`DROP TABLE IF EXISTS b3a_supports_snapshot`,
		`CREATE TABLE b3a_supports_snapshot AS SELECT fact_id, xmin::text::bigint AS xid FROM supply_chain_impact_finding_supports`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("snapshot supports: %v", err)
		}
	}
}

// supportsGuardRewritten returns how many supports the pass since the last snapshot rewrote
// and the total number of supports.
func supportsGuardRewritten(t *testing.T, ctx context.Context, db *sql.DB) (rewritten, total int) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE s.xmin::text::bigint IS DISTINCT FROM b.xid), count(*)
FROM supply_chain_impact_finding_supports AS s
LEFT JOIN b3a_supports_snapshot AS b USING (fact_id)`).Scan(&rewritten, &total); err != nil {
		t.Fatalf("count rewritten supports: %v", err)
	}
	return rewritten, total
}

// TestSupplyChainImpactSupportsGuardChangeMatrixLive is the B3a change matrix at 10,000 rows:
// an identical replay rewrites no support, a content change rewrites exactly the changed rows,
// an evidence-only change (a digest-excluded key with value readers) rewrites exactly the changed
// rows, the same evidence set in another order rewrites none, and an identical pass after the
// changes rewrites none. The guard must still write when it must.
func TestSupplyChainImpactSupportsGuardChangeMatrixLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)
	findings := supportsGuardFindings(supportsGuardLiveRows)
	pass := func(label string) (rewritten, total int) {
		t.Helper()
		supportsGuardSnapshot(t, ctx, db)
		if _, err := writer.WriteSupplyChainImpactFindings(ctx, replaceSetWrite("intent:b3a:"+label, findings...)); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		return supportsGuardRewritten(t, ctx, db)
	}

	if rewritten, total := pass("first-write"); total != supportsGuardLiveRows || rewritten != supportsGuardLiveRows {
		t.Fatalf("first write: %d of %d supports new or changed, want all %d", rewritten, total, supportsGuardLiveRows)
	}
	if rewritten, total := pass("identical"); rewritten != 0 || total != supportsGuardLiveRows {
		t.Fatalf("identical replay rewrote %d of %d supports, want 0: the per-pass keys must not drive a rewrite", rewritten, total)
	}
	for i := 0; i < 100; i++ {
		findings[i].CVSSScore += 0.5
		findings[i].FixedVersion += "-patched"
	}
	if rewritten, _ := pass("content-change"); rewritten != 100 {
		t.Fatalf("a content change in 100 findings rewrote %d supports, want exactly 100", rewritten)
	}
	// A row the pass changed is byte-equal to its fact (it carries this pass's intent id and
	// cause); a row the guard left equals the fact apart from those two keys.
	var changedNotVerbatim, leftNotEqual int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE s.xmin::text::bigint <> b.xid AND s.payload::text IS DISTINCT FROM f.payload::text),
       count(*) FILTER (WHERE s.xmin::text::bigint = b.xid
         AND (s.payload - 'intent_id' - 'cause')::text IS DISTINCT FROM (f.payload - 'intent_id' - 'cause')::text)
FROM supply_chain_impact_finding_supports AS s
JOIN b3a_supports_snapshot AS b USING (fact_id)
JOIN fact_records AS f ON f.fact_id = s.fact_id`).Scan(&changedNotVerbatim, &leftNotEqual); err != nil {
		t.Fatalf("compare supports with facts: %v", err)
	}
	if changedNotVerbatim != 0 || leftNotEqual != 0 {
		t.Fatalf("changed rows not byte-equal to their fact: %d; unchanged rows differing beyond intent_id and cause: %d; want 0 and 0", changedNotVerbatim, leftNotEqual)
	}
	for i := 100; i < 200; i++ {
		findings[i].EvidenceFactIDs = append(append([]string(nil), findings[i].EvidenceFactIDs...), fmt.Sprintf("fact:b3a:evidence:%d:c", i))
	}
	if rewritten, _ := pass("evidence-only"); rewritten != 100 {
		t.Fatalf("an evidence-only change in 100 findings rewrote %d supports, want exactly 100", rewritten)
	}
	for i := 200; i < 300; i++ {
		ids := findings[i].EvidenceFactIDs
		findings[i].EvidenceFactIDs = []string{ids[1], ids[0]}
	}
	if rewritten, _ := pass("reorder"); rewritten != 0 {
		t.Fatalf("the same evidence set in another order rewrote %d supports, want 0", rewritten)
	}
	if rewritten, _ := pass("identical-again"); rewritten != 0 {
		t.Fatalf("an identical pass after the changes rewrote %d supports, want 0", rewritten)
	}
}

// TestSupplyChainImpactSupportsGuardTransitionLive proves the first pass after the code change
// costs nothing: supports written by the C1 statement (which compares the payload verbatim and
// so carries the latest pass's intent id and cause) are not rewritten by one identical pass of
// the new statement.
func TestSupplyChainImpactSupportsGuardTransitionLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)
	findings := supportsGuardFindings(supportsGuardLiveRows)
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, replaceSetWrite("intent:b3a:transition-1", findings...)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// A second pass moves the facts to a new intent and cause; the new guard leaves the supports
	// on the first. Bring the supports to what the C1 statement would have left (the latest
	// intent and cause), as a C1 writer would have on this same pass.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, replaceSetWrite("intent:b3a:transition-2", findings...)); err != nil {
		t.Fatalf("second write: %v", err)
	}
	ids := supportsLiveFactIDs(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration)
	inserted, updated, err := postgres.ProofUpsertSupports(ctx, postgres.SQLDB{DB: db}, replaceSetLiveScope, replaceSetLiveGeneration, ids, false)
	if err != nil || inserted != 0 || updated != int64(len(ids)) {
		t.Fatalf("C1 statement over the second pass: inserted %d updated %d err %v, want 0 inserted and all %d updated (it rewrites on the intent id)", inserted, updated, err, len(ids))
	}
	supportsGuardSnapshot(t, ctx, db)
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, replaceSetWrite("intent:b3a:transition-3", findings...)); err != nil {
		t.Fatalf("third write: %v", err)
	}
	if rewritten, total := supportsGuardRewritten(t, ctx, db); rewritten != 0 || total != supportsGuardLiveRows {
		t.Fatalf("one identical pass of the new statement over C1-written supports rewrote %d of %d, want 0", rewritten, total)
	}
}

// TestSupplyChainImpactSupportsGuardMixedVersionLive proves an old-version and a new-version
// writer alternating on one (scope, generation) converge: after an old-version upsert the
// supports equal the facts, and the new statement never rewrites a row whose content did not
// change, whichever version wrote the stored row.
func TestSupplyChainImpactSupportsGuardMixedVersionLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)
	const rows = 2000
	findings := supportsGuardFindings(rows)
	write := func(label string) {
		t.Helper()
		if _, err := writer.WriteSupplyChainImpactFindings(ctx, replaceSetWrite("intent:b3a:mixed-"+label, findings...)); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	oldWriter := func(label string) {
		t.Helper()
		ids := supportsLiveFactIDs(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration)
		if _, _, err := postgres.ProofUpsertSupports(ctx, postgres.SQLDB{DB: db}, replaceSetLiveScope, replaceSetLiveGeneration, ids, false); err != nil {
			t.Fatalf("old-version upsert %s: %v", label, err)
		}
	}
	payloadsEqualFacts := func(label string) {
		t.Helper()
		var differing int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_finding_supports AS s
JOIN fact_records AS f ON f.fact_id = s.fact_id
WHERE s.payload IS DISTINCT FROM f.payload`).Scan(&differing); err != nil {
			t.Fatalf("compare supports with facts: %v", err)
		}
		if differing != 0 {
			t.Fatalf("%s: %d supports differ from their fact payload, want 0 after an old-version upsert", label, differing)
		}
	}
	newPassRewrites := func(label string) int {
		t.Helper()
		supportsGuardSnapshot(t, ctx, db)
		write(label)
		rewritten, total := supportsGuardRewritten(t, ctx, db)
		if total != rows {
			t.Fatalf("%s: %d supports, want %d", label, total, rows)
		}
		return rewritten
	}

	write("1")
	for round := 2; round <= 4; round++ {
		if got := newPassRewrites(fmt.Sprintf("new-%d", round)); got != 0 {
			t.Fatalf("round %d: the new statement rewrote %d unchanged supports, want 0", round, got)
		}
		oldWriter(fmt.Sprintf("round-%d", round))
		payloadsEqualFacts(fmt.Sprintf("round %d", round))
	}
	// A real change through the new statement still reaches the stored row, and an old-version
	// upsert afterwards brings the rest of the payload keys level.
	findings[0].CVSSScore += 1
	supportsGuardSnapshot(t, ctx, db)
	write("change")
	if rewritten, _ := supportsGuardRewritten(t, ctx, db); rewritten != 1 {
		t.Fatalf("a content change in one finding rewrote %d supports, want exactly 1", rewritten)
	}
	oldWriter("final")
	payloadsEqualFacts("final")
}

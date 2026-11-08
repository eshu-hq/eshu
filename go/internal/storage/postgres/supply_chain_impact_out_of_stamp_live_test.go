// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// outOfStampActiveRows reads the (cve_id, package_id) pairs of one
// generation's active finding rows, in stable order.
func outOfStampActiveRows(t *testing.T, ctx context.Context, db *sql.DB, scope, generation string) [][2]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT payload->>'cve_id', COALESCE(payload->>'package_id', '')
FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND is_tombstone = FALSE
ORDER BY 1, 2`,
		facts.ReducerSupplyChainImpactFindingFactKind, scope, generation)
	if err != nil {
		t.Fatalf("read active rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out [][2]string
	for rows.Next() {
		var cve, pkg string
		if err := rows.Scan(&cve, &pkg); err != nil {
			t.Fatalf("scan active row: %v", err)
		}
		out = append(out, [2]string{cve, pkg})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read active rows: %v", err)
	}
	return out
}

// outOfStampRowTombstoned reports whether one finding row is tombstoned.
func outOfStampRowTombstoned(t *testing.T, ctx context.Context, db *sql.DB, factID string) bool {
	t.Helper()
	var tombstoned bool
	if err := db.QueryRowContext(ctx, `SELECT is_tombstone FROM fact_records WHERE fact_id = $1`, factID).Scan(&tombstoned); err != nil {
		t.Fatalf("read row %s: %v", factID, err)
	}
	return tombstoned
}

// fallbackFinding builds a minimal valid finding for the convergence proof.
func fallbackFinding(cve, pkg string) supplychaincore.SupplyChainImpactFinding {
	return supplychaincore.SupplyChainImpactFinding{
		CVEID:           cve,
		PackageID:       pkg,
		Ecosystem:       "npm",
		ObservedVersion: "1.0.0",
		Status:          supplychaincore.SupplyChainImpactAffectedExact,
	}
}

// fallbackWrite builds a writer input for one scope, mirroring
// replaceSetWrite: a strictly increasing fencing token per write, like the
// production sequence.
func fallbackWrite(intentID, scope, generation string, scoped []string, findings ...supplychaincore.SupplyChainImpactFinding) supplychaincore.SupplyChainImpactWrite {
	return supplychaincore.SupplyChainImpactWrite{
		FencingToken: replaceSetNextToken(),
		IntentID:     intentID,
		ScopeID:      scope,
		GenerationID: generation,
		SourceSystem: "vulnerability_intelligence",
		Cause:        "synthetic #10415 fallback proof",
		Findings:     findings,
		ScopedCVEIDs: scoped,
	}
}

// TestHasOutOfStampActiveRowsLive pins the #10415 trigger probe: an active
// row outside the stamp reports true; a stamp covering every active CVE, an
// empty stamp, a tombstoned copy, an empty-CVE row, and another generation's
// rows all report false.
func TestHasOutOfStampActiveRowsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "stamp-scope", "gen-2", "gen-1")
	seedScopedFindingRow(t, ctx, db, "stamp-scope", "gen-2", "row-keep", "CVE-2026-9001", "DSA-9001-1", "pkg-a", "pkg:npm/a@1", "a", "npm", false)
	seedScopedFindingRow(t, ctx, db, "stamp-scope", "gen-2", "row-drift", "CVE-2026-9002", "DSA-9002-1", "pkg-b", "pkg:npm/b@1", "b", "npm", false)
	seedScopedFindingRow(t, ctx, db, "stamp-scope", "gen-2", "row-tomb", "CVE-2026-9003", "DSA-9003-1", "pkg-t", "pkg:npm/t@1", "t", "npm", true)
	seedScopedFindingRow(t, ctx, db, "stamp-scope", "gen-2", "row-empty-cve", "", "DSA-9000-1", "pkg-e", "pkg:npm/e@1", "e", "npm", false)
	seedScopedFindingRow(t, ctx, db, "stamp-scope", "gen-1", "row-old", "CVE-2026-9009", "DSA-9009-1", "pkg-o", "pkg:npm/o@1", "o", "npm", false)

	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	gen := fanOutGen("stamp-scope", "gen-2")

	if has, err := store.HasOutOfStampActiveSupplyChainImpactRows(ctx, "stamp-scope", gen, []string{"CVE-2026-9001"}); err != nil || !has {
		t.Fatalf("drift stamp probe = %v err = %v, want true (CVE-2026-9002 drifts outside)", has, err)
	}
	if has, err := store.HasOutOfStampActiveSupplyChainImpactRows(ctx, "stamp-scope", gen, []string{"CVE-2026-9001", "CVE-2026-9002"}); err != nil || has {
		t.Fatalf("covering stamp probe = %v err = %v, want false", has, err)
	}
	if has, err := store.HasOutOfStampActiveSupplyChainImpactRows(ctx, "stamp-scope", gen, nil); err != nil || has {
		t.Fatalf("empty stamp probe = %v err = %v, want false without a read", has, err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'row-drift'`); err != nil {
		t.Fatalf("tombstone drift row: %v", err)
	}
	if has, err := store.HasOutOfStampActiveSupplyChainImpactRows(ctx, "stamp-scope", gen, []string{"CVE-2026-9001"}); err != nil || has {
		t.Fatalf("post-reap probe = %v err = %v, want false (tombstoned, empty-CVE, and older-generation rows never trigger)", has, err)
	}
}

// activeRowPairs is the comparable active-set shape for the convergence
// proof: CVE and package pairs, sorted.
func activeRowPairs(rows [][2]string) [][2]string {
	out := append([][2]string(nil), rows...)
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out
}

// TestScopedFallbackConvergesLive pins acceptance criterion (1) against real
// SQL: a holder whose stamp names CVE-2026-9001 but still carries an active
// CVE-2026-9002 row keeps the stale row under a scoped write and reaps it
// under the fallback (full) write, landing on exactly the active set a
// direct full pass writes.
func TestScopedFallbackConvergesLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "fb-scope", "gen-2")
	fanOutSeedScope(t, ctx, db, "ref-scope", "gen-2")
	fbGen := fanOutGen("fb-scope", "gen-2")
	refGen := fanOutGen("ref-scope", "gen-2")

	// The drift: an active CVE-2026-9002 row no pass derives anymore, next
	// to nothing else. The trigger must see it while the stamp names only
	// CVE-2026-9001.
	seedScopedFindingRow(t, ctx, db, "fb-scope", "gen-2", "stale-row", "CVE-2026-9002", "DSA-9002-1", "pkg-b", "pkg:npm/b@1", "b", "npm", false)

	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return replaceSetLiveNow },
	}
	stamp := []string{"CVE-2026-9001"}
	if has, err := store.HasOutOfStampActiveSupplyChainImpactRows(ctx, "fb-scope", fbGen, stamp); err != nil || !has {
		t.Fatalf("trigger probe = %v err = %v, want true before any write", has, err)
	}

	keep := fallbackFinding("CVE-2026-9001", "pkg:npm/a@1")

	// The scoped path the pre-fallback code takes: only the stamped CVE is
	// republished and only the stamp is retracted, so the stale row lingers.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, fallbackWrite("intent:10415:scoped", "fb-scope", fbGen, stamp, keep)); err != nil {
		t.Fatalf("scoped write: %v", err)
	}
	if outOfStampRowTombstoned(t, ctx, db, "stale-row") {
		t.Fatal("scoped write reaped the out-of-stamp row: the pre-fallback behavior is gone")
	}

	// The fallback the trigger selects: a full write of the same findings.
	// The stale row is reaped and the trigger clears.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, fallbackWrite("intent:10415:fallback", "fb-scope", fbGen, nil, keep)); err != nil {
		t.Fatalf("fallback write: %v", err)
	}
	if !outOfStampRowTombstoned(t, ctx, db, "stale-row") {
		t.Fatal("fallback write left the out-of-stamp row active: no convergence")
	}
	if has, err := store.HasOutOfStampActiveSupplyChainImpactRows(ctx, "fb-scope", fbGen, stamp); err != nil || has {
		t.Fatalf("post-fallback probe = %v err = %v, want false", has, err)
	}

	// The reference: a direct full pass over the same findings on a clean
	// scope. The fallback's active set must equal it exactly.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, fallbackWrite("intent:10415:reference", "ref-scope", refGen, nil, keep)); err != nil {
		t.Fatalf("reference full write: %v", err)
	}
	fbActive := activeRowPairs(outOfStampActiveRows(t, ctx, db, "fb-scope", fbGen))
	refActive := activeRowPairs(outOfStampActiveRows(t, ctx, db, "ref-scope", refGen))
	if len(fbActive) == 0 || len(refActive) == 0 {
		t.Fatal("empty active set: the proof writes nothing to compare")
	}
	if !reflect.DeepEqual(fbActive, refActive) {
		t.Fatalf("fallback active set %v differs from full-pass reference %v", fbActive, refActive)
	}
}

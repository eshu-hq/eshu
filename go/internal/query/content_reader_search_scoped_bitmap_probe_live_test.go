// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7930 disposable PostgreSQL 18 proof of the rare/empty single-scope BitmapAnd investigation (probe B; probe A retired under #10607 per the arbiter probe-A disposition); one local PostgreSQL 18 run took about 6 s
//eshu:readiness:env ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE ESHU_SCOPED_SEARCH_PROOF_DSN

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// This file holds the surviving #7930 AUTHORIZED probe (B): for still-red
// rare/empty single-scope cells it investigates whether the generic plan
// already finds a BitmapAnd the custom plan misses. Investigation only:
// whatever the answer, no serving shape changes here (steering is
// REDIRECTED). Probe (A), the per-repo UNION ALL of single-scope legs, was
// retired under #10607 per the arbiter probe-A disposition: already
// no-benefit-killed pre-snippets, and the snippet-era KILL measured
// unbounded-leg artifact plus targetlist snippets, not UNION ALL work. Probe
// (B) never changed serving shapes: it runs measurement-only against the
// disposable fixture.

// planHasNode reports whether any executed node has the given type,
// optionally restricted to a substring of its index name.
func planHasNode(stats scopedWorkStats, nodeType, indexSubstr string) bool {
	for _, shape := range stats.shapes {
		parts := strings.SplitN(shape, "/", 2)
		if len(parts) != 2 || parts[0] != nodeType {
			continue
		}
		if indexSubstr == "" || strings.Contains(parts[1], indexSubstr) {
			return true
		}
	}
	return false
}

// TestRareEmptySingleScopeBitmapInvestigationLive is probe (B): for still-red
// rare/empty single-scope cells it compares the custom plan against the
// generic plan and asks whether the generic plan already finds a BitmapAnd
// the custom plan misses. Investigation only: whatever the answer, no serving
// shape changes here (steering is REDIRECTED). A cell whose generic plan
// holds no BitmapAnd has nothing to steer toward.
func TestRareEmptySingleScopeBitmapInvestigationLive(t *testing.T) {
	ctx, db := openScopedWorkboundFixture(t)

	// probeBAuthorized restricts probe (B) to its ruling scope: still-red
	// rare/empty single-scope terms. A still-red cell outside that scope
	// (e.g. a medium term) is reported as residual with no authorized probe.
	probeBAuthorized := func(cell scopedCell) bool {
		return !cell.multi && (strings.Contains(cell.name, "rare") || strings.Contains(cell.name, "empty"))
	}
	stillRed := []scopedCell{}
	for _, cell := range scopedWorkboundCells() {
		query, args := shippedScopedQuery(cell)
		custom := explainScopedWork(t, ctx, db, query, args, false)
		if !workViolated(custom, cell.limit, cell.offset) {
			continue
		}
		if !probeBAuthorized(cell) {
			t.Logf("probeB: cell=%q is still red but outside probe (B) scope (rare/empty single-scope only); residual for a later rung", cell.name)
			continue
		}
		stillRed = append(stillRed, cell)
	}
	if len(stillRed) == 0 {
		t.Log("probeB: no still-red rare/empty single-scope cells; investigation not needed")
		return
	}
	for _, cell := range stillRed {
		query, args := shippedScopedQuery(cell)
		custom := explainScopedWork(t, ctx, db, query, args, false)
		generic := explainScopedWork(t, ctx, db, query, args, true)
		customAnd := planHasNode(custom, "BitmapAnd", "")
		genericAnd := planHasNode(generic, "BitmapAnd", "")
		t.Logf("probeB cell=%q custom shapes=%s | generic shapes=%s | BitmapAnd custom=%v generic=%v",
			cell.name, strings.Join(custom.shapes, " "), strings.Join(generic.shapes, " "), customAnd, genericAnd)
		if genericAnd && !customAnd {
			t.Logf("probeB cell=%q: STEER CANDIDATE (generic finds a BitmapAnd custom misses; steering REDIRECTED, no change here)", cell.name)
		} else {
			t.Logf("probeB cell=%q: nothing to steer toward (generic holds no BitmapAnd custom misses)", cell.name)
		}
		assertProbeBReadOnly(t, ctx, db)
	}
}

// assertProbeBReadOnly guards the investigation's read-only contract: probe
// (B) runs EXPLAIN only and must leave the corpus row count untouched.
func assertProbeBReadOnly(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_files`).Scan(&count); err != nil {
		t.Fatalf("probeB corpus count: %v", err)
	}
	const want = scopedBigRows + scopedSmallRepoCount*scopedSmallRows
	if count != want {
		t.Fatalf("probeB corpus count = %d, want %d (fixture mutated)", count, want)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersResweepAuditRangeUnionEqualsSingleStatementLive
// is the #11131 T3 differential on live Postgres: the union of one sweep's
// range candidates equals the shipped single-statement audit's candidate set
// (selectWinnersAuditCandidatesSQL) at several budgets, with no identifier in
// two ranges and every live finding fact recomputed exactly once. The fixture
// seeds stale, missing and orphan stored winners (orphans before the first
// identifier, between packages, between repositories, on the oversize
// identifier and after the last), an oversize identifier, operator
// overrides, and the exclusions (superseded generation, derived-image scope,
// tombstones, retracted classifications). Both arms are the shipped
// statements: nothing is hand-copied.
func TestSupplyChainImpactWinnersResweepAuditRangeUnionEqualsSingleStatementLive(t *testing.T) {
	h := newKeyedHarness(t, "#11131 audit range differential")
	liveFacts := auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	auditRangePerturb(t, h)

	var collationOrder bool
	if err := h.db.QueryRowContext(h.ctx, `SELECT '@tootallnate/once' > 'pkg:npm/p99'`).Scan(&collationOrder); err != nil {
		t.Fatalf("collation probe: %v", err)
	}
	// True under a libc or ICU collation that ignores '@' (byte order says
	// false): the fixture's punctuation packages then sort away from their
	// byte position, which only SQL-side bounds handle.
	t.Logf("database collation sorts '@tootallnate/once' after 'pkg:npm/p99': %v", collationOrder)
	single := auditSingleStatementCandidates(t, h.ctx, h.db)
	if len(single) < 20 {
		t.Fatalf("single-statement candidates = %d, want a perturbed fixture with at least 20", len(single))
	}
	for _, orphan := range []string{"a-orphan-first", "pkg:npm/p02x", "repo-1x", "zzz-orphan-last", "@tootallnate/orphan"} {
		found := false
		for k := range single {
			if strings.Contains(k, orphan) {
				found = true
			}
		}
		if !found {
			t.Fatalf("single-statement candidates miss the %s orphan: the fixture lost a shape", orphan)
		}
	}
	for _, budget := range []int{1, 7, 50, 200, 1000, 100000} {
		got := auditSweepAll(t, h.ctx, h.db, budget, postgres.WinnersAuditFirstRangeSQLForTest, postgres.WinnersAuditNextRangeSQLForTest)
		if missing := auditSetDiff(single, got.candidates); len(missing) > 0 {
			t.Errorf("budget %d: %d candidates missing from the union of %d ranges: %v", budget, len(missing), got.ranges, missing)
		}
		for k, n := range got.candidates {
			if !single[k] {
				t.Errorf("budget %d: extra candidate %q", budget, k)
			}
			if n > 1 {
				t.Errorf("budget %d: candidate %q appeared in %d ranges", budget, k, n)
			}
		}
		if got.facts != int64(liveFacts) {
			t.Errorf("budget %d: ranges recomputed %d facts, want each of the %d live facts once", budget, got.facts, liveFacts)
		}
		if budget <= 50 && got.oversize == 0 {
			t.Errorf("budget %d: no range ran the %d-fact identifier alone", budget, auditRangeOversizeFacts)
		}
		if budget == 100000 && got.ranges != 1 {
			t.Errorf("budget %d: %d ranges, want the whole corpus in one", budget, got.ranges)
		}
		t.Logf("budget %d: %d ranges, %d oversize, %d candidates (single %d), %d facts", budget, got.ranges, got.oversize, len(got.candidates), len(single), got.facts)
	}
}

// TestSupplyChainImpactWinnersResweepAuditRangeOpenEndedLastRangeLive is the
// seeded RED of the differential: the shipped statements with the stored
// side of the last range closed (derived by one textual mutation of the
// shipped pair) miss the orphan stored after every fact. The shipped pair
// finds it.
func TestSupplyChainImpactWinnersResweepAuditRangeOpenEndedLastRangeLive(t *testing.T) {
	h := newKeyedHarness(t, "#11131 audit range open-ended last range")
	auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	auditRangePerturb(t, h)
	single := auditSingleStatementCandidates(t, h.ctx, h.db)

	const openEnd = "((SELECT is_last FROM range_bounds) OR ROW(w."
	first, next := postgres.WinnersAuditFirstRangeSQLForTest, postgres.WinnersAuditNextRangeSQLForTest
	if !strings.Contains(first, openEnd) || !strings.Contains(next, openEnd) {
		t.Fatalf("shipped range statements lost the stored-side open end %q", openEnd)
	}
	closedFirst := strings.Replace(first, openEnd, "(FALSE OR ROW(w.", 1)
	closedNext := strings.Replace(next, openEnd, "(FALSE OR ROW(w.", 1)

	for _, budget := range []int{50, 1000} {
		red := auditSweepAll(t, h.ctx, h.db, budget, closedFirst, closedNext)
		missing := auditSetDiff(single, red.candidates)
		lastMissed := false
		for _, k := range missing {
			if strings.HasPrefix(k, "zzz-orphan-last") {
				lastMissed = true
			}
		}
		if !lastMissed {
			t.Errorf("budget %d: the closed last range still found the orphan after every fact (missing %v): the RED does not bite", budget, missing)
		}
		green := auditSweepAll(t, h.ctx, h.db, budget, first, next)
		if m := auditSetDiff(single, green.candidates); len(m) > 0 {
			t.Errorf("budget %d: the shipped pair missed %v", budget, m)
		}
	}
}

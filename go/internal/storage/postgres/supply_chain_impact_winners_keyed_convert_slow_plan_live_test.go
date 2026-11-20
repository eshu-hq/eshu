// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Failing-first plan proof for the fixed-cost convert lane (eshu-hq/eshu
// #10839): at the diagnosed slow-window geometry (69 scopes x 600 fan-out
// expansion, chunk disjoint from the expansion) the convert arm must not take
// the per-expansion-row chunk re-probe shape (41,400 outer loops firing one
// chunk probe per same-scope applied row, 24.8M index searches, 50.2M local
// buffer hits). The RED vehicle is the query shape the shim measured as the
// contract: the convert plan always evaluates the once-per-tick
// MATERIALIZED chunk-applied set, which the pre-fix tree has no reference to.
// The loop/buffer bounds trip on a slow alignment and hold on every
// alignment; the applied-delete assertions EXPLAIN-clear the sibling
// chunk-match site (bounded by construction: its EXISTS outer is the consumed
// scope rows, never the 41.4k-row expansion).
func TestSupplyChainImpactWinnersResweepKeyedConvertSlowWindowPlanLive(t *testing.T) {
	h := newKeyedHarness(t, "#10839 convert slow-window plan proof")
	const scopes = 69
	// Pin one scope row per seeded generation directly into the log first:
	// the tick admits oldest-first, so without pinned low-seq rows the
	// collapsed fact backlog would fill the admitted window and starve the
	// scope expansion the convert assertions plan on (same pinning as the
	// scope-expansion proof). The trigger-staged duplicates collapse onto
	// these keys.
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id)
SELECT 'scope', 'activation', 'vuln-intel://10839-conv/s' || g, 'vuln-intel://10839-conv/s' || g || '/g2' FROM generate_series(0,68) g`)
	for s := range scopes {
		scope := fmt.Sprintf("vuln-intel://10839-conv/s%d", s)
		h.seedScope(scope, scope+"/g1")
		h.addGeneration(scope, scope+"/g2", "pending")
		h.write(scope, scope+"/g2", chunkedFindings(fmt.Sprintf("conv%d", s), 60, 10, 0)...)
		h.activate(scope, scope+"/g1", scope+"/g2")
	}
	// Decoy facts older than every scope row: the 500-identifier chunk is the
	// oldest-first prefix, so it holds only decoys and stays disjoint from
	// the 41,400-row expansion (the diagnosed 0-row slow window). Key-only
	// rows shape the convert/delete plans faithfully, like the existing
	// scope-expansion proof's over-cap arm.
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, captured_at)
SELECT 'fact', 'scanner', 'pkg:npm/10839-decoy/' || g, 'repo-10839-decoy', 'digest-10839-decoy-' || g, 'c0-10839-decoy', 'CVE-2027-' || g, now() - interval '1 year'
FROM generate_series(1,500) g`)
	h.exec(`ANALYZE fact_records`)
	h.exec(`ANALYZE supply_chain_impact_winners_dirty_log`)

	// All 69 converting generations are pending (log pins plus trigger
	// captures); background harness pairs may add more, so the count is
	// scoped to this proof's scopes.
	if got := h.count(`SELECT count(DISTINCT (scope_id, generation_id)) FROM (
SELECT scope_id, generation_id FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope'
UNION ALL
SELECT scope_id, generation_id FROM supply_chain_impact_winners_dirty_staging WHERE kind = 'scope') AS p
WHERE scope_id LIKE 'vuln-intel://10839-conv/%' AND generation_id LIKE '%/g2'`); got != scopes {
		t.Fatalf("pending converting scope pairs = %d, want %d", got, scopes)
	}
	if got := h.count(`SELECT count(*) FROM fact_records
WHERE scope_id LIKE 'vuln-intel://10839-conv/%' AND fact_kind = 'reducer_supply_chain_impact_finding' AND is_tombstone = FALSE`); got != scopes*600 {
		t.Fatalf("seeded expansion rows = %d, want %d", got, scopes*600)
	}

	convertPlan, deletePlan, err := postgres.ExplainWinnersKeyedConvertSlowWindowForTest(h.ctx, h.db, 500)
	if err != nil {
		t.Fatalf("explain slow window: %v", err)
	}
	t.Logf("slow-window convert plan:\n%s", convertPlan)
	t.Logf("slow-window applied-delete plan:\n%s", deletePlan)

	// The once-per-tick set is the measured contract: every convert plan
	// evaluates it, the pre-fix tree never mentions it.
	if !strings.Contains(convertPlan, "chunk_applied") {
		t.Error("convert plan never evaluates the once-per-tick chunk-applied set: the correlated per-row chunk re-probe is back")
	}
	// Geometry proof: the planned expansion is the full 69x600 slow window,
	// so the bounds below measure the diagnosed shape, not an empty plan.
	if !strings.Contains(convertPlan, "rows=41400") {
		t.Error("convert plan never scans the 41,400-row slow-window expansion: the seeded alignment did not reach the plan")
	}
	// Per-expansion-row tripwire: the slow shape nests 41,400 outer loops.
	if strings.Contains(convertPlan, "loops=41400") {
		t.Error("convert plan carries 41,400 outer loops: the per-expansion-row chunk re-probe is back")
	}
	if max := slowPlanLocalBufferMax(t, convertPlan); max >= 5_000_000 {
		t.Errorf("convert plan max local buffers = %d, want < 5,000,000 (slow shape burns 50,231,760)", max)
	}
	for _, searches := range slowPlanChunkIndexSearches(t, convertPlan) {
		if searches >= 1_000_000 {
			t.Errorf("convert plan chunk index searches = %d, want < 1,000,000 (slow shape fires 24,840,000)", searches)
		}
	}
	// Sibling-site clearing: the applied delete probes the chunk per
	// consumed scope row (69 loops here), never per expansion row.
	if strings.Contains(deletePlan, "loops=41400") {
		t.Error("applied-delete plan carries 41,400 outer loops: the sibling chunk-match site flipped")
	}
	if max := slowPlanLocalBufferMax(t, deletePlan); max >= 5_000_000 {
		t.Errorf("applied-delete plan max local buffers = %d, want < 5,000,000", max)
	}
	for _, searches := range slowPlanChunkIndexSearches(t, deletePlan) {
		if searches >= 1_000_000 {
			t.Errorf("applied-delete plan chunk index searches = %d, want < 1,000,000", searches)
		}
	}
}

var slowPlanBufferRe = regexp.MustCompile(`local hit=(\d+)`)

func slowPlanLocalBufferMax(t *testing.T, plan string) int64 {
	t.Helper()
	var max int64
	for _, m := range slowPlanBufferRe.FindAllStringSubmatch(plan, -1) {
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatalf("parse buffer count %q: %v", m[1], err)
		}
		if n > max {
			max = n
		}
	}
	return max
}

var slowPlanSearchRe = regexp.MustCompile(`Index Searches: (\d+)`)

func slowPlanChunkIndexSearches(t *testing.T, plan string) []int64 {
	t.Helper()
	var out []int64
	for _, line := range strings.Split(plan, "\n") {
		if !strings.Contains(line, "winners_keyed_chunk_idents_all_idx") {
			continue
		}
		m := slowPlanSearchRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatalf("parse index searches %q: %v", m[1], err)
		}
		out = append(out, n)
	}
	return out
}

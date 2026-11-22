// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// winnersAuditRangeScaleEnv selects the #11131 range-cost measurement: unset
// skips it (the differential and the built-path proofs cover correctness);
// a positive integer is the fixture's identifier count (30000 seeds about
// 300,000 finding facts with about 2 KB of payload padding each).
const winnersAuditRangeScaleEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_RANGE_SCALE"

// TestSupplyChainImpactWinnersResweepAuditRangeCostAtScaleLive measures the
// shipped range statement at the default budget on a scaled fixture: the
// plan must read the keyed winners index in order under a ROW index
// condition with a Limit (no Seq Scan on fact_records) and must not run the
// oversize branch; it reports the per-range time, the sum over a sweep, the
// single-statement audit time on the same data, the NULL probe plan and
// time, and the per-range writer verify time.
func TestSupplyChainImpactWinnersResweepAuditRangeCostAtScaleLive(t *testing.T) {
	raw := strings.TrimSpace(winnersScaleKnob(winnersAuditRangeScaleEnv))
	if raw == "" {
		t.Skip("set " + winnersAuditRangeScaleEnv + " to the identifier count to measure the range cost")
	}
	identifiers, err := strconv.Atoi(raw)
	if err != nil || identifiers < 100 {
		t.Fatalf("%s=%q: want an identifier count of at least 100", winnersAuditRangeScaleEnv, raw)
	}
	h := newKeyedHarness(t, "#11131 audit range cost at scale")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	t.Cleanup(cancel)
	h.ctx = ctx
	seedStart := time.Now()
	live := auditRangeSeed(t, h, identifiers)
	h.exec(`UPDATE fact_records SET payload = payload || jsonb_build_object('pad',
  (SELECT string_agg(md5(fact_id || i::text), '') FROM generate_series(1, 64) AS i))
WHERE fact_kind = 'reducer_supply_chain_impact_finding'`)
	h.exec(`VACUUM ANALYZE fact_records`)
	auditRangePerturb(t, h)
	t.Logf("seeded %d live facts over %d identifiers in %s", live, identifiers, time.Since(seedStart).Round(time.Second))

	plan := h.explain("EXPLAIN (ANALYZE, BUFFERS) "+postgres.WinnersAuditFirstRangeSQLForTest, postgres.DefaultWinnersAuditRangeBudget)
	if !strings.Contains(plan, "fact_records_supply_chain_impact_winners_keyed_idx") {
		t.Errorf("range plan does not read the keyed winners index:\n%s", plan)
	}
	for _, line := range strings.Split(plan, "\n") {
		if strings.Contains(line, "Seq Scan on fact_records") {
			t.Errorf("range plan scans fact_records sequentially: %s", strings.TrimSpace(line))
		}
	}
	t.Logf("first range plan:\n%s", plan)

	reader := postgres.SQLDB{DB: h.db}
	var lo *postgres.WinnersAuditRangeIdent
	var ranges int
	var total, worst time.Duration
	var facts int64
	for {
		start := time.Now()
		rng, err := postgres.ReadWinnersAuditRange(h.ctx, reader, lo, postgres.DefaultWinnersAuditRangeBudget)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("range %d: %v", ranges, err)
		}
		ranges++
		total += elapsed
		worst = max(worst, elapsed)
		facts += rng.Facts
		if rng.Last {
			break
		}
		lo = rng.Hi
	}
	start := time.Now()
	single := len(auditSingleStatementCandidates(t, h.ctx, h.db))
	singleTime := time.Since(start)
	t.Logf("sweep at budget %d: %d ranges, %d facts, sum %.3fs, worst range %.3fs, %.3f ms/fact; single statement %.3fs (%d candidates), ranged/single %.2f",
		postgres.DefaultWinnersAuditRangeBudget, ranges, facts, total.Seconds(), worst.Seconds(),
		1000*total.Seconds()/float64(max(facts, 1)), singleTime.Seconds(), single, total.Seconds()/singleTime.Seconds())

	start = time.Now()
	keys, err := postgres.ProbeWinnersAuditNullKeys(h.ctx, reader)
	if err != nil || keys.Any() {
		t.Fatalf("NULL probe = %+v, %v", keys, err)
	}
	t.Logf("NULL probe %.3fs; plan:\n%s", time.Since(start).Seconds(), h.explain("EXPLAIN (ANALYZE, BUFFERS) "+postgres.WinnersAuditNullProbeSQLForTest))

	a, _ := newLiveSweepAuditor(h, postgres.DefaultWinnersAuditRangeBudget)
	start = time.Now()
	passes := sweepUntil(t, h.ctx, a, 10000)
	t.Logf("built-path sweep: %d passes in %.3fs (%.3fs per range, read plus verify)",
		len(passes), time.Since(start).Seconds(), time.Since(start).Seconds()/float64(len(passes)))
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestClaimBatchFencePrecomputeSharedKeyStorm is the C-3 storm row for
// #10630 step 2: 8 concurrent ClaimBatch claimers draining a backlog where
// every conflict key holds 3 rows must never hold two live leases on one
// key and must never deliver one work item twice. The fence rewrite moves
// the live-lease check into a once-per-claim set, so a rewrite that broke
// the representative fence or the locked-CTE EvalPlanQual recheck would
// surface here as a duplicate lease or id. Only the oldest row per key is
// ever claimable, so the storm must deliver exactly one (the oldest) row
// per key, each exactly once.
func TestClaimBatchFencePrecomputeSharedKeyStorm(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the fence-precompute storm proof")
	}

	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedGateScope(t, ctx, db, "scope-fence-storm", "gen-fair", now)

	const keys = 32
	const perKey = 3
	for k := 0; k < keys; k++ {
		for r := 0; r < perKey; r++ {
			insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
				workItemID: fmt.Sprintf("fstorm-%02d-%d", k, r), scopeID: "scope-fence-storm", generationID: "gen-fair",
				domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("fstorm-key-%02d", k),
				updatedAt: now.Add(time.Duration(k*perKey+r) * time.Second),
			})
		}
	}

	const claimers = 8
	claimAt := now.Add(2 * time.Hour)
	var (
		mu      sync.Mutex
		claimed = map[string]int{}
		start   = make(chan struct{})
		wg      sync.WaitGroup
	)
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimerDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
			q := ReducerQueue{
				database:      SQLDB{DB: claimerDB},
				LeaseOwner:    fmt.Sprintf("farming-claimer-%d", i),
				LeaseDuration: time.Hour,
				Now:           func() time.Time { return claimAt },
			}
			<-start
			// More rounds than the backlog needs: late rounds race on an
			// almost-drained queue, which is exactly the interleaving the
			// fence must survive.
			for r := 0; r < 20; r++ {
				intents, err := q.ClaimBatch(ctx, 8)
				if err != nil {
					t.Errorf("ClaimBatch() error = %v", err)
					return
				}
				mu.Lock()
				for _, in := range intents {
					claimed[in.IntentID]++
				}
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(claimed) != keys {
		t.Fatalf("storm claimed %d distinct items, want one representative per key = %d: %v",
			len(claimed), keys, claimed)
	}
	for k := 0; k < keys; k++ {
		want := fmt.Sprintf("fstorm-%02d-0", k)
		if n := claimed[want]; n != 1 {
			t.Fatalf("oldest row %q delivered %d times, want exactly once (representative fence broke)", want, n)
		}
	}
	var dupKeys int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM (
    SELECT conflict_domain, COALESCE(conflict_key, scope_id)
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND status IN ('claimed', 'running')
      AND claim_until > $1
    GROUP BY conflict_domain, COALESCE(conflict_key, scope_id)
    HAVING count(*) > 1
) dup`, claimAt).Scan(&dupKeys); err != nil {
		t.Fatalf("count duplicate live leases: %v", err)
	}
	if dupKeys != 0 {
		t.Fatalf("%d conflict keys hold more than one live lease after the storm", dupKeys)
	}
}

// TestClaimBatchFencePrecomputePlanAndReplay is the C-2/shape + bounded
// replay row for #10630 step 2 on a >=10k backlog:
//
//   - OLD (frozen pre-change candidate) vs NEW (derived from the shipped
//     query) EXPLAIN ANALYZE x3 in rollback on the identical fixture: the
//     OLD candidate section must carry the per-row fence SubPlans (the
//     removed shape, non-vacuous), the NEW candidate section must carry
//     zero SubPlans while executing the precomputed live_fence_keys set.
//   - 20 timed ClaimBatch rounds on two identically seeded databases (one
//     per side is unnecessary: the shipped query is one side and the frozen
//     OLD full claim cannot run without the OLD lock tail, so the replay
//     times the shipped claim and the EXPLAIN pair carries the
//     before/after; the paired claim p50/p95 + items/s come from the timed
//     rounds).
//
// Timings are logged as evidence, not asserted as gates (single-sample wall
// time on a fixture is not a proof, and the fixture is not production
// scale). The asserted gates are the plan shapes.
func TestClaimBatchFencePrecomputePlanAndReplay(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the fence-precompute replay proof")
	}

	oldQuery := frozenPreFenceCandidate(t)
	newQuery, ok := deriveReadOnlyCandidateSelect(claimReducerWorkBatchQuery)
	if !ok {
		t.Fatal("could not derive read-only candidate SELECT from the shipped batch claim query")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedFenceReplayFixture(t, ctx, db, now)

	clock := now.Add(2 * time.Hour)
	candArgs := []any{clock, nil, "n/a", clock.Add(time.Hour), false, 0, 0, 8}

	var oldPlans, newPlans [][]string
	for i := 0; i < 3; i++ {
		oldPlans = append(oldPlans, explainAnalyzeRollback(t, ctx, db, oldQuery, candArgs))
		newPlans = append(newPlans, explainAnalyzeRollback(t, ctx, db, newQuery, candArgs))
	}
	oldPlan, newPlan := oldPlans[2], newPlans[2]

	// The fence is a correlated NOT EXISTS, which the planner executes as
	// a per-representative inner probe (loops=N on the inflight scan),
	// not as a "SubPlan" node — so the shape signal is the probe loop
	// count on the fence's own table alias, unique as
	// "fact_work_items inflight" across the whole plan. The rewrite must
	// replace those N probes with one execution of the precomputed
	// live_fence_keys set and zero inflight probes.
	oldProbeLoops := maxPlanLoopsOnTable(t, oldPlan, "fact_work_items inflight")
	newProbeLoops := maxPlanLoopsOnTable(t, newPlan, "fact_work_items inflight")
	t.Logf("fence probe loops: old=%d new=%d", oldProbeLoops, newProbeLoops)
	t.Logf("execution time x3 old-candidate=[%s %s %s] new-candidate=[%s %s %s]",
		planExecutionTime(oldPlans[0]), planExecutionTime(oldPlans[1]), planExecutionTime(oldPlans[2]),
		planExecutionTime(newPlans[0]), planExecutionTime(newPlans[1]), planExecutionTime(newPlans[2]))
	t.Logf("buffers old-candidate=[%s] new-candidate=[%s]",
		strings.Join(planBufferLines(oldPlan), " / "),
		strings.Join(planBufferLines(newPlan), " / "))
	if oldProbeLoops < 1000 {
		t.Fatalf("old plan executes the fence %d times on the replay fixture, want >= 1000 probes; "+
			"the fixture does not exercise the removed per-row shape:\n%s", oldProbeLoops, strings.Join(oldPlan, "\n"))
	}
	if newProbeLoops != 0 {
		t.Errorf("new plan still probes fact_work_items inflight (loops=%d); "+
			"the correlated fence is not removed:\n%s", newProbeLoops, strings.Join(newPlan, "\n"))
	}
	if joined := strings.Join(newPlan, "\n"); !strings.Contains(joined, "live_fence_keys") {
		t.Errorf("new candidate plan does not execute the precomputed live_fence_keys set:\n%s", joined)
	}

	// Bounded timed replay: 20 ClaimBatch rounds on a second identically
	// seeded database, so claim side effects never perturb the EXPLAIN
	// fixture above. Reports per-claim p50/p95 and derived items/s at the
	// shipped batch limit.
	replayDB := openReducerFairnessDB(t, ctx, dsn)
	seedFenceReplayFixture(t, ctx, replayDB, now)
	q := ReducerQueue{
		database:      SQLDB{DB: replayDB},
		LeaseOwner:    "fence-replay",
		LeaseDuration: time.Hour,
		Now:           func() time.Time { return clock },
	}
	var latencies []time.Duration
	admitted := 0
	for i := 0; i < 20; i++ {
		start := time.Now()
		intents, err := q.ClaimBatch(ctx, 8)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("replay ClaimBatch round %d error = %v", i, err)
		}
		latencies = append(latencies, elapsed)
		admitted += len(intents)
	}
	t.Logf("replay claims=%d admitted=%d latencies=%v", len(latencies), admitted, latencies)
	p50, p95 := latencyQuantiles(latencies)
	var itemsPerSec float64
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	if total > 0 {
		itemsPerSec = float64(admitted) / total.Seconds()
	}
	t.Logf("paired claim p50=%s p95=%s admitted=%d items/s=%.2f (12k+ backlog, batch limit 8)",
		p50, p95, admitted, itemsPerSec)
	if admitted == 0 {
		t.Fatal("replay admitted zero rows; the fixture or claim is broken")
	}
}

// seedFenceReplayFixture seeds a >=10k-row backlog: 6000 conflict keys with
// an older/newer pending pair each (reps windows exercised at scale), 200
// live-held keys (holder + pending sibling: the fence exercised at scale),
// and the plain rows needed so every claim fills its batch. supply_chain_
// impact carries no readiness requirement, so no gate holds rows back.
func seedFenceReplayFixture(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
	t.Helper()
	seedGateScope(t, ctx, db, "scope-fence-replay", "gen-fair", now)
	const keys = 6000
	insertChunk := func(start, count int) {
		t.Helper()
		var sb strings.Builder
		sb.WriteString(`INSERT INTO fact_work_items (
		    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		    conflict_key, status, attempt_count, payload, created_at, updated_at
		) VALUES `)
		args := make([]any, 0, count*4)
		for i := 0; i < count; i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			base := len(args) + 1
			fmt.Fprintf(&sb, "($%d, 'scope-fence-replay', 'gen-fair', 'reducer', 'supply_chain_impact', 'scope', $%d, 'pending', 0, jsonb_build_object('entity_key', $%d::text), $%d, $%d)",
				base, base+1, base, base+2, base+3)
			n := start + i
			key := fmt.Sprintf("freplay-key-%05d", n/2)
			ts := now.Add(time.Duration(n) * time.Second)
			args = append(args, fmt.Sprintf("freplay-%05d", n), key, ts, ts)
		}
		mustExecGateArgs(t, ctx, db, sb.String(), args...)
	}
	const total = keys * 2
	const chunk = 1000
	for start := 0; start < total; start += chunk {
		n := chunk
		if start+n > total {
			n = total - start
		}
		insertChunk(start, n)
	}
	// 200 live-held keys: holder lease live at the replay clock, pending
	// sibling fenced behind it. Lease expiries stagger across the two
	// hours after the replay clock the way worker leases spread in
	// production; a single shared expiry would collapse claim_until to
	// one most-common value and mislead the planner exactly where the
	// fence estimate matters.
	clock := now.Add(2 * time.Hour)
	for i := 0; i < 200; i++ {
		holder := fmt.Sprintf("freplay-holder-%03d", i)
		sibling := fmt.Sprintf("freplay-sibling-%03d", i)
		key := fmt.Sprintf("freplay-held-key-%03d", i)
		mustExecGateArgs(t, ctx, db, `INSERT INTO fact_work_items (
		    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		    conflict_key, status, attempt_count, payload, created_at, updated_at
		) VALUES ($1, 'scope-fence-replay', 'gen-fair', 'reducer', 'supply_chain_impact', 'scope',
		    $2, 'pending', 0, jsonb_build_object('entity_key', $1::text), $3, $3),
		    ($4, 'scope-fence-replay', 'gen-fair', 'reducer', 'supply_chain_impact', 'scope',
		    $2, 'pending', 0, jsonb_build_object('entity_key', $4::text), $3, $3)`,
			holder, key, now, sibling)
		mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET status='claimed', lease_owner='freplay', claim_until=$1 WHERE work_item_id=$2`,
			clock.Add(time.Duration(i+1)*36*time.Second), holder)
	}
	// Production autovacuum keeps planner stats on this hot table; the
	// proof fixture ANALYZEs explicitly so the plan comparison measures
	// the rewrite, not missing statistics.
	if _, err := db.ExecContext(ctx, `ANALYZE fact_work_items`); err != nil {
		t.Fatalf("analyze replay fixture: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items`).Scan(&n); err != nil {
		t.Fatalf("count replay fixture: %v", err)
	}
	t.Logf("replay fixture rows=%d (want >= 10000)", n)
	if n < 10000 {
		t.Fatalf("replay fixture has %d rows, want >= 10000", n)
	}
}

var planLoopsRe = regexp.MustCompile(`loops=(\d+)`)

// maxPlanLoopsOnTable returns the largest loops= count on any EXPLAIN
// line naming the given table reference (e.g. the fence's
// "fact_work_items inflight" inner probe). 0 when no line names it,
// which is the expected NEW shape after the precompute removes the
// correlated probe.
func maxPlanLoopsOnTable(t *testing.T, plan []string, table string) int {
	t.Helper()
	max := 0
	for _, line := range plan {
		if !strings.Contains(line, table) {
			continue
		}
		for _, m := range planLoopsRe.FindAllStringSubmatch(line, -1) {
			var n int
			if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
				t.Fatalf("parse loops count %q: %v", m[1], err)
			}
			if n > max {
				max = n
			}
		}
	}
	return max
}

func latencyQuantiles(ls []time.Duration) (p50, p95 time.Duration) {
	if len(ls) == 0 {
		return 0, 0
	}
	cp := append([]time.Duration(nil), ls...)
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j] < cp[j-1]; j-- {
			cp[j], cp[j-1] = cp[j-1], cp[j]
		}
	}
	return cp[len(cp)/2], cp[(len(cp)*95)/100]
}

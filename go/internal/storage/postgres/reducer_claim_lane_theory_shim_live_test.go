// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason conflict-key lane theory shim (#11430 proof item 1): the derived laned candidate SELECT (lane predicate injected into the shipped base CTE) must show per-lane scan shrink and aggregate L-lane claim CPU within 10% of the unlaned candidate SELECT at QA-scale backlog, else STOP with no implementation, gated by the ESHU_REDUCER_FAIRNESS_PROOF_DSN env Skip; Postgres only; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DISPOSABLE ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// laneTheoryLanes is the L=4 lane tiling under test: four disjoint hex-char
// sets covering all 16 md5 hex digits, matching the design doc's lane-char
// scheme (one hex char = 16 lanes max; a lane owns a set of chars).
var laneTheoryLanes = [][4]string{
	{"0", "1", "2", "3"},
	{"4", "5", "6", "7"},
	{"8", "9", "a", "b"},
	{"c", "d", "e", "f"},
}

// laneTheoryBaseAnchor is the shipped base-CTE domain filter fragment the
// lane predicate appends to. It is unique to base (the stale-gate twins
// qualify domain as work./stale.), so a single Replace targets exactly the
// snapshot level the design names and fails loudly on gate-text drift.
const laneTheoryBaseAnchor = "AND ($2::text[] IS NULL OR domain = ANY($2::text[]))"

// laneTheoryPredicate renders the snapshot-level lane filter for one lane: a
// pure function of (conflict_domain, ckey) with the same COALESCE fallback
// base.ckey uses, so NULL conflict_key rows still land in exactly one lane
// (conflict_domain is NOT NULL per migration 005).
func laneTheoryPredicate(chars [4]string) string {
	return "\n      AND SUBSTRING(md5(fact_work_items.conflict_domain || '|' || COALESCE(fact_work_items.conflict_key, fact_work_items.scope_id)) FROM 1 FOR 1) IN ('" +
		strings.Join(chars[:], "','") + "')"
}

// laneTheoryChain renders the shipped candidate chain (WITH-body) with the
// lane predicate injected into base, derived from the shipped Go sources —
// never hand-copied. ok is false when the anchor no longer appears exactly
// once (gate-text drift must fail loudly, not silently unlane the shim).
func laneTheoryChain(lane *[4]string) (string, bool) {
	chain := reducerClaimBatchCandidateChainCTEs()
	if lane == nil {
		return chain, true
	}
	if strings.Count(chain, laneTheoryBaseAnchor) != 1 {
		return "", false
	}
	return strings.Replace(chain, laneTheoryBaseAnchor, laneTheoryBaseAnchor+laneTheoryPredicate(*lane), 1), true
}

// laneTheorySelect renders the full candidate SELECT the lock loop consumes:
// every rest-arm ORDER BY key is a candidate output column, and LIMIT 32
// mirrors the vuln-lane batchClaimSize (Workers*4 at 8 workers). $3/$4 ride
// the funnel's tautology so placeholder numbering stays identical to the
// shipped chain; the LIMIT is a literal so no $8 is needed.
func laneTheorySelect(chain string) string {
	return `WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + reducerClaimStaleGateCTEs() + `,
` + chain + `
SELECT work_item_id
FROM candidate
WHERE ($3::text IS NULL OR $3::text IS NOT NULL)
  AND ($4::timestamptz IS NULL OR $4::timestamptz IS NOT NULL)
ORDER BY reclaim_rank ASC, reducer_domain_priority ASC, reducer_source_inflight_count ASC, reducer_source_fair_rank ASC, reducer_domain_fair_rank ASC, reducer_domain_inflight_count ASC, updated_at ASC, work_item_id ASC
LIMIT 32`
}

// laneTheoryIDSetSelect renders the unlimited candidate id set for
// partition sanity (union of lanes must equal the unlaned set exactly).
// The timed claim-shape query below keeps its LIMIT; this one must not.
func laneTheoryIDSetSelect(chain string) string {
	return `WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + reducerClaimStaleGateCTEs() + `,
` + chain + `
SELECT work_item_id
FROM candidate
WHERE ($3::text IS NULL OR $3::text IS NOT NULL)
  AND ($4::timestamptz IS NULL OR $4::timestamptz IS NOT NULL)`
}

type laneTheoryPlan struct {
	Plan struct {
		SharedHitBlocks   int64 `json:"Shared Hit Blocks"`
		SharedReadBlocks  int64 `json:"Shared Read Blocks"`
		TempReadBlocks    int64 `json:"Temp Read Blocks"`
		TempWrittenBlocks int64 `json:"Temp Written Blocks"`
	} `json:"Plan"`
	ExecutionTime float64 `json:"Execution Time"`
}

// laneTheoryExplain runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on a
// read-only candidate SELECT (no locks taken) and returns execution
// milliseconds, shared buffers touched (hit + read), and temp buffers
// touched (read + written, from sort/window spills).
func laneTheoryExplain(ctx context.Context, db *sql.DB, query string, args ...any) (float64, int64, int64, error) {
	var raw string
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return 0, 0, 0, err
	}
	var plans []laneTheoryPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		return 0, 0, 0, err
	}
	if len(plans) != 1 {
		return 0, 0, 0, fmt.Errorf("explain returned %d plans, want 1", len(plans))
	}
	shared := plans[0].Plan.SharedHitBlocks + plans[0].Plan.SharedReadBlocks
	temp := plans[0].Plan.TempReadBlocks + plans[0].Plan.TempWrittenBlocks
	return plans[0].ExecutionTime, shared, temp, nil
}

// TestLaneTheoryDerivedPrefixMatchesProduction is the hermetic guard for the
// live shim below: the derived WITH-prefix (readiness + stale + candidate
// chain) must be a byte-prefix of the shipped batch-claim query, so the
// shim can never silently diverge from production text. No database needed.
func TestLaneTheoryDerivedPrefixMatchesProduction(t *testing.T) {
	chain, ok := laneTheoryChain(nil)
	if !ok {
		t.Fatal("unlaned chain derivation failed")
	}
	derived := "WITH " + reducerClaimReadinessRequirementsCTE() + ",\n" + reducerClaimStaleGateCTEs() + ",\n" + chain
	if !strings.HasPrefix(claimReducerWorkBatchQuery, "\n"+derived) {
		t.Fatal("derived candidate prefix is not a byte-prefix of the shipped batch-claim query: shim text diverged from production")
	}
	if got := strings.Count(chain, laneTheoryBaseAnchor); got != 1 {
		t.Fatalf("base anchor appears %d times in derived chain, want exactly 1", got)
	}
}

func laneTheoryMedian(xs []float64) float64 {
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

// seedLaneTheoryBacklog plants a QA-scale backlog on one scope+generation:
// 9000 supply_chain_impact rows plus 1000 advisory_group_projection rows,
// every key distinct (the shim-measured 1-row-per-key shape), updated_at
// spread over the past hour. One INSERT ... SELECT keeps the seed fast.
func seedLaneTheoryBacklog(t *testing.T, ctx context.Context, db *sql.DB, scopeID string, now time.Time) {
	t.Helper()
	seedReducerFairnessScope(t, ctx, db, scopeID, now)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, payload, created_at, updated_at
)
SELECT 'lane-sci-' || g::text, $1, 'gen-fair', 'reducer', 'supply_chain_impact',
    'scope', 'lane-key-' || g::text, 'pending', 0,
    jsonb_build_object('entity_key', 'lane-sci-' || g::text, 'reason', 'lane-theory', 'fact_id', 'lane-sci-' || g::text, 'source_system', 'nvd'),
    $2::timestamptz - (g * interval '1 second'), $2::timestamptz - (g * interval '1 second')
FROM generate_series(1, 9000) AS g
UNION ALL
SELECT 'lane-adv-' || g::text, $1, 'gen-fair', 'reducer', 'advisory_group_projection',
    'advisory_group', 'adv-key-' || g::text, 'pending', 0,
    jsonb_build_object('entity_key', 'lane-adv-' || g::text, 'reason', 'lane-theory', 'fact_id', 'lane-adv-' || g::text, 'source_system', 'osv'),
    $2::timestamptz - (g * interval '1 second'), $2::timestamptz - (g * interval '1 second')
FROM generate_series(1, 1000) AS g`,
		scopeID, now); err != nil {
		t.Fatalf("seed lane-theory backlog: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE fact_work_items`); err != nil {
		t.Fatalf("analyze lane-theory backlog: %v", err)
	}
}

// TestClaimLaneTheoryShim is #11430 proof item 1 (theory FIRST): at a
// QA-scale backlog the laned candidate SELECTs derived from the shipped
// chain must partition the unlaned set exactly (union equals global),
// shrink per-lane scans, and keep aggregate L-lane claim CPU within 10% of
// unlaned. A failure is the STOP signal: no implementation follows.
func TestClaimLaneTheoryShim(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the lane-theory shim")
	}
	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	seedLaneTheoryBacklog(t, ctx, db, "scope-lane-theory", now)

	unlanedChain, ok := laneTheoryChain(nil)
	if !ok {
		t.Fatal("unlaned chain derivation failed")
	}
	lanedChains := make([]string, 0, len(laneTheoryLanes))
	for i := range laneTheoryLanes {
		chain, ok := laneTheoryChain(&laneTheoryLanes[i])
		if !ok {
			t.Fatalf("lane %d chain derivation failed: base anchor drift", i)
		}
		lanedChains = append(lanedChains, chain)
	}

	args := []any{
		now,
		[]string{"supply_chain_impact", "advisory_group_projection"},
		"lane-theory-shim",
		now.Add(time.Hour),
		false,
		0,
		0,
	}
	unlanedQuery := laneTheorySelect(unlanedChain)
	lanedQueries := make([]string, 0, len(lanedChains))
	for _, chain := range lanedChains {
		lanedQueries = append(lanedQueries, laneTheorySelect(chain))
	}
	unlanedSetQuery := laneTheoryIDSetSelect(unlanedChain)
	lanedSetQueries := make([]string, 0, len(lanedChains))
	for _, chain := range lanedChains {
		lanedSetQueries = append(lanedSetQueries, laneTheoryIDSetSelect(chain))
	}

	// Partition sanity on the real SELECTs (not EXPLAIN): union of lanes
	// must equal the unlaned set exactly.
	fetchIDs := func(query string) map[string]bool {
		t.Helper()
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			t.Fatalf("fetch candidate ids: %v", err)
		}
		defer func() { _ = rows.Close() }()
		ids := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan candidate id: %v", err)
			}
			ids[id] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows candidate ids: %v", err)
		}
		return ids
	}
	unlanedIDs := fetchIDs(unlanedSetQuery)
	unionIDs := map[string]bool{}
	for i, query := range lanedSetQueries {
		for id := range fetchIDs(query) {
			if unionIDs[id] {
				t.Fatalf("lane %d double-admits candidate %q: lanes overlap", i, id)
			}
			unionIDs[id] = true
		}
	}
	if len(unionIDs) != len(unlanedIDs) {
		t.Fatalf("lane union covers %d candidates, unlaned covers %d: gap or overlap", len(unionIDs), len(unlanedIDs))
	}
	for id := range unionIDs {
		if !unlanedIDs[id] {
			t.Fatalf("lane union admits %q missing from unlaned set", id)
		}
	}
	t.Logf("partition sanity: %d unlaned candidates = union of %d lanes", len(unlanedIDs), len(laneTheoryLanes))

	// Timed comparison: 1 warmup (discarded), then 5 measured runs; median.
	const runs = 5
	measure := func(query string) (execMS, sharedBufs, tempBufs float64) {
		t.Helper()
		if _, _, _, err := laneTheoryExplain(ctx, db, query, args...); err != nil {
			t.Fatalf("warmup explain: %v", err)
		}
		execs := make([]float64, 0, runs)
		shared := make([]float64, 0, runs)
		temp := make([]float64, 0, runs)
		for range runs {
			ms, s, tmp, err := laneTheoryExplain(ctx, db, query, args...)
			if err != nil {
				t.Fatalf("explain: %v", err)
			}
			execs = append(execs, ms)
			shared = append(shared, float64(s))
			temp = append(temp, float64(tmp))
		}
		return laneTheoryMedian(execs), laneTheoryMedian(shared), laneTheoryMedian(temp)
	}
	unlanedMS, unlanedBuf, unlanedTemp := measure(unlanedQuery)
	t.Logf("unlaned: %.2f ms exec, %.0f shared buffers, %.0f temp buffers", unlanedMS, unlanedBuf, unlanedTemp)
	aggregateMS := 0.0
	for i, query := range lanedQueries {
		ms, buf, tmp := measure(query)
		aggregateMS += ms
		t.Logf("lane %d: %.2f ms exec, %.0f shared buffers, %.0f temp buffers", i, ms, buf, tmp)
		if buf >= unlanedBuf {
			t.Errorf("STOP: lane %d touches %.0f buffers, no scan shrink vs unlaned %.0f", i, buf, unlanedBuf)
		}
	}
	t.Logf("aggregate %d-lane exec %.2f ms vs unlaned %.2f ms (ratio %.3f)", len(lanedQueries), aggregateMS, unlanedMS, aggregateMS/unlanedMS)
	if aggregateMS > 1.10*unlanedMS {
		t.Errorf("STOP: aggregate %d-lane claim CPU %.2f ms regresses >10%% over unlaned %.2f ms", len(lanedQueries), aggregateMS, unlanedMS)
	}
}

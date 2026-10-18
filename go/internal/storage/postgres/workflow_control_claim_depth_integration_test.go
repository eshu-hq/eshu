// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
)

// TestWorkflowClaimDepthEightClaimersDrainExactlyOnce proves the #8468
// harness on an even backlog: eight claimers on eight distinct backends
// drain every seeded item exactly once, no work item or claim id is granted
// twice, and the database agrees (one completed claim row per item). This
// is the SKIP LOCKED / EvalPlanQual no-duplicate-grant proof the fairness
// and pacing leaves inherit.
func TestWorkflowClaimDepthEightClaimersDrainExactlyOnce(t *testing.T) {
	database, store, ctx, poolLimit := openClaimDepthHarness(t, claimDepthClaimers)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	keys := []claimDepthKeyBacklog{
		{FairnessKey: "claim-depth:alpha", Count: 64},
		{FairnessKey: "claim-depth:beta", Count: 64},
		{FairnessKey: "claim-depth:gamma", Count: 64},
		{FairnessKey: "claim-depth:delta", Count: 64},
		{FairnessKey: "claim-depth:epsilon", Count: 64},
	}
	seeded := seedClaimDepthBacklog(t, store, ctx, "claim-depth-even", base, keys)

	result := drainClaimDepthBacklog(t, database, ctx, claimDepthClaimers, poolLimit)

	assertClaimDepthConcurrency(t, result, claimDepthClaimers)
	assertClaimDepthExactlyOnce(t, seeded, result)

	var completedItems, claimRows, distinctClaimedItems int
	if err := database.QueryRowContext(ctx, `
SELECT
    (SELECT count(*) FROM workflow_work_items WHERE run_id = $1 AND status = 'completed'),
    (SELECT count(*) FROM workflow_claims AS claim
       JOIN workflow_work_items AS item ON item.work_item_id = claim.work_item_id
      WHERE item.run_id = $1 AND claim.status = 'completed'),
    (SELECT count(DISTINCT claim.work_item_id) FROM workflow_claims AS claim
       JOIN workflow_work_items AS item ON item.work_item_id = claim.work_item_id
      WHERE item.run_id = $1)
`, "claim-depth-even").Scan(&completedItems, &claimRows, &distinctClaimedItems); err != nil {
		t.Fatalf("count drained rows: %v", err)
	}
	if completedItems != len(seeded) || claimRows != len(seeded) || distinctClaimedItems != len(seeded) {
		t.Fatalf("db drain counts completed_items=%d claim_rows=%d distinct_claimed_items=%d, want all %d",
			completedItems, claimRows, distinctClaimedItems, len(seeded))
	}
	first := claimDepthFirstByKey(result.Records)
	for _, key := range keys {
		record, ok := first[key.FairnessKey]
		if !ok {
			t.Fatalf("no grant recorded for key %q", key.FairnessKey)
		}
		t.Logf("even first-claim key=%s seq=%d elapsed=%s", key.FairnessKey, record.Seq, record.Elapsed)
	}
	t.Logf("even drain items=%d claimers=%d duration=%s claims_per_second=%.1f empty_attempts=%d backends=%v",
		len(seeded), claimDepthClaimers, result.Duration, result.ClaimsPerSecond(), result.EmptyAttempts, result.BackendPIDs)
}

// TestWorkflowClaimDepthStarvationProbeMeasuresFIFOFloor records today's
// small-key first-claim latency under a large-key backlog. The shipped
// selector is pure FIFO inside one (kind, instance) and never reads
// fairness_key, so a small key seeded behind the large key cannot be
// granted until the large backlog is claimed. The probe asserts only that
// current truth, in grant order: every small key's first grant lands after
// the large backlog (minus the grants that can be in flight when Seq is
// assigned). #8469 must flip this floor; when it does, this test fails and
// is rewritten to the fair bound in the same change.
//
// This is also the stuck-pacing case #8468 requires to fail before any
// pacing code exists: asserting the fair bound (small-key first grant
// before the large backlog drains) against the shipped selector fails on
// real Postgres today (measured: first small grant seq 501 against a bound
// of 493), so a pacing or fairness mechanism that engages and never
// releases small keys is caught here instead of shipping unseen.
func TestWorkflowClaimDepthStarvationProbeMeasuresFIFOFloor(t *testing.T) {
	database, store, ctx, poolLimit := openClaimDepthHarness(t, claimDepthClaimers)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	const largeCount = 500
	largeKey := "claim-depth:large"
	smallKeys := []string{
		"claim-depth:small-1",
		"claim-depth:small-2",
		"claim-depth:small-3",
		"claim-depth:small-4",
	}
	keys := []claimDepthKeyBacklog{{FairnessKey: largeKey, Count: largeCount}}
	for _, small := range smallKeys {
		keys = append(keys, claimDepthKeyBacklog{FairnessKey: small, Count: 20, CreatedOffset: time.Second})
	}
	seeded := seedClaimDepthBacklog(t, store, ctx, "claim-depth-skew", base, keys)

	result := drainClaimDepthBacklog(t, database, ctx, claimDepthClaimers, poolLimit)

	assertClaimDepthConcurrency(t, result, claimDepthClaimers)
	assertClaimDepthExactlyOnce(t, seeded, result)

	first := claimDepthFirstByKey(result.Records)
	largeFirst, ok := first[largeKey]
	if !ok {
		t.Fatalf("no grant recorded for large key %q", largeKey)
	}
	// Seq is assigned on grant return, so up to claimers-1 large grants can
	// be recorded after the first small grant even though they were granted
	// before it. The floor keeps exactly that slack and no more.
	floor := int64(largeCount - claimDepthClaimers + 1)
	for _, small := range smallKeys {
		record, ok := first[small]
		if !ok {
			t.Fatalf("no grant recorded for small key %q", small)
		}
		if record.Seq < floor {
			t.Fatalf("small key %q first grant seq = %d, want >= %d under today's FIFO selector "+
				"(if fairness landed, rewrite this floor per #8469)", small, record.Seq, floor)
		}
		if record.Elapsed < largeFirst.Elapsed {
			t.Fatalf("small key %q first grant elapsed %s precedes large key first grant %s",
				small, record.Elapsed, largeFirst.Elapsed)
		}
		t.Logf("skew first-claim key=%s seq=%d elapsed=%s", small, record.Seq, record.Elapsed)
	}
	t.Logf("skew first-claim key=%s seq=%d elapsed=%s", largeKey, largeFirst.Seq, largeFirst.Elapsed)
	t.Logf("skew drain items=%d claimers=%d duration=%s claims_per_second=%.1f empty_attempts=%d backends=%v",
		len(seeded), claimDepthClaimers, result.Duration, result.ClaimsPerSecond(), result.EmptyAttempts, result.BackendPIDs)
}

// TestClaimDepthAnalysisHelpers pins the hermetic analysis the live proofs
// assert with, so a broken duplicate or first-claim computation fails
// without a database.
func TestClaimDepthAnalysisHelpers(t *testing.T) {
	records := []claimDepthRecord{
		{WorkItemID: "a", FairnessKey: "k1", ClaimID: "c1", Seq: 2},
		{WorkItemID: "b", FairnessKey: "k2", ClaimID: "c2", Seq: 3},
		{WorkItemID: "a", FairnessKey: "k1", ClaimID: "c3", Seq: 4},
		{WorkItemID: "c", FairnessKey: "k1", ClaimID: "c4", Seq: 1},
	}
	if duplicates := claimDepthDuplicateWorkItems(records); len(duplicates) != 1 || duplicates[0] != "a" {
		t.Fatalf("duplicates = %v, want [a]", duplicates)
	}
	first := claimDepthFirstByKey(records)
	if got := first["k1"].WorkItemID; got != "c" {
		t.Fatalf("first[k1] = %q, want c (lowest Seq)", got)
	}
	if got := first["k2"].WorkItemID; got != "b" {
		t.Fatalf("first[k2] = %q, want b", got)
	}
	if missing := claimDepthMissing([]string{"a", "b", "c", "d"}, records); len(missing) != 1 || missing[0] != "d" {
		t.Fatalf("missing = %v, want [d]", missing)
	}
	if missing := claimDepthMissing([]string{"a", "b", "c"}, records); len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8468 claim-issuance-under-depth harness proofs on real PostgreSQL: eight concurrent claimers on eight distinct pinned backends drain a seeded workflow backlog exactly once (no duplicate work-item or claim grant under SKIP LOCKED and the Read Committed EvalPlanQual recheck, one completed claim row per item), and the #8469 fair-rotation bound proves every key of a skewed backlog is first granted within the first draws (small keys no longer wait for the 500-item large key to drain); isolated schema from ESHU_POSTGRES_DSN with the full bootstrap applied by the harness opener; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

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

// TestWorkflowClaimDepthFairRotationServesSmallKeysEarly is the #8469 fair
// bound the FIFO-floor probe was rewritten into when the
// priority-then-rotation selector landed. Same skewed backlog (a 500-item
// large key, four 20-item small keys one second younger, one priority
// class), eight concurrent claimers: every key's first grant must land
// within the first draws, not after the large backlog drains. The bound
// keeps slack for the grants that can be in flight when Seq is assigned
// (at most claimers-1): a key missing from the first 8 draws has
// probability (4/5)^8 ≈ 2.6e-6 under the uniform rotation, so Seq 15 is a
// failure, not noise.
func TestWorkflowClaimDepthFairRotationServesSmallKeysEarly(t *testing.T) {
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

	const fairFirstGrantSeqBound = int64(claimDepthClaimers + 7)
	first := claimDepthFirstByKey(result.Records)
	assertClaimDepthMinGrantsPerOwner(t, result, claimDepthClaimers, 10)
	// The anti-serialization gate from the claim-pacing design: a fairness
	// change that fixes the tail by throttling the burst lowers aggregate
	// throughput and fails here. The 119 floor is the shipped FIFO rate on
	// the reference machine; this drain must hold it with all 8 claimers.
	if got := result.ClaimsPerSecond(); got < 119 {
		t.Fatalf("skew drain throughput = %.1f claims/s, want >= 119 (design floor)", got)
	}
	for _, key := range append([]string{largeKey}, smallKeys...) {
		record, ok := first[key]
		if !ok {
			t.Fatalf("no grant recorded for key %q", key)
		}
		if record.Seq > fairFirstGrantSeqBound {
			t.Fatalf("key %q first grant seq = %d, want <= %d under the fair rotation",
				key, record.Seq, fairFirstGrantSeqBound)
		}
		t.Logf("skew first-claim key=%s seq=%d elapsed=%s", key, record.Seq, record.Elapsed)
	}
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

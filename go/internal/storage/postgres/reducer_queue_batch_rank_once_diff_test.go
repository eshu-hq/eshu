// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// TestClaimBatchRankOnceRewriteMatchesPreRewriteCandidateSetAndOrder is the
// 0/0 differential proof for the #3624 Track 2 rank-once window rewrite: the
// new candidate SELECT must return the IDENTICAL work_item_id set, in the
// IDENTICAL order, as the pre-rewrite correlated-subquery candidate SELECT,
// on a fixture that exercises:
//
//   - multiple pending siblings sharing a conflict key,
//   - an expired claimed/running holder with an older pending sibling on the
//     same conflict key (the #4137 peer_flag=0 non-member edge — the expired
//     holder must remain the representative even though it is not the
//     fair_same_rn=1 fairness peer),
//   - multiple domains competing for per-domain fairness rank.
//
// This mirrors the live shim's bidirectional EXCEPT diff (0/0 on the full
// backlog and on a seeded expired-lease fixture) at the Go test level so the
// equivalence proof runs in CI without a live corpus. It executes against a
// disposable Postgres; skipped unless a DSN is provided so the package unit
// suite stays hermetic.
func TestClaimBatchRankOnceRewriteMatchesPreRewriteCandidateSetAndOrder(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the rank-once rewrite differential proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	now := time.Date(2026, time.July, 4, 12, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-rank-once-diff", now)

	const domainA = "supply_chain_impact"
	const domainB = "aws_cloud_runtime_drift"

	// Multiple pending siblings on one conflict key: only the earliest
	// (updated_at, work_item_id) may become the representative.
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: "diff-multi-sib-a", scopeID: "scope-rank-once-diff", generationID: "gen-fair",
		domain: domainA, conflictDomain: reducerConflictDomainScope, conflictKey: "diff-multi-key",
		updatedAt: now.Add(1 * time.Second),
	})
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: "diff-multi-sib-b", scopeID: "scope-rank-once-diff", generationID: "gen-fair",
		domain: domainA, conflictDomain: reducerConflictDomainScope, conflictKey: "diff-multi-key",
		updatedAt: now.Add(2 * time.Second),
	})

	// Expired claimed/running holder + an older pending sibling on the same
	// conflict key: #4137 requires the expired holder stay the
	// representative (same_rn = 1) even though the older pending sibling
	// would otherwise sort first by (updated_at, work_item_id), and even
	// though the holder is not necessarily the fair_same_rn = 1 fairness
	// peer (the fair_same window orders by is_search_doc/updated_at/id only,
	// ignoring claimed_running_first).
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: "diff-expired-older-pending", scopeID: "scope-rank-once-diff", generationID: "gen-fair",
		domain: domainA, conflictDomain: reducerConflictDomainScope, conflictKey: "diff-expired-key",
		updatedAt: now.Add(-time.Hour),
	})
	insertReducerFairnessClaimedWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: "diff-expired-holder", scopeID: "scope-rank-once-diff", generationID: "gen-fair",
		domain: domainA, conflictDomain: reducerConflictDomainScope, conflictKey: "diff-expired-key",
		updatedAt: now.Add(-30 * time.Minute),
	}, now.Add(-time.Minute)) // claim_until in the past: an expired, reclaimable lease.

	// Multi-domain competition for per-domain fairness rank: an older,
	// larger backlog in domainA and a newer, smaller set in domainB, each
	// row in its own conflict group.
	for i := 0; i < 6; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("diff-domainA-%02d", i), scopeID: "scope-rank-once-diff", generationID: "gen-fair",
			domain: domainA, conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("diff-domainA-key-%02d", i),
			updatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	for i := 0; i < 3; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("diff-domainB-%02d", i), scopeID: "scope-rank-once-diff", generationID: "gen-fair",
			domain: domainB, conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("diff-domainB-key-%02d", i),
			updatedAt: now.Add(10*time.Minute + time.Duration(i)*time.Minute),
		})
	}

	args := []any{
		now.Add(2 * time.Hour), // $1 p_now
		nil,                    // $2 domain filter (nil => no restriction)
		"n/a",                  // $3 lease owner (unused by read-only candidate select)
		now.Add(3 * time.Hour), // $4 claim_until (unused by read-only candidate select)
		false,                  // $5 require projector drain
		0,                      // $6 expected source-local projectors
		0,                      // $7 semantic entity claim limit
		100,                    // $8 batch limit (large enough to cover the whole fixture)
	}

	oldIDs := queryCandidateWorkItemIDs(t, ctx, db, oldReducerBatchCandidateSelectQuery, args)
	newIDs := queryCandidateWorkItemIDs(t, ctx, db, newReducerBatchCandidateSelectQuery, args)

	if len(oldIDs) == 0 {
		t.Fatal("fixture produced zero candidates from the OLD query; fixture is not exercising the claim path")
	}

	onlyInOld := diffStringSlices(oldIDs, newIDs)
	onlyInNew := diffStringSlices(newIDs, oldIDs)
	if len(onlyInOld) != 0 || len(onlyInNew) != 0 {
		t.Fatalf(
			"rank-once rewrite candidate set diverges from pre-rewrite candidate set (want 0/0): "+
				"only in OLD = %v, only in NEW = %v\nOLD order = %v\nNEW order = %v",
			onlyInOld, onlyInNew, oldIDs, newIDs,
		)
	}
	if len(oldIDs) != len(newIDs) || fmt.Sprint(oldIDs) != fmt.Sprint(newIDs) {
		t.Fatalf("rank-once rewrite candidate ORDER diverges from pre-rewrite order:\nOLD = %v\nNEW = %v", oldIDs, newIDs)
	}
}

// insertReducerFairnessClaimedWorkItem seeds a work item already in
// 'claimed' status with the given claim_until, reusing
// insertReducerFairnessWorkItem's column shape via a follow-up UPDATE so the
// #4137 expired-holder fixture does not need its own bespoke INSERT.
func insertReducerFairnessClaimedWorkItem(
	t *testing.T, ctx context.Context, db *sql.DB, item reducerFairnessWorkItem, claimUntil time.Time,
) {
	t.Helper()
	insertReducerFairnessWorkItem(t, ctx, db, item)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'expired-holder', claim_until = $2
WHERE work_item_id = $1`, item.workItemID, claimUntil); err != nil {
		t.Fatalf("mark fairness work item %q claimed: %v", item.workItemID, err)
	}
}

func queryCandidateWorkItemIDs(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query candidate work_item_ids: %v\nquery:\n%s", err, query)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan candidate work_item_id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate candidate work_item_ids: %v", err)
	}
	return ids
}

// diffStringSlices returns the elements of a that do not appear in b,
// preserving a's order. Used for the "only in OLD" / "only in NEW" 0/0 report.
func diffStringSlices(a, b []string) []string {
	inB := make(map[string]struct{}, len(b))
	for _, v := range b {
		inB[v] = struct{}{}
	}
	var diff []string
	for _, v := range a {
		if _, ok := inB[v]; !ok {
			diff = append(diff, v)
		}
	}
	return diff
}

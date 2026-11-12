// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"
)

// TestClaimBatchLockRecheckPredicatesPinnedByCount guards the EvalPlanQual
// row-self recheck on both batch-claim locking arms by exact count, not by
// substring presence (#10568 review F2). Each arm — the reserved
// search-doc arm and the rest arm — locks through its own per-candidate
// LATERAL probe and must carry the full lock_target lease/visibility/status
// recheck, so every predicate and the FOR UPDATE OF ... SKIP LOCKED clause
// must appear exactly twice in the live (comment-stripped) query. A future
// edit that drops the predicates from one arm (the reserved arm is the
// newer copy) fails here instead of silently reintroducing lease theft on
// that arm; the Contains pins in reducer_queue_batch_test.go cannot see
// that failure because one surviving copy satisfies them.
func TestClaimBatchLockRecheckPredicatesPinnedByCount(t *testing.T) {
	t.Parallel()

	// The query embeds -- explanations that name these shapes historically,
	// so strip line comments before counting: only live clauses count.
	uncommented := regexp.MustCompile(`(?m)--.*$`).ReplaceAllString(claimReducerWorkBatchQuery, "")
	for _, want := range []string{
		"JOIN LATERAL (",
		"FOR UPDATE OF lock_target SKIP LOCKED",
		"WHERE lock_target.stage = 'reducer'",
		"AND lock_target.status IN ('pending', 'retrying', 'claimed', 'running')",
		"AND (lock_target.claim_until IS NULL OR lock_target.claim_until <= $1)",
		"AND (lock_target.visible_at IS NULL OR lock_target.visible_at <= $1 OR (lock_target.status = 'pending' AND lock_target.visible_at <= clock_timestamp()))",
	} {
		if got := strings.Count(uncommented, want); got != 2 {
			t.Fatalf("lock recheck fragment %q appears %d times in the batch claim query, want exactly 2 "+
				"(one per locking arm; a dropped copy reintroduces lease theft on that arm):\n%s",
				want, got, claimReducerWorkBatchQuery)
		}
	}
}

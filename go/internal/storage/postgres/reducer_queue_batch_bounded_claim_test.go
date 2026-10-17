// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestClaimBatchQueryStaysBatchBounded is the #7596 item-3 regression guard.
//
// Item 3 flags fact_work_items bloat against the reducer claim's once-a-second
// per-worker poll. The drive shim measured the claim at ~2.3 ms idle and
// ~36 ms over a 4k backlog with fully disjoint SKIP LOCKED row sets: cost
// scales with the candidate set the statement scans before LIMIT. That bound
// only holds while the claim stays batch-bounded — dropping the LIMIT (or
// adding a second, unbounded one) turns every idle poll into a
// full-candidate scan. This test issues the production ClaimBatch path
// against the query fake and pins exactly one LIMIT clause, bound to the
// caller's batch size ($8), so a future edit cannot silently unbind the poll
// cost from the batch.
func TestClaimBatchQueryStaysBatchBounded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	q := ReducerQueue{
		database:      db,
		LeaseOwner:    "test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	if _, err := q.ClaimBatch(context.Background(), 5); err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "LIMIT $8") {
		t.Fatalf("batch claim query lost its batch bound (LIMIT $8):\n%s", query)
	}
	// The query embeds -- explanations that name historical shapes (e.g.
	// "ORDER BY ... LIMIT 1"), so strip line comments before counting: only
	// live clauses may satisfy or break the bound.
	uncommented := regexp.MustCompile(`(?m)--.*$`).ReplaceAllString(query, "")
	if limits := regexp.MustCompile(`(?i)\bLIMIT\b`).FindAllString(uncommented, -1); len(limits) != 1 {
		t.Fatalf("batch claim query has %d LIMIT clauses, want exactly 1 (LIMIT $8):\n%s", len(limits), query)
	}
	if got, want := db.queries[0].args[7], 5; got != want {
		t.Fatalf("batch claim $8 = %v, want %v (the caller limit must reach the bound)", got, want)
	}
}

// TestClaimBatchDefaultsNonPositiveLimitToDefaultBatch is the #7596 item-3
// companion guard: the idle-poll cost the shim measured assumes the
// production batch bound, not an unbounded claim. A zero or negative limit
// must fall back to the default batch size (16) rather than reaching the
// query as-is.
func TestClaimBatchDefaultsNonPositiveLimitToDefaultBatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	for _, limit := range []int{0, -3} {
		db := &fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: nil},
			},
		}
		q := ReducerQueue{
			database:      db,
			LeaseOwner:    "test",
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now },
		}

		if _, err := q.ClaimBatch(context.Background(), limit); err != nil {
			t.Fatalf("ClaimBatch(%d) error = %v", limit, err)
		}
		if got, want := db.queries[0].args[7], 16; got != want {
			t.Fatalf("ClaimBatch(%d) $8 = %v, want default %v", limit, got, want)
		}
	}
}

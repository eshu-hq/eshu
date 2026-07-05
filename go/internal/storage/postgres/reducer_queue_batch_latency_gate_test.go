// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// claimBatchLatencyBacklogSize is the seeded pending backlog the latency guard
// times the rank-once candidate select against. It is large enough that an
// O(N^2) regression on the claim query (the shape #4703 rewrote away) blows far
// past claimBatchLatencyBudget, while the shipped O(N) rank-once query stays in
// the tens of milliseconds.
const claimBatchLatencyBacklogSize = 5000

// claimBatchLatencyBudget is a deliberately GENEROUS wall-clock ceiling for one
// candidate select over claimBatchLatencyBacklogSize pending rows. The shipped
// rank-once query runs in ~tens of ms on this backlog; the pre-#4703 O(N^2)
// shape took multiple seconds even at ~900 rows, so at 5000 rows a regression is
// tens of seconds to minutes. The 8s budget is far above healthy latency and far
// below a quadratic regression, so it catches the regression without flaking on
// a slow CI runner.
const claimBatchLatencyBudget = 8 * time.Second

// TestReducerContentionGateClaimBatchLatencyBoundedOnSeededBacklog is the
// committed, Postgres-backed claim-latency guard for the #4703 rank-once rewrite
// (#4704). It seeds a large pending backlog, runs the production rank-once
// candidate select once, and asserts it completes within a generous absolute
// budget. This is the "prove wall-clock on the real query against the real
// backlog, not just EXPLAIN" discipline: it fails CI if a future edit to
// reducer_queue_batch_query.go reintroduces an O(N^2) tail, rather than only
// surfacing in a manual drain. The name shares the TestReducerContentionGate
// prefix so the existing reducer-contention-gate.yml `-run` filter runs it.
//
// Performance Evidence: on a resident full-corpus Postgres 18 the shipped
// rank-once candidate select over 5000 seeded pending rows completes in ~tens of
// milliseconds (well under the 8s budget); the pre-#4703 correlated-subquery
// shape is O(N^2) and would breach the budget at this backlog size.
func TestReducerContentionGateClaimBatchLatencyBoundedOnSeededBacklog(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the claim-batch latency guard")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	now := time.Date(2026, time.July, 4, 12, 0, 0, 0, time.UTC)
	const scopeID = "scope-claim-latency-gate"
	// openReducerFairnessDB runs in a throwaway schema dropped (CASCADE) on
	// cleanup, so the seeded scope and its work items need no explicit teardown.
	seedReducerFairnessScope(t, ctx, db, scopeID, now)

	// Each row sits in its own conflict group (unique conflict key) so the
	// candidate select must rank the full backlog — the worst case for a
	// per-conflict-group ranking query.
	const domain = "supply_chain_impact"
	seedReducerFairnessLatencyBacklog(t, ctx, db, scopeID, domain, now, claimBatchLatencyBacklogSize)

	args := []any{
		now.Add(2 * time.Hour), // $1 p_now
		nil,                    // $2 domain filter (nil => no restriction)
		"n/a",                  // $3 lease owner (unused by read-only candidate select)
		now.Add(3 * time.Hour), // $4 claim_until (unused by read-only candidate select)
		false,                  // $5 require projector drain
		0,                      // $6 expected source-local projectors
		0,                      // $7 semantic entity claim limit
		200,                    // $8 batch limit
	}

	// Hard backstop only: the explicit elapsed > claimBatchLatencyBudget check
	// below is what gives the clear regression message, so this deadline sits
	// above the budget (+4s cushion) and should never be the one that fires on
	// a normal slow-but-not-hung regression.
	selectCtx, cancel := context.WithTimeout(ctx, claimBatchLatencyBudget+4*time.Second)
	defer cancel()

	start := time.Now()
	ids := queryCandidateWorkItemIDs(t, selectCtx, db, newReducerBatchCandidateSelectQuery, args)
	elapsed := time.Since(start)

	if len(ids) == 0 {
		t.Fatalf("candidate select returned zero rows over a %d-row backlog; fixture is not exercising the claim path", claimBatchLatencyBacklogSize)
	}
	t.Logf("rank-once candidate select over %d pending rows: %v (budget %v)", claimBatchLatencyBacklogSize, elapsed, claimBatchLatencyBudget)
	if elapsed > claimBatchLatencyBudget {
		t.Fatalf("rank-once candidate select took %v over %d rows, exceeding the %v budget — likely an O(N^2) regression on reducer_queue_batch_query.go",
			elapsed, claimBatchLatencyBacklogSize, claimBatchLatencyBudget)
	}
}

// reducerFairnessLatencySeedRowParams is the count of distinct bound
// parameters per row in seedReducerFairnessLatencyBacklog's batched INSERT
// (work_item_id, scope_id, generation_id, domain, conflict_domain,
// conflict_key, source_system, updated_at — work_item_id and updated_at are
// each referenced twice within the row via repeated $N placeholders, matching
// insertReducerFairnessWorkItem's single-row statement).
const reducerFairnessLatencySeedRowParams = 8

// reducerFairnessLatencySeedBatchSize caps each multi-row INSERT VALUES list.
// At reducerFairnessLatencySeedRowParams params/row, 1000 rows per statement
// is 8000 bound params, comfortably under Postgres's 65535-param limit per
// statement.
const reducerFairnessLatencySeedBatchSize = 1000

// seedReducerFairnessLatencyBacklog seeds n pending fact_work_items rows for
// scopeID/domain via batched multi-row INSERTs, replicating the exact column
// list, defaults, and per-row values that insertReducerFairnessWorkItem
// produces (stage='reducer', conflict_domain=reducerConflictDomainScope,
// status='pending', attempt_count=0, source_system='aws', the same
// jsonb_build_object payload shape, created_at=updated_at=the row's
// timestamp) so the seeded rows are byte-equivalent to what the old
// insertReducerFairnessWorkItem loop produced. This only exists to make the
// latency-gate seed fast; insertReducerFairnessWorkItem itself is left
// untouched since other tests call it directly.
func seedReducerFairnessLatencyBacklog(
	t *testing.T, ctx context.Context, db *sql.DB, scopeID, domain string, now time.Time, n int,
) {
	t.Helper()
	for start := 0; start < n; start += reducerFairnessLatencySeedBatchSize {
		end := start + reducerFairnessLatencySeedBatchSize
		if end > n {
			end = n
		}
		var sb strings.Builder
		sb.WriteString(`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, payload, created_at, updated_at
) VALUES `)
		args := make([]any, 0, (end-start)*reducerFairnessLatencySeedRowParams)
		for i := start; i < end; i++ {
			if i > start {
				sb.WriteString(",")
			}
			p := len(args)
			// $p+1=work_item_id, $p+2=scope_id, $p+3=generation_id,
			// $p+4=domain, $p+5=conflict_domain, $p+6=conflict_key,
			// $p+7=source_system, $p+8=updated_at (work_item_id and
			// updated_at each reused for a second column below).
			fmt.Fprintf(&sb, "($%d::text, $%d, $%d, 'reducer', $%d, $%d, $%d, 'pending', 0, "+
				"jsonb_build_object('entity_key', $%d::text, 'reason', 'fairness', 'fact_id', $%d::text, 'source_system', $%d::text), $%d, $%d)",
				p+1, p+2, p+3, p+4, p+5, p+6, p+1, p+1, p+7, p+8, p+8)
			workItemID := fmt.Sprintf("latency-%05d", i)
			conflictKey := fmt.Sprintf("latency-key-%05d", i)
			updatedAt := now.Add(time.Duration(i) * time.Millisecond)
			args = append(args,
				workItemID, scopeID, "gen-fair", domain, reducerConflictDomainScope, conflictKey,
				"aws", updatedAt,
			)
		}
		if _, err := db.ExecContext(ctx, sb.String(), args...); err != nil {
			t.Fatalf("batched insert fairness work items [%d,%d): %v", start, end, err)
		}
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// seedSortedLockScope inserts one scope for the given source system.
func seedSortedLockScope(t *testing.T, database *sql.DB, scopeID, sourceSystem string) {
	t.Helper()
	if _, err := database.Exec(`
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ($1, 'repository', $2, $1, $2, $1, now(), now(), 'active')
`, scopeID, sourceSystem); err != nil {
		t.Fatalf("insert scope %s: %v", scopeID, err)
	}
}

// claimGenerationInRollback runs query as the claim statement inside a
// transaction it rolls back, and returns the claimed generation ("" when the
// claim found nothing).
func claimGenerationInRollback(t *testing.T, database *sql.DB, query string, at time.Time) string {
	t.Helper()
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	// A claim that waits on a held row lock must fail, not hang the test.
	if _, err := tx.Exec(`SET LOCAL lock_timeout = '5s'`); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	var generation string
	err = tx.QueryRow(query, at, "differential", at.Add(time.Minute), "").Scan(
		new(string), new(string), new(string), new(string), new(string), new(bool), new(string), new(string),
		&generation, new(int), new(time.Time), new(time.Time), new(string), new(string), new(string), new([]byte),
	)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return generation
}

// TestProjectorClaimSortedLockStepMatchesWholePool is the sort-then-probe
// differential (#7198): the shipped claim, whose lock step reads a sorted
// subquery and stops at the first lockable row, must pick exactly the
// candidate the whole-pool lock step of b67c3bd5ca picks. The reference text
// is derived from the shipped constant (wholePoolClaimQuery). Each state runs
// both texts in rolled-back transactions on the same seed.
func TestProjectorClaimSortedLockStepMatchesWholePool(t *testing.T) {
	whole, ok := wholePoolClaimQuery()
	if !ok {
		t.Fatal("cannot derive the whole-pool reference from claimProjectorWorkQuery")
	}
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second)
	reset := func(t *testing.T) {
		t.Helper()
		if _, err := database.Exec(`DELETE FROM fact_work_items; DELETE FROM scope_generations; DELETE FROM ingestion_scopes`); err != nil {
			t.Fatalf("reset: %v", err)
		}
	}
	work := func(t *testing.T, scopeID, generationID, status string, updatedAt time.Time, claimUntil *time.Time) {
		t.Helper()
		if _, err := database.Exec(`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'push', $3, $3, 'pending')`, generationID, scopeID, updatedAt); err != nil {
			t.Fatalf("insert generation %s: %v", generationID, err)
		}
		var owner any
		if claimUntil != nil {
			owner = "other-worker"
		}
		if _, err := database.Exec(`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at, last_attempt_at
) VALUES ($1, $2, $3, 'projector', 'source_local', $4, 1, $5, $6, $7, '{}'::jsonb, $7, $7, $7)`,
			projectorWorkItemID(scopeID, generationID), scopeID, generationID, status, owner, claimUntil, updatedAt); err != nil {
			t.Fatalf("insert work %s: %v", generationID, err)
		}
	}
	past := func(d time.Duration) *time.Time { v := at.Add(-d); return &v }
	future := func(d time.Duration) *time.Time { v := at.Add(d); return &v }

	cases := []struct {
		name string
		seed func(t *testing.T)
		// hold is an optional row lock another transaction holds while both
		// texts claim.
		hold string
	}{
		{
			name: "ties_on_updated_at_break_on_work_item_id",
			seed: func(t *testing.T) {
				for _, id := range []string{"scope-c", "scope-a", "scope-b"} {
					seedSortedLockScope(t, database, id, "git")
					work(t, id, "gen-"+id, "pending", at.Add(-time.Hour), nil)
				}
			},
		},
		{
			name: "expired_lease_reclaim_ranks_first",
			seed: func(t *testing.T) {
				seedSortedLockScope(t, database, "scope-old", "git")
				work(t, "scope-old", "gen-old", "pending", at.Add(-3*time.Hour), nil)
				seedSortedLockScope(t, database, "scope-exp", "git")
				work(t, "scope-exp", "gen-exp", "claimed", at.Add(-time.Hour), past(time.Minute))
			},
		},
		{
			name: "source_with_inflight_lease_ranks_after_quiet_source",
			seed: func(t *testing.T) {
				seedSortedLockScope(t, database, "scope-git-live", "git")
				work(t, "scope-git-live", "gen-git-live", "claimed", at.Add(-4*time.Hour), future(time.Hour))
				seedSortedLockScope(t, database, "scope-git", "git")
				work(t, "scope-git", "gen-git", "pending", at.Add(-3*time.Hour), nil)
				seedSortedLockScope(t, database, "scope-aws", "aws")
				work(t, "scope-aws", "gen-aws", "pending", at.Add(-time.Hour), nil)
			},
		},
		{
			name: "busy_first_fence_row_moves_to_next",
			seed: func(t *testing.T) {
				for i, id := range []string{"scope-1", "scope-2", "scope-3"} {
					seedSortedLockScope(t, database, id, "git")
					work(t, id, "gen-"+id, "pending", at.Add(-time.Duration(3-i)*time.Hour), nil)
				}
			},
			hold: `SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-1' FOR NO KEY UPDATE`,
		},
		{
			name: "busy_first_work_row_moves_to_next",
			seed: func(t *testing.T) {
				for i, id := range []string{"scope-1", "scope-2", "scope-3"} {
					seedSortedLockScope(t, database, id, "git")
					work(t, id, "gen-"+id, "pending", at.Add(-time.Duration(3-i)*time.Hour), nil)
				}
			},
			hold: fmt.Sprintf(`SELECT 1 FROM fact_work_items WHERE work_item_id = '%s' FOR UPDATE`, projectorWorkItemID("scope-1", "gen-scope-1")),
		},
		{
			name: "every_scope_busy_claims_nothing",
			seed: func(t *testing.T) {
				seedSortedLockScope(t, database, "scope-1", "git")
				work(t, "scope-1", "gen-scope-1", "pending", at.Add(-time.Hour), nil)
			},
			hold: `SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-1' FOR NO KEY UPDATE`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reset(t)
			tc.seed(t)
			if tc.hold != "" {
				holder, err := database.BeginTx(ctx, nil)
				if err != nil {
					t.Fatalf("begin holder: %v", err)
				}
				defer func() { _ = holder.Rollback() }()
				if _, err := holder.Exec(tc.hold); err != nil {
					t.Fatalf("hold: %v", err)
				}
			}
			shipped := claimGenerationInRollback(t, database, claimProjectorWorkQuery, at)
			reference := claimGenerationInRollback(t, database, whole, at)
			t.Logf("sorted lock step claimed %q, whole-pool lock step claimed %q", shipped, reference)
			if shipped != reference {
				t.Fatalf("sorted lock step claimed %q, whole-pool lock step claimed %q", shipped, reference)
			}
		})
	}
}

// planNode is the subset of EXPLAIN (FORMAT JSON) this test reads.
type planNode struct {
	NodeType     string     `json:"Node Type"`
	SubplanName  string     `json:"Subplan Name"`
	RelationName string     `json:"Relation Name"`
	ActualLoops  int        `json:"Actual Loops"`
	Plans        []planNode `json:"Plans"`
}

func findPlanNode(node planNode, match func(planNode) bool) (planNode, bool) {
	if match(node) {
		return node, true
	}
	for _, child := range node.Plans {
		if found, ok := findPlanNode(child, match); ok {
			return found, true
		}
	}
	return planNode{}, false
}

// TestProjectorClaimSortedLockStepStopsAtFirstLockableRow is the live plan
// check for the sort-then-probe lock step (#7198). The first candidate's fence
// row is held by another transaction and 30 scopes are ready. The claim's
// candidate CTE must plan as Limit -> LockRows -> Nested Loop with no Sort
// above the join, and the fence and work probes must run once per pool row
// visited: 2 (the skipped first candidate and the claimed second), not 30.
// A second, real claim confirms it takes the second candidate.
//
// The plan depends on the planner choosing a nested loop over the sorted
// pool. The CTE's row estimate does not vary with the seed, so that choice is
// stable for this shape; a planner that falls back to a hash join would keep
// the claim correct but fail this test.
func TestProjectorClaimSortedLockStepStopsAtFirstLockableRow(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		scopeID := fmt.Sprintf("scope-%02d", i)
		seedClaimMaintenanceScopes(t, database, scopeID)
		seedClaimMaintenanceWork(t, database, scopeID, "gen-"+scopeID, "pending", "pending", time.Duration(30-i)*time.Hour, nil)
	}
	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.Exec(`SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-00' FOR NO KEY UPDATE`); err != nil {
		t.Fatalf("hold first fence row: %v", err)
	}

	at := time.Now().UTC()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain: %v", err)
	}
	if _, err := tx.Exec(`SET LOCAL lock_timeout = '5s'`); err != nil {
		_ = tx.Rollback()
		t.Fatalf("set lock_timeout: %v", err)
	}
	var raw []byte
	if err := tx.QueryRow("EXPLAIN (ANALYZE, FORMAT JSON) "+claimProjectorWorkQuery,
		at, "planner", at.Add(time.Minute), "").Scan(&raw); err != nil {
		_ = tx.Rollback()
		t.Fatalf("explain claim: %v", err)
	}
	_ = tx.Rollback()
	var plans []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	candidate, ok := findPlanNode(plans[0].Plan, func(n planNode) bool { return n.SubplanName == "CTE candidate" })
	if !ok {
		t.Fatalf("plan has no CTE candidate:\n%s", raw)
	}
	if candidate.NodeType != "Limit" || len(candidate.Plans) != 1 || candidate.Plans[0].NodeType != "LockRows" {
		t.Fatalf("CTE candidate = %s -> %v, want Limit -> LockRows:\n%s", candidate.NodeType, candidate.Plans, raw)
	}
	lockRows := candidate.Plans[0]
	if len(lockRows.Plans) != 1 || lockRows.Plans[0].NodeType != "Nested Loop" {
		t.Fatalf("LockRows input = %v, want a Nested Loop with no Sort above the join:\n%s", lockRows.Plans, raw)
	}
	// EXPLAIN renames repeated aliases (work_2 and so on), so match the
	// relation; the lock step's join holds exactly one probe of each.
	for _, relation := range []string{"projector_scope_claim_fences", "fact_work_items"} {
		node, ok := findPlanNode(lockRows, func(n planNode) bool { return n.RelationName == relation })
		if !ok {
			t.Fatalf("lock step has no %s probe:\n%s", relation, raw)
		}
		if node.ActualLoops != 2 {
			t.Fatalf("%s probe ran %d loops, want 2 (skipped first candidate, claimed second)", relation, node.ActualLoops)
		}
	}

	if got := claimGenerationInRollback(t, database, claimProjectorWorkQuery, at); got != "gen-scope-01" {
		t.Fatalf("claim with scope-00's fence row held = %q, want gen-scope-01", got)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestClaimBatchLockRecheckDropsConcurrentlyClaimedRow is the failing-then-green
// regression for the #3624 rank-once rewrite's `locked` CTE lease-theft window.
//
// The rewrite moved FOR UPDATE SKIP LOCKED off the predicate-bearing candidate
// SELECT onto an outer `locked` CTE that joins `lock_target` to the (statement
// snapshot) `candidate` set. If `lock_target`'s only qual is that id join, then
// under Read Committed a row another worker claimed and committed between our
// snapshot and this lock still satisfies the join: PostgreSQL's EvalPlanQual
// recheck re-runs the locking CTE's quals against the updated row, finds only
// the id match, locks it, and the `claimed` UPDATE overwrites the other worker's
// fresh lease (lease theft / duplicate work). Re-applying the row-self
// lease/visibility/status predicates on `lock_target` makes EvalPlanQual drop
// such a row, exactly as the pre-rewrite query did when FOR UPDATE sat directly
// on the candidate SELECT.
//
// This test reproduces the EvalPlanQual recheck deterministically on a minimal
// mirror of the `locked` CTE shape: a third connection holds a FOR UPDATE lock
// on the row (forcing the lock to block instead of racing), the row is then
// claimed+committed while the locker waits, and we observe whether the locker
// still returns it. It asserts the id-join-only shape steals and the shape that
// re-applies the predicates (byte-identical to what claimReducerWorkBatchQuery's
// `locked` CTE now uses — see the WHERE lock_target.* assertions in
// TestClaimBatchFencesSameConflictCandidates) is safe.
//
// Production uses SKIP LOCKED rather than a blocking FOR UPDATE, but the
// EvalPlanQual recheck qual set is identical; SKIP LOCKED only changes behavior
// while another transaction HOLDS the lock, not after it commits and releases —
// which is exactly the theft window this test drives.
func TestClaimBatchLockRecheckDropsConcurrentlyClaimedRow(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the lock-recheck regression")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	table := fmt.Sprintf("_epq_probe_%d", time.Now().UnixNano())
	mustExec(t, ctx, db, fmt.Sprintf(`
CREATE TABLE %s (
    work_item_id text PRIMARY KEY,
    stage        text NOT NULL,
    status       text NOT NULL,
    claim_until  timestamptz,
    visible_at   timestamptz
)`, table))
	defer func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table) }()

	// The id-join-only shape (the #3624 bug): lock_target has no predicates.
	buggyLock := fmt.Sprintf(`
WITH candidate AS (SELECT 'r1'::text AS work_item_id)
SELECT lock_target.work_item_id
FROM %s AS lock_target
JOIN candidate ON candidate.work_item_id = lock_target.work_item_id
FOR UPDATE OF lock_target`, table)

	// The fixed shape: the row-self lease/visibility/status predicates the
	// shipped `locked` CTE re-applies on lock_target.
	fixedLock := fmt.Sprintf(`
WITH candidate AS (SELECT 'r1'::text AS work_item_id)
SELECT lock_target.work_item_id
FROM %s AS lock_target
JOIN candidate ON candidate.work_item_id = lock_target.work_item_id
WHERE lock_target.stage = 'reducer'
  AND lock_target.status IN ('pending', 'retrying', 'claimed', 'running')
  AND (lock_target.claim_until IS NULL OR lock_target.claim_until <= now())
  AND (lock_target.visible_at IS NULL OR lock_target.visible_at <= now())
FOR UPDATE OF lock_target`, table)

	if locked := runLockRecheckCase(t, ctx, db, table, buggyLock); !equalStrings(locked, []string{"r1"}) {
		t.Fatalf("id-join-only lock did not reproduce the lease-theft window: locked=%v, want [r1]", locked)
	}
	if locked := runLockRecheckCase(t, ctx, db, table, fixedLock); len(locked) != 0 {
		t.Fatalf("lease-predicate lock overwrote a concurrently-claimed row (lease theft): locked=%v, want []", locked)
	}
}

// runLockRecheckCase seeds row r1 pending, has a holder connection lock it, runs
// lockSQL in a goroutine (which blocks on the holder), then claims+commits r1
// from the holder and returns the ids the locker ended up locking.
func runLockRecheckCase(t *testing.T, ctx context.Context, db *sql.DB, table, lockSQL string) []string {
	t.Helper()
	mustExec(t, ctx, db, fmt.Sprintf(
		"INSERT INTO %s (work_item_id, stage, status, claim_until, visible_at) "+
			"VALUES ('r1','reducer','pending',NULL,NULL) "+
			"ON CONFLICT (work_item_id) DO UPDATE SET status='pending', claim_until=NULL, visible_at=NULL", table))

	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("holder conn: %v", err)
	}
	defer func() { _ = holder.Close() }()

	holderTx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := holderTx.ExecContext(ctx,
		fmt.Sprintf("SELECT work_item_id FROM %s WHERE work_item_id='r1' FOR UPDATE", table)); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	var (
		wg      sync.WaitGroup
		locked  []string
		lockErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			lockErr = err
			return
		}
		defer func() { _ = tx.Rollback() }()
		rows, err := tx.QueryContext(ctx, lockSQL) // blocks until holder commits
		if err != nil {
			lockErr = err
			return
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				lockErr = err
				return
			}
			locked = append(locked, id)
		}
		lockErr = rows.Err()
	}()

	// Let the locker block on the holder's lock before the concurrent claim.
	time.Sleep(750 * time.Millisecond)

	// The concurrent worker claims r1 with a fresh future lease and commits,
	// releasing its lock so the blocked locker proceeds into EvalPlanQual.
	if _, err := holderTx.ExecContext(ctx, fmt.Sprintf(
		"UPDATE %s SET status='claimed', claim_until=now() + interval '1 hour' WHERE work_item_id='r1'", table)); err != nil {
		t.Fatalf("concurrent claim: %v", err)
	}
	if err := holderTx.Commit(); err != nil {
		t.Fatalf("holder commit: %v", err)
	}

	wg.Wait()
	if lockErr != nil {
		t.Fatalf("locker query: %v", lockErr)
	}
	return locked
}

func mustExec(t *testing.T, ctx context.Context, db *sql.DB, sqlText string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, sqlText); err != nil {
		t.Fatalf("exec %q: %v", sqlText, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestCrossScopeCompletionEventAfterFanoutSnapshotRemainsPendingLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	db.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	const (
		scopeID    = "repository:5740-snapshot"
		generation = "generation:5740-snapshot"
		supplyID   = "reducer_5740_snapshot_supply"
		owner      = "snapshot-fanout-owner"
		lockKey    = int64(57405462)
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate snapshot scope: %v", err)
	}
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, supplyID, scopeID, generation,
		reducer.DomainSupplyChainImpact, now,
	)
	eventA := insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainContainerImageIdentity,
		"pending", "", time.Time{}, 0, now.Add(-time.Second),
	)
	store := NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return now }
	lease, ok, err := store.Claim(ctx, owner, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim snapshot event A = %+v ok=%t err=%v", lease, ok, err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
CREATE FUNCTION block_cross_scope_schedule() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(%d);
    RETURN NEW;
END
$$;
CREATE TRIGGER a_block_cross_scope_schedule
BEFORE UPDATE ON fact_work_items
FOR EACH ROW
WHEN (OLD.status = 'succeeded' AND NEW.status = 'pending')
EXECUTE FUNCTION block_cross_scope_schedule()
`, lockKey)); err != nil {
		t.Fatalf("install snapshot blocker: %v", err)
	}
	blocker, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open snapshot blocker connection: %v", err)
	}
	defer blocker.Close()
	if _, err := blocker.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		t.Fatalf("hold snapshot advisory lock: %v", err)
	}
	defer func() {
		_, _ = blocker.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	type fanoutOutcome struct {
		result reducer.CrossScopeCompletionResult
		err    error
	}
	completed := make(chan fanoutOutcome, 1)
	go func() {
		result, fanoutErr := store.Fanout(ctx, lease, 500)
		completed <- fanoutOutcome{result: result, err: fanoutErr}
	}()
	waitForCrossScopeAdvisoryWaiter(t, ctx, db, lockKey)
	eventB := insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainContainerImageIdentity,
		"pending", "", time.Time{}, 0, now,
	)
	if _, err := blocker.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
		t.Fatalf("release snapshot advisory lock: %v", err)
	}
	outcome := <-completed
	if outcome.err != nil {
		t.Fatalf("fanout captured event A: %v", outcome.err)
	}
	if outcome.result.EventsProcessed != 1 || outcome.result.ProducerItemsProcessed != 1 {
		t.Fatalf("snapshot fanout result = %+v, want one captured event/item", outcome.result)
	}
	assertCrossScopeCompletionEventState(t, ctx, db, eventA, "")
	assertCrossScopeCompletionEventState(t, ctx, db, eventB, "pending")
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)
}

func waitForCrossScopeAdvisoryWaiter(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	lockKey int64,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_locks
    WHERE locktype = 'advisory'
      AND NOT granted
      AND classid = 0
      AND objid = $1::oid
)
`, lockKey).Scan(&waiting); err != nil {
			t.Fatalf("read snapshot advisory waiter: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fanout did not reach the snapshot advisory blocker")
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10084 two-shard completion: a committed shard opens epoch 1 and waits holding no advisory lock while a join-only refused-only shard joins, leads the maintenance pass, and marks completion; a join-only arrival before any open epoch records nothing; a waiter cancelled mid-wait returns promptly; disposable database from ESHU_POSTGRES_DSN; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestDeferredMaintenanceBarrierTwoShardCompletionLive is the #10084
// acceptance-criterion live proof: shard 0 commits and drains while shard 1
// is refused-only (HasCommitted=false). The epoch must complete with both
// arrivals recorded.
//
// It also pins the diagnosis of the first throwaway harness deadlock
// (#10084 comment 6670692993): a join-only arrival that lands before any
// epoch is open records nothing and returns nil, so an opener that arrives
// after it waits for a peer that already came and went. That is a harness
// ordering artifact, not a product deadlock — the no-epoch early return and
// the unbounded wait are both the designed contract — so this proof orders
// the epoch open before the join and asserts each intermediate state.
//
// The final phase observes the F5 shutdown path live: a shard parked in the
// unbounded completion wait returns promptly when its context is cancelled,
// holding no advisory lock.
func TestDeferredMaintenanceBarrierTwoShardCompletionLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #10084 two-shard completion proof")
	}
	if os.Getenv("ESHU_POSTGRES_DISPOSABLE") != "1" {
		t.Fatalf("ESHU_POSTGRES_DSN is set without ESHU_POSTGRES_DISPOSABLE=1: refusing a non-disposable proof target")
	}
	admin, err := postgresproof.WithDatabase(dsn, "postgres")
	if err != nil {
		t.Fatalf("retarget proof DSN to the administrative database: %v", err)
	}
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, admin,
		os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	ctx := context.Background()
	var targetDB string
	if err := database.QueryRowContext(ctx, "SELECT current_database()").Scan(&targetDB); err != nil {
		t.Fatalf("read proof database: %v", err)
	}
	targetDSN, err := postgresproof.WithDatabase(admin, targetDB)
	if err != nil {
		t.Fatalf("target proof database: %v", err)
	}
	openShard := func() *sql.DB {
		db, err := sql.Open("pgx", targetDSN)
		if err != nil {
			t.Fatalf("open shard handle: %v", err)
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	shard0DB, shard1DB := openShard(), openShard()
	store0 := NewIngestionStore(SQLDB{DB: shard0DB})
	store1 := NewIngestionStore(SQLDB{DB: shard1DB})

	barrierRows := func() (barriers, arrivals int) {
		t.Helper()
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM deferred_maintenance_barriers WHERE barrier_name = 'ingester_deferred_relationship_maintenance'`,
		).Scan(&barriers); err != nil {
			t.Fatalf("count barriers: %v", err)
		}
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM deferred_maintenance_barrier_arrivals WHERE barrier_name = 'ingester_deferred_relationship_maintenance'`,
		).Scan(&arrivals); err != nil {
			t.Fatalf("count arrivals: %v", err)
		}
		return barriers, arrivals
	}
	epochState := func(epoch int64) (arrivals int, completed bool) {
		t.Helper()
		var completedAt sql.NullTime
		if err := database.QueryRowContext(ctx,
			`SELECT completed_at FROM deferred_maintenance_barriers WHERE barrier_name = 'ingester_deferred_relationship_maintenance' AND epoch = $1`,
			epoch,
		).Scan(&completedAt); err != nil {
			t.Fatalf("read epoch %d: %v", epoch, err)
		}
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM deferred_maintenance_barrier_arrivals WHERE barrier_name = 'ingester_deferred_relationship_maintenance' AND epoch = $1`,
			epoch,
		).Scan(&arrivals); err != nil {
			t.Fatalf("count epoch %d arrivals: %v", epoch, err)
		}
		return arrivals, completedAt.Valid
	}
	waitFor := func(timeout time.Duration, message string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out after %s waiting for %s", timeout, message)
	}
	advisoryLocks := func() int {
		t.Helper()
		var count int
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory'`,
		).Scan(&count); err != nil {
			t.Fatalf("count advisory locks: %v", err)
		}
		return count
	}

	// Phase 1 — a join-only arrival before any open epoch is a no-op. This
	// is the throwaway-harness artifact mechanism: shard 1 arriving first
	// records nothing and returns, so an opener that starts after it waits
	// for a peer that already came and went.
	if err := store1.RunDeferredRelationshipMaintenanceAfterShardDrain(ctx,
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 1, HasCommitted: false},
		nil, nil); err != nil {
		t.Fatalf("early join-only arrival: %v", err)
	}
	if barriers, arrivals := barrierRows(); barriers != 0 || arrivals != 0 {
		t.Fatalf("early join-only arrival wrote barriers=%d arrivals=%d, want 0 0", barriers, arrivals)
	}

	// Phase 2 — the committed shard opens epoch 1 and waits. The wait must
	// hold no advisory lock: the arrival transaction already committed, so
	// the parked waiter cannot be party to a lock cycle.
	openerCtx, openerCancel := context.WithCancel(ctx)
	defer openerCancel()
	openerDone := make(chan error, 1)
	go func() {
		openerDone <- store0.RunDeferredRelationshipMaintenanceAfterShardDrain(openerCtx,
			DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 0, HasCommitted: true},
			nil, nil)
	}()
	waitFor(30*time.Second, "epoch 1 to open", func() bool {
		barriers, _ := barrierRows()
		return barriers == 1
	})
	waitFor(30*time.Second, "shard 0 arrival", func() bool {
		arrivals, _ := epochState(1)
		return arrivals == 1
	})
	if _, completed := epochState(1); completed {
		t.Fatal("epoch 1 completed with only one arrival")
	}
	if got := advisoryLocks(); got != 0 {
		t.Fatalf("parked opener holds %d advisory locks, want 0 (arrival committed before the wait)", got)
	}
	select {
	case err := <-openerDone:
		t.Fatalf("opener returned early with err=%v while epoch 1 is still open", err)
	case <-time.After(2 * time.Second):
	}

	// Phase 3 — the refused-only shard joins the open epoch, leads the
	// maintenance pass as the final arrival, and marks completion; the
	// opener observes it.
	if err := store1.RunDeferredRelationshipMaintenanceAfterShardDrain(ctx,
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 1, HasCommitted: false},
		nil, nil); err != nil {
		t.Fatalf("join-only completion: %v", err)
	}
	select {
	case err := <-openerDone:
		if err != nil {
			t.Fatalf("opener: %v", err)
		}
	case <-time.After(5 * time.Minute):
		t.Fatal("opener never observed completion after the join-only shard arrived")
	}
	if arrivals, completed := epochState(1); arrivals != 2 || !completed {
		t.Fatalf("epoch 1 state = arrivals=%d completed=%v, want arrivals=2 completed=true", arrivals, completed)
	}
	t.Logf("epoch 1 completed with both arrivals recorded")

	// Phase 4 (F5) — a shard parked in the unbounded completion wait exits
	// promptly on cancellation. The opener records its arrival for epoch 2
	// and parks; cancelling must return context.Canceled without recording
	// completion.
	shutdownCtx, shutdownCancel := context.WithCancel(ctx)
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- store0.RunDeferredRelationshipMaintenanceAfterShardDrain(shutdownCtx,
			DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 0, HasCommitted: true},
			nil, nil)
	}()
	waitFor(30*time.Second, "epoch 2 to open", func() bool {
		barriers, _ := barrierRows()
		return barriers == 2
	})
	waitFor(30*time.Second, "shard 0 epoch-2 arrival", func() bool {
		arrivals, _ := epochState(2)
		return arrivals == 1
	})
	shutdownCancel()
	select {
	case err := <-shutdownDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled wait returned err=%v, want context.Canceled", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancelled waiter did not return within 15s (poll interval is 250ms)")
	}
	if arrivals, completed := epochState(2); arrivals != 1 || completed {
		t.Fatalf("epoch 2 state after cancel = arrivals=%d completed=%v, want arrivals=1 completed=false", arrivals, completed)
	}
	t.Logf("F5 shutdown: cancelled waiter returned context.Canceled with epoch 2 still open")
}

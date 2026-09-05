// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// A held first key must stop every writer before it owns any later key. The
// reverse heap order makes this an ordering assertion, not a probabilistic race.
func TestReducerAckFanoutCommonLockOrderLive(t *testing.T) {
	for _, variant := range []struct {
		name   string
		domain reducer.Domain
		fanout bool
		stale  bool
		dirty  bool
	}{
		{"generic", reducer.DomainSupplyChainImpact, false, false, false},
		{"identity", reducer.DomainContainerImageIdentity, false, false, false},
		{"cicd", reducer.DomainCICDRunCorrelation, false, false, false},
		{"fanout", reducer.DomainSupplyChainImpact, true, false, false},
		{"generic_stale_owner", reducer.DomainSupplyChainImpact, false, true, false},
		{"identity_stale_epoch", reducer.DomainContainerImageIdentity, false, true, false},
		{"cicd_stale_owner", reducer.DomainCICDRunCorrelation, false, true, false},
		{"fanout_stale_status", reducer.DomainSupplyChainImpact, true, true, false},
		{"fanout_already_dirty", reducer.DomainSupplyChainImpact, true, false, true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			db := openContainerImageIdentityAckCapabilityProofDB(t)
			db.SetMaxOpenConns(6)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			now := time.Now().UTC()
			const scope, generation = "repository:6488-order", "generation:6488-order"
			seedContainerImageIdentityAckScope(t, ctx, db, scope)
			seedContainerImageIdentityAckGeneration(t, ctx, db, scope, generation)
			if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, scope, generation); err != nil {
				t.Fatal(err)
			}
			intents := []reducer.Intent{}
			for _, id := range []string{"order-z", "order-a"} {
				insertCrossScopeCompletionBaseConsumer(t, ctx, db, id, scope, generation, variant.domain, now)
				intents = append(intents, reducer.Intent{IntentID: id, Domain: variant.domain, ClaimEpoch: 1})
			}
			if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running', lease_owner='order-owner', claim_until=$1, container_image_identity_claim_epoch=1`, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if variant.dirty {
				if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET cross_scope_replay_required=TRUE`); err != nil {
					t.Fatal(err)
				}
			}
			worker := ackFanoutProbeConnection(t, ctx, db, "order-worker")
			if _, err := worker.ExecContext(ctx, `SET enable_indexscan=off; SET enable_bitmapscan=off`); err != nil {
				t.Fatal(err)
			}
			var pid int
			if err := worker.Conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			blocker, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err := blocker.ExecContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE work_item_id='order-a' FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			var lease reducer.CrossScopeCompletionLease
			if variant.fanout {
				event := insertCrossScopeCompletionEvent(t, ctx, db, reducer.DomainCICDRunCorrelation, "claimed", "order-fanout", now.Add(time.Hour), 1, now)
				lease = reducer.CrossScopeCompletionLease{EventID: event, ProducerDomain: reducer.DomainCICDRunCorrelation, LeaseOwner: "order-fanout", ClaimEpoch: 1}
			}
			done := make(chan error, 1)
			go func() {
				if variant.fanout {
					store := NewCrossScopeCompletionStore(worker)
					store.Now = func() time.Time { return now }
					_, err := store.Fanout(ctx, lease, 1)
					done <- err
					return
				}
				queue := ReducerQueue{db: worker, LeaseOwner: "order-owner", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
				done <- queue.AckBatch(ctx, intents, nil)
			}()
			var blockerPID int
			if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			waitForReducerRowLockWaiter(t, ctx, db, pid, blockerPID)
			probe, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, lockErr := probe.ExecContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE work_item_id='order-z' FOR UPDATE NOWAIT`)
			_ = probe.Rollback()
			if lockErr != nil {
				t.Errorf("writer locked later key before blocked first key: %v", lockErr)
			}
			if variant.stale {
				change := "lease_owner='new-owner'"
				if variant.domain == reducer.DomainContainerImageIdentity {
					change = "container_image_identity_claim_epoch=2"
				}
				if variant.fanout {
					change = "status='pending',lease_owner=NULL,claim_until=NULL"
				}
				if _, err := blocker.ExecContext(ctx, "UPDATE fact_work_items SET "+change+" WHERE work_item_id='order-a'"); err != nil {
					t.Fatal(err)
				}
			}
			if err := blocker.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			for _, intent := range intents {
				status, replay := "succeeded", false
				if variant.fanout {
					status, replay = "running", true
				}
				if variant.stale && intent.IntentID == "order-a" {
					status, replay = "running", false
					if variant.fanout {
						status = "pending"
					}
				}
				assertCrossScopeConsumerState(t, ctx, db, intent.IntentID, status, replay)
			}
		})
	}
}

func waitForReducerRowLockWaiter(t *testing.T, ctx context.Context, db *sql.DB, pid, blockerPID int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if err := db.QueryRowContext(ctx, `SELECT $2::int = ANY(pg_blocking_pids($1))`, pid, blockerPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d never reached held first row", pid)
}

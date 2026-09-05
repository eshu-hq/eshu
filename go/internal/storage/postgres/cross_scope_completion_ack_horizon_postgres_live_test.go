// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Exercise ON CONFLICT against an existing queued event on both sides of its
// capture. Before capture EPQ must include the increment; after capture the ACK
// must leave a new durable event when the captured conflict row is deleted.
func TestCrossScopeCompletionProducerAckCaptureHorizonLive(t *testing.T) {
	for _, domain := range []reducer.Domain{reducer.DomainContainerImageIdentity, reducer.DomainCICDRunCorrelation} {
		for _, afterCapture := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/after_capture=%t", domain, afterCapture), func(t *testing.T) {
				db := openContainerImageIdentityAckCapabilityProofDB(t)
				db.SetMaxOpenConns(8)
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				now := time.Now().UTC()
				const scope, generation = "repository:6488-horizon", "generation:6488-horizon"
				seedContainerImageIdentityAckScope(t, ctx, db, scope)
				seedContainerImageIdentityAckGeneration(t, ctx, db, scope, generation)
				if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, scope, generation); err != nil {
					t.Fatal(err)
				}
				insertCrossScopeCompletionBaseConsumer(t, ctx, db, "horizon-consumer", scope, generation, reducer.DomainSupplyChainImpact, now)
				insertCrossScopeCompletionBaseConsumer(t, ctx, db, "horizon-producer", scope, generation, domain, now)
				if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running',lease_owner='horizon-ack',claim_until=$1,container_image_identity_claim_epoch=1 WHERE work_item_id='horizon-producer'`, now.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				claimed := insertCrossScopeCompletionEvent(t, ctx, db, domain, "claimed", "horizon-fanout", now.Add(time.Hour), 1, now)
				queued := insertCrossScopeCompletionEvent(t, ctx, db, domain, "pending", "", time.Time{}, 0, now.Add(-time.Hour))
				// Keep the old queued event eligible even if ACK refreshes its debounce.
				if _, err := db.ExecContext(ctx, `UPDATE cross_scope_completion_events SET visible_at=$1 WHERE event_id=$2`, now.Add(-time.Minute), queued); err != nil {
					t.Fatal(err)
				}
				worker := ackFanoutProbeConnection(t, ctx, db, "horizon-fanout")
				var fanoutPID int
				if err := worker.Conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&fanoutPID); err != nil {
					t.Fatal(err)
				}
				store := NewCrossScopeCompletionStore(worker)
				store.Now = func() time.Time { return now }
				lease := reducer.CrossScopeCompletionLease{EventID: claimed, ProducerDomain: domain, LeaseOwner: "horizon-fanout", ClaimEpoch: 1}
				intents := []reducer.Intent{{IntentID: "horizon-producer", Domain: domain, ClaimEpoch: 1}}
				type outcome struct {
					result reducer.CrossScopeCompletionResult
					err    error
				}
				fanoutDone := make(chan outcome, 1)
				startFanout := func() { go func() { r, e := store.Fanout(ctx, lease, 2); fanoutDone <- outcome{r, e} }() }
				expectedItems := int64(3)
				expectedIntents := 1
				var consumerDone chan error
				if !afterCapture {
					expectedIntents = 0
					if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running', lease_owner='consumer-ack',claim_until=$1,cross_scope_replay_required=TRUE WHERE work_item_id='horizon-consumer'`, now.Add(time.Hour)); err != nil {
						t.Fatal(err)
					}
					// Execute the actual ACK statement but keep its event increment uncommitted
					// until fanout reaches that event's row lock under its earlier snapshot.
					tx, err := db.BeginTx(ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback()
					queue := ReducerQueue{db: SQLTx{Tx: tx}, LeaseOwner: "horizon-ack", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
					if err := queue.AckBatch(ctx, intents, nil); err != nil {
						t.Fatal(err)
					}
					startFanout()
					var producerPID int
					if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&producerPID); err != nil {
						t.Fatal(err)
					}
					waitForReducerRowLockWaiter(t, ctx, db, fanoutPID, producerPID)
					// Fanout must already own the consumer row while waiting for the event.
					probe, err := db.BeginTx(ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					_, lockErr := probe.ExecContext(ctx, `SELECT work_item_id FROM fact_work_items WHERE work_item_id='horizon-consumer' FOR UPDATE NOWAIT`)
					_ = probe.Rollback()
					if lockErr == nil {
						t.Error("fanout waited on queued event before locking its consumer")
					}
					consumerConn := ackFanoutProbeConnection(t, ctx, db, "consumer-ack")
					var consumerPID int
					if err := consumerConn.Conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&consumerPID); err != nil {
						t.Fatal(err)
					}
					consumerQueue := ReducerQueue{db: consumerConn, LeaseOwner: "consumer-ack", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
					consumerDone = make(chan error, 1)
					go func() {
						consumerDone <- consumerQueue.AckBatch(ctx, []reducer.Intent{{IntentID: "horizon-consumer", Domain: reducer.DomainSupplyChainImpact}}, nil)
					}()
					waitForReducerRowLockWaiter(t, ctx, db, consumerPID, fanoutPID)
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
				} else {
					expectedItems = 2
					const lockKey = 64885740
					if _, err := db.ExecContext(ctx, fmt.Sprintf(`
CREATE FUNCTION block_horizon_schedule() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_advisory_xact_lock(%d); RETURN NEW; END $$;
CREATE TRIGGER a_block_horizon_schedule BEFORE UPDATE ON fact_work_items
FOR EACH ROW WHEN (OLD.work_item_id='horizon-consumer')
EXECUTE FUNCTION block_horizon_schedule()`, lockKey)); err != nil {
						t.Fatal(err)
					}
					blocker, err := db.Conn(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer blocker.Close()
					if _, err := blocker.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
						t.Fatal(err)
					}
					defer func() { _, _ = blocker.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey) }()
					startFanout()
					waitForCrossScopeAdvisoryWaiter(t, ctx, db, lockKey)
					ack := ackFanoutProbeConnection(t, ctx, db, "horizon-ack")
					var ackPID int
					if err := ack.Conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&ackPID); err != nil {
						t.Fatal(err)
					}
					queue := ReducerQueue{db: ack, LeaseOwner: "horizon-ack", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
					ackDone := make(chan error, 1)
					go func() { ackDone <- queue.AckBatch(ctx, intents, nil) }()
					waitForReducerRowLockWaiter(t, ctx, db, ackPID, fanoutPID)
					if _, err := blocker.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
						t.Fatal(err)
					}
					if err := <-ackDone; err != nil {
						t.Fatal(err)
					}
				}
				got := <-fanoutDone
				if got.err != nil {
					t.Fatal(got.err)
				}
				if got.result.EventsProcessed != 2 || got.result.ProducerItemsProcessed != expectedItems || got.result.IntentsEnqueued != expectedIntents {
					t.Fatalf("fanout=%+v, want events=2 items=%d intents=%d", got.result, expectedItems, expectedIntents)
				}
				assertCrossScopeCompletionEventState(t, ctx, db, claimed, "")
				assertCrossScopeCompletionEventState(t, ctx, db, queued, "")
				if consumerDone != nil {
					if err := <-consumerDone; err != nil {
						t.Fatal(err)
					}
				}
				assertCrossScopeConsumerState(t, ctx, db, "horizon-consumer", "pending", false)
				var events, items int
				if err := db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(producer_item_count),0) FROM cross_scope_completion_events`).Scan(&events, &items); err != nil {
					t.Fatal(err)
				}
				want := 0
				if afterCapture {
					want = 1
				}
				if events != want || items != want {
					t.Fatalf("remaining events=%d items=%d, want %d each", events, items, want)
				}
			})
		}
	}
}

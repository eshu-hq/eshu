// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// A real audit FK holds KEY SHARE on the work row until commit. Completing a
// non-key mutation must remain compatible with that reference. This proves lock
// compatibility, not a new deadlock in the admin replay transaction sequence.
func TestReducerContentionGateAckFanoutAuditFKCompatibilityLive(t *testing.T) {
	for _, variant := range []struct {
		name   string
		domain reducer.Domain
		fanout bool
	}{
		{"generic", reducer.DomainSupplyChainImpact, false},
		{"identity", reducer.DomainContainerImageIdentity, false},
		{"cicd", reducer.DomainCICDRunCorrelation, false},
		{"fanout", reducer.DomainSupplyChainImpact, true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			db := openReducerAckFanoutProofDB(t)
			db.SetMaxOpenConns(4)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var hasWorkFK bool
			if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid='fact_replay_events'::regclass
      AND confrelid='fact_work_items'::regclass
      AND contype='f' AND convalidated
      AND conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid='fact_replay_events'::regclass AND attname='work_item_id')]
      AND confkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid='fact_work_items'::regclass AND attname='work_item_id')]
)`).Scan(&hasWorkFK); err != nil {
				t.Fatal(err)
			}
			if !hasWorkFK {
				t.Fatal("fixture lacks validated audit-to-work FK required for KEY SHARE proof")
			}
			now := time.Now().UTC()
			const scope, generation, id = "repository:6488-fk", "generation:6488-fk", "work:6488-fk"
			seedContainerImageIdentityAckScope(t, ctx, db, scope)
			seedContainerImageIdentityAckGeneration(t, ctx, db, scope, generation)
			if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, scope, generation); err != nil {
				t.Fatal(err)
			}
			insertCrossScopeCompletionBaseConsumer(t, ctx, db, id, scope, generation, variant.domain, now)
			if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running',lease_owner='fk-ack',claim_until=$1,container_image_identity_claim_epoch=1 WHERE work_item_id=$2`, now.Add(time.Hour), id); err != nil {
				t.Fatal(err)
			}
			audit, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer audit.Rollback()
			if _, err := audit.ExecContext(ctx, `INSERT INTO fact_replay_events (replay_event_id,work_item_id,scope_id,generation_id,created_at) VALUES ('audit:6488-fk',$1,$2,$3,$4)`, id, scope, generation, now); err != nil {
				t.Fatal(err)
			}
			worker := ackFanoutProbeConnection(t, ctx, db, "fk-writer")
			if _, err := worker.ExecContext(ctx, `SET lock_timeout='1s'`); err != nil {
				t.Fatal(err)
			}
			if variant.fanout {
				event := insertCrossScopeCompletionEvent(t, ctx, db, reducer.DomainCICDRunCorrelation, "claimed", "fk-fanout", now.Add(time.Hour), 1, now)
				store := NewCrossScopeCompletionStore(worker)
				store.Now = func() time.Time { return now }
				result, err := store.Fanout(ctx, reducer.CrossScopeCompletionLease{EventID: event, ProducerDomain: reducer.DomainCICDRunCorrelation, LeaseOwner: "fk-fanout", ClaimEpoch: 1}, 1)
				if err != nil {
					t.Fatalf("fanout blocked by audit FK KEY SHARE: %v", err)
				}
				if result.EventsProcessed != 1 || result.ProducerItemsProcessed != 1 || result.IntentsEnqueued != 1 {
					t.Fatalf("fanout result=%+v", result)
				}
				assertCrossScopeConsumerState(t, ctx, db, id, "running", true)
			} else {
				queue := ReducerQueue{db: worker, LeaseOwner: "fk-ack", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
				if err := queue.AckBatch(ctx, []reducer.Intent{{IntentID: id, Domain: variant.domain, ClaimEpoch: 1}}, nil); err != nil {
					t.Fatalf("ACK blocked by audit FK KEY SHARE: %v", err)
				}
				assertCrossScopeConsumerState(t, ctx, db, id, "succeeded", false)
			}
			var visible int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_replay_events WHERE replay_event_id='audit:6488-fk'`).Scan(&visible); err != nil {
				t.Fatal(err)
			}
			if visible != 0 {
				t.Fatal("audit transaction ended before writer completed")
			}
			if err := audit.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_replay_events WHERE replay_event_id='audit:6488-fk'`).Scan(&visible); err != nil {
				t.Fatal(err)
			}
			if visible != 1 {
				t.Fatalf("committed audit rows=%d, want 1", visible)
			}
		})
	}
}

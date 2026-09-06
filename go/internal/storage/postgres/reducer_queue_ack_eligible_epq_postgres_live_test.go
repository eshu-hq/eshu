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

// A heartbeat changes the tuple version while keeping the exact ACK eligible.
// Ordered prelocking must preserve this ACK after its concurrent-row recheck.
func TestReducerContentionGateAckEligibleEPQLive(t *testing.T) {
	for _, domain := range []reducer.Domain{reducer.DomainSupplyChainImpact, reducer.DomainContainerImageIdentity, reducer.DomainCICDRunCorrelation} {
		t.Run(string(domain), func(t *testing.T) {
			db := openReducerAckFanoutProofDB(t)
			db.SetMaxOpenConns(5)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			now := time.Now().UTC().Truncate(time.Microsecond)
			const scope, generation, id, owner = "repository:6488-positive-epq", "generation:6488-positive-epq", "work:6488-positive-epq", "positive-epq-ack"
			seedContainerImageIdentityAckScope(t, ctx, db, scope)
			seedContainerImageIdentityAckGeneration(t, ctx, db, scope, generation)
			insertCrossScopeCompletionBaseConsumer(t, ctx, db, id, scope, generation, domain, now)
			if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running',lease_owner=$1,claim_until=$2,container_image_identity_claim_epoch=1 WHERE work_item_id=$3`, owner, now.Add(time.Hour), id); err != nil {
				t.Fatal(err)
			}
			var oldTID, newTID string
			if err := db.QueryRowContext(ctx, `SELECT ctid::text FROM fact_work_items WHERE work_item_id=$1`, id).Scan(&oldTID); err != nil {
				t.Fatal(err)
			}
			heartbeat, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer heartbeat.Rollback()
			if err := heartbeat.QueryRowContext(ctx, `UPDATE fact_work_items SET updated_at=$1 WHERE work_item_id=$2 RETURNING ctid::text`, now.Add(time.Microsecond), id).Scan(&newTID); err != nil {
				t.Fatal(err)
			}
			if oldTID == newTID {
				t.Fatal("heartbeat did not change tuple version")
			}
			var blockerPID, workerPID int
			if err := heartbeat.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatal(err)
			}
			worker := ackFanoutProbeConnection(t, ctx, db, "positive-epq-ack")
			if err := worker.Conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&workerPID); err != nil {
				t.Fatal(err)
			}
			query, args := eligibleEPQAckQuery(now, owner, reducer.Intent{IntentID: id, Domain: domain, ClaimEpoch: 1})
			type outcome struct {
				result sql.Result
				err    error
			}
			done := make(chan outcome, 1)
			go func() { r, e := worker.ExecContext(ctx, query, args...); done <- outcome{r, e} }()
			waitForReducerRowLockWaiter(t, ctx, db, workerPID, blockerPID)
			if err := heartbeat.Commit(); err != nil {
				t.Fatal(err)
			}
			got := <-done
			if got.err != nil {
				t.Fatalf("eligible ACK after heartbeat commit: %v", got.err)
			}
			affected, err := got.result.RowsAffected()
			if err != nil {
				t.Fatal(err)
			}
			var status, leaseOwner string
			if err := db.QueryRowContext(ctx, `SELECT status,COALESCE(lease_owner,'') FROM fact_work_items WHERE work_item_id=$1`, id).Scan(&status, &leaseOwner); err != nil {
				t.Fatal(err)
			}
			t.Logf("before=%s heartbeat=%s affected=%d status=%s owner=%q", oldTID, newTID, affected, status, leaseOwner)
			if affected != 1 || status != "succeeded" || leaseOwner != "" {
				t.Fatalf("lost still-eligible ACK after EPQ: affected=%d status=%s owner=%q", affected, status, leaseOwner)
			}
			var emitted int64
			if err := db.QueryRowContext(ctx, `SELECT COALESCE(sum(producer_item_count),0) FROM cross_scope_completion_events`).Scan(&emitted); err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if domain == reducer.DomainSupplyChainImpact {
				want = 0
			}
			if emitted != want {
				t.Fatalf("producer items=%d, want %d", emitted, want)
			}
		})
	}
}

func eligibleEPQAckQuery(now time.Time, owner string, intent reducer.Intent) (string, []any) {
	switch intent.Domain {
	case reducer.DomainContainerImageIdentity:
		return ackContainerImageIdentityReducerWorkBatchQuery(now, owner, []reducer.Intent{intent})
	case reducer.DomainCICDRunCorrelation:
		return ackCICDRunCorrelationReducerWorkBatchQuery(now, owner, []reducer.Intent{intent})
	default:
		return ackReducerWorkBatchQuery(1), []any{now, owner, intent.IntentID}
	}
}

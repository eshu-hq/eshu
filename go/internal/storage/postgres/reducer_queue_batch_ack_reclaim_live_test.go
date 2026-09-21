// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerQueueAckBatchFencesSupersededClaimLive is the lease-safety proof
// for the #6162 fix, run against real Postgres rather than a fake execer.
//
// It reproduces the Ifa expire-lease cell's exact perturbation -- the same
// UPDATE the cell issues, with no kill -- so one work item genuinely carries
// two claim identities, and then checks the three properties the fix rests on:
//
//  1. authority: the superseded claim alone acks nothing and leaves the row
//     claimed, because the ack statement fences on last_attempt_at;
//  2. liveness: the batch carrying both claims still acks the surviving one and
//     reports ErrReducerClaimRejected, which the batch acker survives, instead
//     of erroring out and leaving the row claimed forever;
//  3. order independence: the acker's pending slice order depends on which
//     worker goroutine finishes first, so both orders must resolve the same.
func TestReducerQueueAckBatchFencesSupersededClaimLive(t *testing.T) {
	db := openReducerAckReclaimProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	const owner = "reducer-6162-live"
	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
	}

	for _, order := range []struct {
		name       string
		staleFirst bool
	}{
		{name: "stale first", staleFirst: true},
		{name: "fresh first", staleFirst: false},
	} {
		t.Run(order.name, func(t *testing.T) {
			scopeID := fmt.Sprintf("repository:6162-ack-reclaim-%t", order.staleFirst)
			generationID := "generation:6162-ack-reclaim"
			workItemID := fmt.Sprintf("reducer_6162_ack_reclaim_%t", order.staleFirst)

			seedReducerAckReclaimScope(t, ctx, db, scopeID)
			seedReducerAckReclaimGeneration(t, ctx, db, scopeID, generationID)
			seedReducerAckReclaimWorkItem(t, ctx, db, workItemID, scopeID, generationID)

			stale := claimOneReducerAckReclaimIntent(t, ctx, queue, workItemID)
			if got, want := stale.AttemptCount, 1; got != want {
				t.Fatalf("first claim attempt count = %d, want %d", got, want)
			}

			// The cell's statement, verbatim: expire every live reducer lease
			// while its handler is still in flight.
			if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET claim_until = now()
WHERE stage = 'reducer' AND status IN ('claimed', 'running')
`); err != nil {
				t.Fatalf("force lease expiry: %v", err)
			}

			fresh := claimOneReducerAckReclaimIntent(t, ctx, queue, workItemID)
			if got, want := fresh.AttemptCount, 2; got != want {
				t.Fatalf("reclaim attempt count = %d, want %d", got, want)
			}
			if stale.ClaimedAt == nil || fresh.ClaimedAt == nil {
				t.Fatal("both claims must carry a claim timestamp")
			}
			if stale.ClaimedAt.Equal(*fresh.ClaimedAt) {
				t.Fatalf(
					"both claims share last_attempt_at %v, so the batch would not be a reclaim at all",
					*stale.ClaimedAt,
				)
			}

			// (1) The superseded claim on its own has no authority.
			staleOnlyErr := queue.AckBatch(ctx, []reducer.Intent{stale}, nil)
			if !errors.Is(staleOnlyErr, reducer.ErrExecutionClaimRejected) {
				t.Fatalf("superseded-only AckBatch() = %v, want a claim rejection", staleOnlyErr)
			}
			status, leaseOwner := readReducerAckReclaimWorkItem(t, ctx, db, workItemID)
			if status != "claimed" || leaseOwner != owner {
				t.Fatalf(
					"after the superseded-only ACK: status=%q lease_owner=%q, want claimed/%s",
					status, leaseOwner, owner,
				)
			}

			// (2) and (3) The batch carrying both claims, in either order.
			batch := []reducer.Intent{stale, fresh}
			if !order.staleFirst {
				batch = []reducer.Intent{fresh, stale}
			}
			batchErr := queue.AckBatch(ctx, batch, nil)
			if !errors.Is(batchErr, reducer.ErrExecutionClaimRejected) {
				t.Fatalf(
					"duplicate-claim AckBatch() = %v, want a claim rejection the batch acker survives",
					batchErr,
				)
			}
			status, leaseOwner = readReducerAckReclaimWorkItem(t, ctx, db, workItemID)
			if status != "succeeded" {
				t.Fatalf(
					"after the duplicate-claim ACK: status=%q, want succeeded; a row left claimed is the #6162 stall",
					status,
				)
			}
			if leaseOwner != "" {
				t.Fatalf("after the duplicate-claim ACK: lease_owner=%q, want it cleared", leaseOwner)
			}
		})
	}
}

func claimOneReducerAckReclaimIntent(
	t *testing.T,
	ctx context.Context,
	queue ReducerQueue,
	workItemID string,
) reducer.Intent {
	t.Helper()
	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	for _, intent := range intents {
		if intent.IntentID == workItemID {
			return intent
		}
	}
	t.Fatalf("claim batch returned %d intents, none of them %s", len(intents), workItemID)
	return reducer.Intent{}
}

func readReducerAckReclaimWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
) (string, string) {
	t.Helper()
	var status string
	var leaseOwner sql.NullString
	if err := db.QueryRowContext(ctx, `
SELECT status, lease_owner FROM fact_work_items WHERE work_item_id = $1
`, workItemID).Scan(&status, &leaseOwner); err != nil {
		t.Fatalf("read work item %s: %v", workItemID, err)
	}
	return status, leaseOwner.String
}

func seedReducerAckReclaimScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    $1, 'repository', 'git', $1, 'reducer', $1,
    clock_timestamp(), clock_timestamp(), 'active'
)
`, scopeID); err != nil {
		t.Fatalf("seed ACK reclaim scope: %v", err)
	}
}

func seedReducerAckReclaimGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    $2, $1, 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), 'active'
)
ON CONFLICT DO NOTHING
`, scopeID, generationID); err != nil {
		t.Fatalf("seed ACK reclaim generation: %v", err)
	}
}

func seedReducerAckReclaimWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	scopeID string,
	generationID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    payload, created_at, updated_at
) VALUES (
    $1, $2, $3, 'reducer', 'gcp_resource_materialization',
    'intent', $1, 'pending', 0,
    jsonb_build_object(
        'entity_key', $1::text,
        'reason', 'issue 6162 ack reclaim lease-safety proof',
        'fact_id', $1::text,
        'source_system', 'git'
    ),
    clock_timestamp(), clock_timestamp()
)
`, workItemID, scopeID, generationID); err != nil {
		t.Fatalf("seed ACK reclaim work item: %v", err)
	}
}

func openReducerAckReclaimProofDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN to a disposable Postgres database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	schema := fmt.Sprintf("eshu_6162_ack_reclaim_%d", time.Now().UnixNano())
	adminDB := openActiveOCIWarningIndexProofDB(t, dsn)
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create ACK reclaim schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE",
		); err != nil {
			t.Errorf("drop ACK reclaim schema: %v", err)
		}
	})
	db := openActiveOCIWarningIndexProofDB(t, activeOCIWarningIndexSchemaDSN(t, dsn, schema))
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply ACK reclaim schema: %v", err)
	}
	return db
}

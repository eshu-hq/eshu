// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8009 queue-fence proof that a stale reducer ACK cannot change its successor's row: the superseded-only ACK is rejected and leaves the row claimed, the duplicate-claim batch acks the survivor, and the expiry-interleave replacement claim completes while the stale heartbeat and ACK are rejected; disposable PostgreSQL databases through postgresproof with the production expiry helper; enrolled in the live-postgres-readiness runner; TestReducerQueueAckBatchFencesSupersededClaimLive 5.30s and TestReducerQueueExpiryInterleaveLive 10.27s (15.9s package) on local PostgreSQL 16.15 on 2026-10-12; CI PostgreSQL 18: TestReducerQueueAckBatchFencesSupersededClaimLive 2.160s and TestReducerQueueExpiryInterleaveLive 2.160s (242/242 PASS) in run 38359196081 on 2026-10-13
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_ACK_RECLAIM_PROOF_DISPOSABLE ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
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

// snapshotReducerExpirySuccessor returns the full expiry-target queue row as
// JSON text and the cross_scope_completion_events row count, so a fenced stale
// ACK can be proven to change neither.
func snapshotReducerExpirySuccessor(t *testing.T, ctx context.Context, db *sql.DB) (string, int) {
	t.Helper()
	var row string
	if err := db.QueryRowContext(ctx, "SELECT to_jsonb(work)::text FROM fact_work_items work WHERE work_item_id = 'expiry-target'").Scan(&row); err != nil {
		t.Fatalf("snapshot successor row: %v", err)
	}
	var events int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM cross_scope_completion_events").Scan(&events); err != nil {
		t.Fatalf("snapshot cross_scope_completion_events: %v", err)
	}
	return row, events
}

// openReducerAckReclaimProofDB opens a disposable PostgreSQL database through
// postgresproof and bootstraps the Eshu schema on it. It reads
// ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN (an administrative postgres-database
// DSN) and ESHU_REDUCER_ACK_RECLAIM_PROOF_DISPOSABLE=1; an empty DSN skips,
// while a set-but-unusable DSN fails closed. The live-postgres-readiness
// runner provides both against its PostgreSQL 18 service.
func openReducerAckReclaimProofDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(t,
		strings.TrimSpace(os.Getenv("ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN")),
		os.Getenv("ESHU_REDUCER_ACK_RECLAIM_PROOF_DISPOSABLE"),
		90*time.Second)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply ACK reclaim schema: %v", err)
	}
	return db
}

// reducerAckReclaimDisposableDSN returns a DSN for the disposable database db
// points at, derived from the administrative DSN by swapping in the current
// database name. The production expiry helper shells out to psql, so it needs
// the disposable address as text rather than the test's *sql.DB.
func reducerAckReclaimDisposableDSN(t *testing.T, ctx context.Context, adminDSN string, db *sql.DB) string {
	t.Helper()
	var databaseName string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatalf("read disposable database name: %v", err)
	}
	// A hand-exported DSN may carry a trailing space or newline; trim at
	// this consumer too, matching the OpenDisposableDatabase call site.
	parsed, err := url.Parse(strings.TrimSpace(adminDSN))
	if err != nil {
		t.Fatalf("parse administrative DSN: %v", err)
	}
	parsed.Path = "/" + databaseName
	parsed.RawPath = ""
	return parsed.String()
}

// TestReducerQueueExpiryInterleaveLive controls the handler-to-ACK boundary
// while exercising production claim, heartbeat, ACK, and fault-expiry SQL.
// It is a queue mechanism shim, not a substitute for the real fault cell.
func TestReducerQueueExpiryInterleaveLive(t *testing.T) {
	for _, held := range []bool{false, true} {
		name := "ACK wins after claim poll"
		if held {
			name = "handler held through replacement claim"
		}
		t.Run(name, func(t *testing.T) {
			db := openReducerAckReclaimProofDB(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			queue := ReducerQueue{database: SQLDB{DB: db}, LeaseOwner: "expiry-proof", LeaseDuration: time.Minute}
			seedReducerAckReclaimScope(t, ctx, db, "repository:7939")
			seedReducerAckReclaimGeneration(t, ctx, db, "repository:7939", "generation:7939")
			seedReducerAckReclaimWorkItem(t, ctx, db, "expiry-target", "repository:7939", "generation:7939")
			stale := claimOneReducerAckReclaimIntent(t, ctx, queue, "expiry-target")
			if err := queue.Heartbeat(ctx, stale); err != nil {
				t.Fatalf("startup heartbeat: %v", err)
			}
			entered, release, acked := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				close(entered)
				select {
				case <-release:
					acked <- queue.AckBatch(ctx, []reducer.Intent{stale}, nil)
				case <-ctx.Done():
					acked <- ctx.Err()
				}
			}()
			<-entered
			var observed int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM fact_work_items WHERE stage='reducer' AND status='claimed'").Scan(&observed); err != nil || observed != 1 {
				t.Fatalf("claim poll: count=%d err=%v", observed, err)
			}
			if !held {
				close(release)
				if err := <-acked; err != nil {
					t.Fatalf("ACK before expiry: %v", err)
				}
			}
			var schema string
			if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
				t.Fatal(err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			helper := filepath.Join(cwd, "../../../../scripts/lib/ifa_fault_reclaim_assert.sh")
			command := exec.CommandContext(ctx, "bash", "-c", `source "$1"; ifa_det_pg() { psql "$3" -v ON_ERROR_STOP=1 -Atc "$4"; }; ifa_fault_expire_reducer_claims proof 0 "$2" unused`, "proof", helper, reducerAckReclaimDisposableDSN(t, ctx, os.Getenv("ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN"), db))
			command.Env = append(os.Environ(), "PGOPTIONS=-c search_path="+schema)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("production expiry helper: %v: %s", err, output)
			}
			want := "UPDATE 0"
			if held {
				want = "UPDATE 1"
			}
			if strings.TrimSpace(string(output)) != want {
				t.Fatalf("expiry result=%q want %q", output, want)
			}
			t.Logf("claimed poll=%d; production expiry=%s", observed, strings.TrimSpace(string(output)))
			if !held {
				return
			}
			// An independent item completes while the first handler is held.
			seedReducerAckReclaimWorkItem(t, ctx, db, "control", "repository:7939", "generation:7939")
			intents, err := queue.ClaimBatch(ctx, 8)
			if err != nil || len(intents) != 2 {
				t.Fatalf("competing claim: intents=%d err=%v", len(intents), err)
			}
			var fresh reducer.Intent
			for _, intent := range intents {
				if intent.IntentID == "expiry-target" {
					fresh = intent
				} else if err := queue.AckBatch(ctx, []reducer.Intent{intent}, nil); err != nil {
					t.Fatalf("control ACK: %v", err)
				}
			}
			if fresh.AttemptCount != 2 || fresh.ClaimedAt == nil || stale.ClaimedAt == nil || fresh.ClaimedAt.Equal(*stale.ClaimedAt) {
				t.Fatalf("replacement claim missing new epoch/attempt: %+v", fresh)
			}
			if err := queue.Heartbeat(ctx, stale); !errors.Is(err, reducer.ErrExecutionClaimRejected) {
				t.Fatalf("stale heartbeat=%v", err)
			}
			// No heartbeat goroutine runs here, so the live successor's whole
			// row, claim_until included, must survive the stale ACK unchanged.
			successorBefore, eventsBefore := snapshotReducerExpirySuccessor(t, ctx, db)
			close(release)
			if err := <-acked; !errors.Is(err, reducer.ErrExecutionClaimRejected) {
				t.Fatalf("stale ACK=%v", err)
			}
			successorAfter, eventsAfter := snapshotReducerExpirySuccessor(t, ctx, db)
			if successorAfter != successorBefore {
				t.Fatalf("stale ACK changed the live successor row:\nbefore=%s\nafter=%s", successorBefore, successorAfter)
			}
			if eventsAfter != eventsBefore {
				t.Fatalf("stale ACK changed cross_scope_completion_events: before=%d after=%d", eventsBefore, eventsAfter)
			}
			if err := queue.AckBatch(ctx, []reducer.Intent{fresh}, nil); err != nil {
				t.Fatalf("fresh ACK: %v", err)
			}
			status, owner := readReducerAckReclaimWorkItem(t, ctx, db, "expiry-target")
			if status != "succeeded" || owner != "" {
				t.Fatalf("terminal row=%s/%s", status, owner)
			}
			t.Log("replacement attempt=2; control completed while held; stale heartbeat/ACK rejected; fresh terminal success")
		})
	}
}
